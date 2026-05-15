package githubsync

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	infraservices "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	importexport "github.com/takutakahashi/agentapi-proxy/pkg/import"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
	"gopkg.in/yaml.v3"
)

// Syncer orchestrates bidirectional GitHub sync for a user or team.
type Syncer struct {
	settingsRepo  portrepos.SettingsRepository
	scheduleRepo  schedule.Manager
	webhookRepo   portrepos.WebhookRepository
	memoryRepo    portrepos.MemoryRepository
	taskRepo      portrepos.TaskRepository
	taskGroupRepo portrepos.TaskGroupRepository
	userFileRepo  portrepos.UserFileRepository
}

// NewSyncer creates a Syncer. Non-nil repos are synced.
func NewSyncer(
	settingsRepo portrepos.SettingsRepository,
	scheduleRepo schedule.Manager,
	webhookRepo portrepos.WebhookRepository,
	memoryRepo portrepos.MemoryRepository,
	taskRepo portrepos.TaskRepository,
	taskGroupRepo portrepos.TaskGroupRepository,
	userFileRepo portrepos.UserFileRepository,
) *Syncer {
	return &Syncer{
		settingsRepo:  settingsRepo,
		scheduleRepo:  scheduleRepo,
		webhookRepo:   webhookRepo,
		memoryRepo:    memoryRepo,
		taskRepo:      taskRepo,
		taskGroupRepo: taskGroupRepo,
		userFileRepo:  userFileRepo,
	}
}

// Push exports all resources for settingsName and commits them to GitHub.
// settingsName is the user ID or team slug (same key as /settings/:name).
func (s *Syncer) Push(ctx context.Context, settingsName, userID string, commitMessage string) (*PushResponse, error) {
	settings, err := s.settingsRepo.FindByName(ctx, settingsName)
	if err != nil {
		return nil, fmt.Errorf("settings not found for %q: %w", settingsName, err)
	}

	cfg := settings.GitSync()
	if cfg == nil || !cfg.Enabled {
		return nil, fmt.Errorf("GitHub sync is not enabled for %q", settingsName)
	}

	token := cfg.GitHubToken
	if token == "" {
		return nil, fmt.Errorf("github_token is required in git_sync config for %q", settingsName)
	}

	enc, err := NewSyncEncryptor(ctx, cfg.Encryption.KMSKeyARN, cfg.Encryption.AWSRegion)
	if err != nil {
		return nil, fmt.Errorf("failed to init sync encryptor: %w", err)
	}

	// Get or generate fixed DEK
	var dek []byte
	if cfg.Encryption.EncryptedDEK == "" {
		var encDEK string
		dek, encDEK, err = enc.GenerateAndEncryptDEK(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to generate DEK: %w", err)
		}
		cfg.Encryption.EncryptedDEK = encDEK
		cfg.Encryption.DEKVersion = 1
		settings.SetGitSync(cfg)
		if saveErr := s.settingsRepo.Save(ctx, settings); saveErr != nil {
			return nil, fmt.Errorf("failed to save new DEK: %w", saveErr)
		}
		log.Printf("[SYNC] Generated new DEK (v1) for %s", settingsName)
	} else {
		dek, err = enc.DecryptDEK(ctx, cfg.Encryption.EncryptedDEK)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt DEK: %w", err)
		}
	}

	rootPath := strings.TrimRight(cfg.RootPath, "/") + "/"
	files := make(map[string][]byte)

	if err := s.exportTeamResources(ctx, settingsName, userID, dek, rootPath, files); err != nil {
		log.Printf("[SYNC] team resource export warning for %s: %v", settingsName, err)
	}

	if err := s.exportUserResources(ctx, userID, dek, rootPath, files); err != nil {
		log.Printf("[SYNC] user resource export warning for %s: %v", settingsName, err)
	}

	meta := SyncMeta{
		APIVersion: "agentapi-proxy/v1",
		Kind:       "SyncMeta",
		SyncedAt:   time.Now().UTC(),
		Encryption: SyncMetaEnc{
			Provider:   "aws_kms",
			KMSKeyARN:  cfg.Encryption.KMSKeyARN,
			Algorithm:  "AES-256-GCM",
			DEKVersion: cfg.Encryption.DEKVersion,
		},
	}
	if metaBytes, merr := yaml.Marshal(meta); merr == nil {
		files[rootPath+".sync-meta.yaml"] = metaBytes
	}

	if len(files) == 0 {
		return &PushResponse{PushedAt: time.Now()}, nil
	}

	if commitMessage == "" {
		commitMessage = fmt.Sprintf("agentapi-proxy sync: %s", time.Now().UTC().Format(time.RFC3339))
	}

	ghClient, err := NewGitHubSyncClient(ctx, token, cfg.RepoFullName)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}

	sha, err := ghClient.PushFiles(ctx, cfg.Branch, commitMessage, files)
	if err != nil {
		return nil, fmt.Errorf("failed to push to GitHub: %w", err)
	}

	return &PushResponse{
		CommitSHA: sha,
		PushedAt:  time.Now(),
		Summary:   SyncSummary{FilesWritten: len(files)},
	}, nil
}

// Pull downloads resources from GitHub and imports them.
func (s *Syncer) Pull(ctx context.Context, settingsName, userID string, deleteOrphans bool) (*PullResponse, error) {
	settings, err := s.settingsRepo.FindByName(ctx, settingsName)
	if err != nil {
		return nil, fmt.Errorf("settings not found for %q: %w", settingsName, err)
	}

	cfg := settings.GitSync()
	if cfg == nil || !cfg.Enabled {
		return nil, fmt.Errorf("GitHub sync is not enabled for %q", settingsName)
	}

	token := cfg.GitHubToken
	if token == "" {
		return nil, fmt.Errorf("github_token is required in git_sync config for %q", settingsName)
	}

	if cfg.Encryption.EncryptedDEK == "" {
		return nil, fmt.Errorf("no DEK found — push at least once before pulling")
	}

	enc, err := NewSyncEncryptor(ctx, cfg.Encryption.KMSKeyARN, cfg.Encryption.AWSRegion)
	if err != nil {
		return nil, fmt.Errorf("failed to init sync encryptor: %w", err)
	}
	dek, err := enc.DecryptDEK(ctx, cfg.Encryption.EncryptedDEK)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt DEK: %w", err)
	}

	rootPath := strings.TrimRight(cfg.RootPath, "/") + "/"

	ghClient, err := NewGitHubSyncClient(ctx, token, cfg.RepoFullName)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}

	paths, err := ghClient.ListFiles(ctx, cfg.Branch, rootPath)
	if err != nil {
		return nil, fmt.Errorf("failed to list GitHub files: %w", err)
	}

	filesWritten := 0
	for _, path := range paths {
		if strings.HasSuffix(path, ".sync-meta.yaml") {
			continue
		}
		content, err := ghClient.GetFile(ctx, cfg.Branch, path)
		if err != nil {
			log.Printf("[SYNC] Warning: failed to get %s: %v", path, err)
			continue
		}
		rel := strings.TrimPrefix(path, rootPath)
		if err := s.importFile(ctx, rel, content, settingsName, userID, dek); err != nil {
			log.Printf("[SYNC] Warning: failed to import %s: %v", rel, err)
			continue
		}
		filesWritten++
	}

	return &PullResponse{
		PulledAt: time.Now(),
		Summary:  SyncSummary{FilesWritten: filesWritten},
	}, nil
}

// RotateKey generates a fresh DEK, re-encrypts all GitHub files, and updates Settings.
func (s *Syncer) RotateKey(ctx context.Context, settingsName, userID string) (*RotateKeyResponse, error) {
	settings, err := s.settingsRepo.FindByName(ctx, settingsName)
	if err != nil {
		return nil, fmt.Errorf("settings not found for %q: %w", settingsName, err)
	}

	cfg := settings.GitSync()
	if cfg == nil || !cfg.Enabled {
		return nil, fmt.Errorf("GitHub sync is not enabled for %q", settingsName)
	}

	if cfg.Encryption.EncryptedDEK == "" {
		return nil, fmt.Errorf("no existing DEK — push at least once before rotating")
	}

	enc, err := NewSyncEncryptor(ctx, cfg.Encryption.KMSKeyARN, cfg.Encryption.AWSRegion)
	if err != nil {
		return nil, fmt.Errorf("failed to init sync encryptor: %w", err)
	}

	oldDEK, err := enc.DecryptDEK(ctx, cfg.Encryption.EncryptedDEK)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt old DEK: %w", err)
	}

	rootPath := strings.TrimRight(cfg.RootPath, "/") + "/"

	ghClient, err := NewGitHubSyncClient(ctx, cfg.GitHubToken, cfg.RepoFullName)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}

	paths, err := ghClient.ListFiles(ctx, cfg.Branch, rootPath)
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	newDEK, newEncDEK, err := enc.GenerateAndEncryptDEK(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to generate new DEK: %w", err)
	}
	newVersion := cfg.Encryption.DEKVersion + 1

	files := make(map[string][]byte, len(paths))
	for _, path := range paths {
		content, err := ghClient.GetFile(ctx, cfg.Branch, path)
		if err != nil {
			return nil, fmt.Errorf("failed to get %s during rotation: %w", path, err)
		}
		reenc, err := reencryptYAML(content, oldDEK, newDEK)
		if err != nil {
			return nil, fmt.Errorf("failed to re-encrypt %s: %w", path, err)
		}
		files[path] = reenc
	}

	sha, err := ghClient.PushFiles(ctx, cfg.Branch,
		fmt.Sprintf("agentapi-proxy key rotation: DEK v%d", newVersion), files)
	if err != nil {
		return nil, fmt.Errorf("failed to push re-encrypted files: %w", err)
	}

	cfg.Encryption.EncryptedDEK = newEncDEK
	cfg.Encryption.DEKVersion = newVersion
	settings.SetGitSync(cfg)
	if err := s.settingsRepo.Save(ctx, settings); err != nil {
		return nil, fmt.Errorf("failed to save new DEK: %w", err)
	}

	return &RotateKeyResponse{
		CommitSHA:  sha,
		RotatedAt:  time.Now(),
		DEKVersion: newVersion,
	}, nil
}

// exportTeamResources exports schedules/webhooks/settings via the existing exporter.
func (s *Syncer) exportTeamResources(ctx context.Context, settingsName, userID string, dek []byte, rootPath string, files map[string][]byte) error {
	noopSvc := infraservices.NewNoopEncryptionService()
	exporter := importexport.NewExporter(s.scheduleRepo, s.webhookRepo, s.settingsRepo, noopSvc)

	resources, err := exporter.Export(ctx, settingsName, userID, importexport.ExportOptions{
		Format:         importexport.ExportFormatYAML,
		IncludeSecrets: true,
	})
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}

	if err := encryptTeamResourcesFields(resources, dek); err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}

	data, err := yaml.Marshal(resources)
	if err != nil {
		return fmt.Errorf("serialize: %w", err)
	}

	files[rootPath+"resources.yaml"] = data
	return nil
}

// exportUserResources exports user-scoped memories, tasks, task groups, and files.
func (s *Syncer) exportUserResources(ctx context.Context, userID string, dek []byte, rootPath string, files map[string][]byte) error {
	type userExport struct {
		APIVersion string            `yaml:"apiVersion"`
		Kind       string            `yaml:"kind"`
		Memories   []memoryExport    `yaml:"memories,omitempty"`
		Tasks      []taskExport      `yaml:"tasks,omitempty"`
		TaskGroups []taskGroupExport `yaml:"task_groups,omitempty"`
		Files      []userFileExport  `yaml:"files,omitempty"`
	}

	export := userExport{
		APIVersion: "agentapi-proxy/v1",
		Kind:       "UserResources",
	}

	if s.memoryRepo != nil {
		memories, err := s.memoryRepo.List(ctx, portrepos.MemoryFilter{
			Scope:   entities.ScopeUser,
			OwnerID: userID,
		})
		if err == nil {
			for _, m := range memories {
				export.Memories = append(export.Memories, memoryExport{
					ID:        m.ID(),
					Title:     m.Title(),
					Content:   m.Content(),
					Tags:      m.Tags(),
					Scope:     string(m.Scope()),
					CreatedAt: m.CreatedAt().Format(time.RFC3339),
					UpdatedAt: m.UpdatedAt().Format(time.RFC3339),
				})
			}
		}
	}

	if s.taskRepo != nil {
		tasks, err := s.taskRepo.List(ctx, portrepos.TaskFilter{
			Scope:   entities.ScopeUser,
			OwnerID: userID,
		})
		if err == nil {
			for _, t := range tasks {
				export.Tasks = append(export.Tasks, taskExport{
					ID:        t.ID(),
					Title:     t.Title(),
					Status:    string(t.Status()),
					TaskType:  string(t.TaskType()),
					SessionID: t.SessionID(),
					CreatedAt: t.CreatedAt().Format(time.RFC3339),
					UpdatedAt: t.UpdatedAt().Format(time.RFC3339),
				})
			}
		}
	}

	if s.taskGroupRepo != nil {
		groups, err := s.taskGroupRepo.List(ctx, portrepos.TaskGroupFilter{
			Scope:   entities.ScopeUser,
			OwnerID: userID,
		})
		if err == nil {
			for _, g := range groups {
				export.TaskGroups = append(export.TaskGroups, taskGroupExport{
					ID:          g.ID(),
					Name:        g.Name(),
					Description: g.Description(),
					CreatedAt:   g.CreatedAt().Format(time.RFC3339),
					UpdatedAt:   g.UpdatedAt().Format(time.RFC3339),
				})
			}
		}
	}

	if s.userFileRepo != nil {
		ufiles, err := s.userFileRepo.List(ctx, userID)
		if err == nil {
			for _, f := range ufiles {
				encContent, encErr := EncryptField(dek, f.Content())
				if encErr != nil {
					log.Printf("[SYNC] Warning: encrypt file %s content: %v", f.Name(), encErr)
					encContent = ""
				}
				export.Files = append(export.Files, userFileExport{
					ID:          f.ID(),
					Name:        f.Name(),
					Path:        f.Path(),
					Content:     encContent,
					Permissions: f.Permissions(),
					CreatedAt:   f.CreatedAt().Format(time.RFC3339),
					UpdatedAt:   f.UpdatedAt().Format(time.RFC3339),
				})
			}
		}
	}

	data, err := yaml.Marshal(export)
	if err != nil {
		return fmt.Errorf("serialize: %w", err)
	}
	files[rootPath+"user-resources.yaml"] = data
	return nil
}

// importFile dispatches a file to the appropriate import handler.
func (s *Syncer) importFile(ctx context.Context, relPath string, content []byte, settingsName, userID string, dek []byte) error {
	switch relPath {
	case "resources.yaml":
		return s.importTeamResources(ctx, content, settingsName, userID, dek)
	case "user-resources.yaml":
		return s.importUserResources(ctx, content, userID, dek)
	default:
		return nil
	}
}

func (s *Syncer) importTeamResources(ctx context.Context, data []byte, settingsName, userID string, dek []byte) error {
	var resources importexport.TeamResources
	if err := yaml.Unmarshal(data, &resources); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	if err := decryptTeamResourcesFields(&resources, dek); err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}

	noopSvc := infraservices.NewNoopEncryptionService()
	importer := importexport.NewImporter(s.scheduleRepo, s.webhookRepo, s.settingsRepo, noopSvc)
	_, err := importer.Import(ctx, &resources, userID, importexport.ImportOptions{
		Mode:           importexport.ImportModeUpsert,
		IDField:        "name",
		AllowPartial:   true,
		SkipValidation: true,
	})
	return err
}

func (s *Syncer) importUserResources(ctx context.Context, data []byte, userID string, dek []byte) error {
	type userImport struct {
		Memories []memoryExport   `yaml:"memories"`
		Files    []userFileExport `yaml:"files"`
	}

	var imp userImport
	if err := yaml.Unmarshal(data, &imp); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	if s.memoryRepo != nil {
		for _, m := range imp.Memories {
			scope := entities.ResourceScope(m.Scope)
			if scope == "" {
				scope = entities.ScopeUser
			}
			mem := entities.NewMemoryWithTags(m.ID, m.Title, m.Content, scope, userID, "", m.Tags)
			// Try Update first to preserve the exported ID (no-op if already up-to-date).
			// Fall back to Create only when the entry doesn't exist (404).
			if updateErr := s.memoryRepo.Update(ctx, mem); updateErr != nil {
				if createErr := s.memoryRepo.Create(ctx, mem); createErr != nil {
					log.Printf("[SYNC] Warning: upsert memory %s: update=%v create=%v", m.ID, updateErr, createErr)
				}
			}
		}
	}

	if s.userFileRepo != nil {
		for _, f := range imp.Files {
			plainContent := f.Content
			if IsEncrypted(plainContent) {
				dec, err := DecryptField(dek, plainContent)
				if err != nil {
					log.Printf("[SYNC] Warning: decrypt file %s content: %v", f.Name, err)
					continue
				}
				plainContent = dec
			}
			uf := entities.NewUserFile(f.ID, f.Name, f.Path, plainContent, f.Permissions)
			if err := s.userFileRepo.Save(ctx, userID, uf); err != nil {
				log.Printf("[SYNC] Warning: save file %s: %v", f.Name, err)
			}
		}
	}

	return nil
}

// encryptTeamResourcesFields encrypts sensitive plaintext fields using the fixed DEK.
func encryptTeamResourcesFields(r *importexport.TeamResources, dek []byte) error {
	encryptMapValues := func(m map[string]string, context string) error {
		for k, v := range m {
			if IsSensitiveKey(k) && !IsEncrypted(v) {
				enc, err := EncryptField(dek, v)
				if err != nil {
					return fmt.Errorf("encrypt %s %s: %w", context, k, err)
				}
				m[k] = enc
			}
		}
		return nil
	}

	for i := range r.Schedules {
		sc := &r.Schedules[i].SessionConfig
		if err := encryptMapValues(sc.Environment, "schedule env"); err != nil {
			return err
		}
		sc.EnvironmentEncrypted = nil
	}

	for i := range r.Webhooks {
		w := &r.Webhooks[i]
		if w.Secret != "" && !IsEncrypted(w.Secret) {
			enc, err := EncryptField(dek, w.Secret)
			if err != nil {
				return fmt.Errorf("encrypt webhook secret: %w", err)
			}
			w.Secret = enc
		}
		w.SecretEncrypted = nil

		if sc := w.SessionConfig; sc != nil {
			if err := encryptMapValues(sc.Environment, "webhook env"); err != nil {
				return err
			}
			sc.EnvironmentEncrypted = nil
		}
		for j := range w.Triggers {
			if sc := w.Triggers[j].SessionConfig; sc != nil {
				if err := encryptMapValues(sc.Environment, "trigger env"); err != nil {
					return err
				}
				sc.EnvironmentEncrypted = nil
			}
		}
	}

	if s := r.Settings; s != nil {
		if s.ClaudeCodeOAuthToken != "" && !IsEncrypted(s.ClaudeCodeOAuthToken) {
			enc, err := EncryptField(dek, s.ClaudeCodeOAuthToken)
			if err != nil {
				return fmt.Errorf("encrypt oauth token: %w", err)
			}
			s.ClaudeCodeOAuthToken = enc
		}
		s.ClaudeCodeOAuthTokenEncrypted = nil

		if b := s.Bedrock; b != nil {
			if b.SecretAccessKey != "" && !IsEncrypted(b.SecretAccessKey) {
				enc, err := EncryptField(dek, b.SecretAccessKey)
				if err != nil {
					return fmt.Errorf("encrypt bedrock secret key: %w", err)
				}
				b.SecretAccessKey = enc
			}
			b.SecretAccessKeyEncrypted = nil
			b.AccessKeyIDEncrypted = nil
		}

		for _, mcp := range s.MCPServers {
			if err := encryptMapValues(mcp.Env, "MCP env"); err != nil {
				return err
			}
			mcp.EnvEncrypted = nil
			if err := encryptMapValues(mcp.Headers, "MCP header"); err != nil {
				return err
			}
			mcp.HeadersEncrypted = nil
		}
	}

	return nil
}

// decryptTeamResourcesFields decrypts enc:v1: values in TeamResources.
func decryptTeamResourcesFields(r *importexport.TeamResources, dek []byte) error {
	decryptMapValues := func(m map[string]string, context string) error {
		for k, v := range m {
			if IsEncrypted(v) {
				plain, err := DecryptField(dek, v)
				if err != nil {
					return fmt.Errorf("decrypt %s %s: %w", context, k, err)
				}
				m[k] = plain
			}
		}
		return nil
	}

	for i := range r.Schedules {
		if err := decryptMapValues(r.Schedules[i].SessionConfig.Environment, "schedule env"); err != nil {
			return err
		}
	}

	for i := range r.Webhooks {
		w := &r.Webhooks[i]
		if IsEncrypted(w.Secret) {
			plain, err := DecryptField(dek, w.Secret)
			if err != nil {
				return fmt.Errorf("decrypt webhook secret: %w", err)
			}
			w.Secret = plain
		}
		if sc := w.SessionConfig; sc != nil {
			if err := decryptMapValues(sc.Environment, "webhook env"); err != nil {
				return err
			}
		}
		for j := range w.Triggers {
			if sc := w.Triggers[j].SessionConfig; sc != nil {
				if err := decryptMapValues(sc.Environment, "trigger env"); err != nil {
					return err
				}
			}
		}
	}

	if s := r.Settings; s != nil {
		if IsEncrypted(s.ClaudeCodeOAuthToken) {
			plain, err := DecryptField(dek, s.ClaudeCodeOAuthToken)
			if err != nil {
				return fmt.Errorf("decrypt oauth token: %w", err)
			}
			s.ClaudeCodeOAuthToken = plain
		}
		if b := s.Bedrock; b != nil {
			if IsEncrypted(b.SecretAccessKey) {
				plain, err := DecryptField(dek, b.SecretAccessKey)
				if err != nil {
					return fmt.Errorf("decrypt bedrock secret key: %w", err)
				}
				b.SecretAccessKey = plain
			}
		}
		for _, mcp := range s.MCPServers {
			if err := decryptMapValues(mcp.Env, "MCP env"); err != nil {
				return err
			}
			if err := decryptMapValues(mcp.Headers, "MCP header"); err != nil {
				return err
			}
		}
	}

	return nil
}

// reencryptYAML decrypts all enc:v1: values with oldDEK and re-encrypts with newDEK.
func reencryptYAML(data, oldDEK, newDEK []byte) ([]byte, error) {
	var root interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return data, nil // not YAML — pass through unchanged
	}
	reencrypted := reencryptValue(root, oldDEK, newDEK)
	return yaml.Marshal(reencrypted)
}

func reencryptValue(v interface{}, oldDEK, newDEK []byte) interface{} {
	switch val := v.(type) {
	case string:
		if !IsEncrypted(val) {
			return val
		}
		plain, err := DecryptField(oldDEK, val)
		if err != nil {
			return val
		}
		enc, err := EncryptField(newDEK, plain)
		if err != nil {
			return val
		}
		return enc
	case map[string]interface{}:
		for k, child := range val {
			val[k] = reencryptValue(child, oldDEK, newDEK)
		}
		return val
	case []interface{}:
		for i, child := range val {
			val[i] = reencryptValue(child, oldDEK, newDEK)
		}
		return val
	default:
		return v
	}
}

// --- DTO types for user-scoped resource export ---

type memoryExport struct {
	ID        string            `yaml:"id"`
	Title     string            `yaml:"title"`
	Content   string            `yaml:"content"`
	Scope     string            `yaml:"scope"`
	Tags      map[string]string `yaml:"tags,omitempty"`
	CreatedAt string            `yaml:"created_at"`
	UpdatedAt string            `yaml:"updated_at"`
}

type taskExport struct {
	ID        string `yaml:"id"`
	Title     string `yaml:"title"`
	Status    string `yaml:"status"`
	TaskType  string `yaml:"task_type"`
	SessionID string `yaml:"session_id,omitempty"`
	CreatedAt string `yaml:"created_at"`
	UpdatedAt string `yaml:"updated_at"`
}

type taskGroupExport struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	CreatedAt   string `yaml:"created_at"`
	UpdatedAt   string `yaml:"updated_at"`
}

type userFileExport struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Path        string `yaml:"path"`
	Content     string `yaml:"content"` // always encrypted with DEK
	Permissions string `yaml:"permissions,omitempty"`
	CreatedAt   string `yaml:"created_at"`
	UpdatedAt   string `yaml:"updated_at"`
}

// keep errors import satisfied
var _ = errors.New
