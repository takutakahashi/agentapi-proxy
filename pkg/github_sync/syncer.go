package githubsync

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	infraservices "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	importexport "github.com/takutakahashi/agentapi-proxy/pkg/import"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
	"gopkg.in/yaml.v3"
)

// Syncer orchestrates bidirectional GitHub sync for a user or team.
//
// File layout in GitHub:
//
//	<rootPath>/teams/<settingsName>/schedules/<id>.yaml
//	<rootPath>/teams/<settingsName>/webhooks/<id>.yaml
//	<rootPath>/teams/<settingsName>/settings.yaml
//	<rootPath>/users/<userID>/files/<id>.yaml
//	<rootPath>/.sync-meta.yaml
//
// A push from a personal settings (settingsName == userID) exports only
// user resources; a push from a team settings exports only team resources.
type Syncer struct {
	settingsRepo portrepos.SettingsRepository
	scheduleRepo schedule.Manager
	webhookRepo  portrepos.WebhookRepository
	slackbotRepo portrepos.SlackBotRepository
	userFileRepo portrepos.UserFileRepository
}

// NewSyncer creates a Syncer. Non-nil repos are synced.
func NewSyncer(
	settingsRepo portrepos.SettingsRepository,
	scheduleRepo schedule.Manager,
	webhookRepo portrepos.WebhookRepository,
	_ portrepos.MemoryRepository,   // unused — kept for call-site compatibility
	_ portrepos.TaskRepository,     // unused
	_ portrepos.TaskGroupRepository, // unused
	userFileRepo portrepos.UserFileRepository,
	slackbotRepo portrepos.SlackBotRepository,
) *Syncer {
	return &Syncer{
		settingsRepo: settingsRepo,
		scheduleRepo: scheduleRepo,
		webhookRepo:  webhookRepo,
		slackbotRepo: slackbotRepo,
		userFileRepo: userFileRepo,
	}
}

// isPersonalSync returns true when settingsName refers to the caller's own
// personal settings (as opposed to a team/shared settings name).
func isPersonalSync(settingsName, userID string) bool {
	return settingsName == userID
}

// Push exports all resources for settingsName and commits them to GitHub.
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

	if isPersonalSync(settingsName, userID) {
		if err := s.exportUserFiles(ctx, userID, dek, rootPath, files); err != nil {
			log.Printf("[SYNC] user file export warning for %s: %v", settingsName, err)
		}
		if err := s.exportUserSchedules(ctx, userID, dek, rootPath, files); err != nil {
			log.Printf("[SYNC] user schedule export warning for %s: %v", settingsName, err)
		}
		if err := s.exportUserWebhooks(ctx, userID, dek, rootPath, files); err != nil {
			log.Printf("[SYNC] user webhook export warning for %s: %v", settingsName, err)
		}
		if err := s.exportUserSlackbots(ctx, userID, dek, rootPath, files); err != nil {
			log.Printf("[SYNC] user slackbot export warning for %s: %v", settingsName, err)
		}
	} else {
		if err := s.exportTeamSchedules(ctx, settingsName, userID, dek, rootPath, files); err != nil {
			log.Printf("[SYNC] schedule export warning for %s: %v", settingsName, err)
		}
		if err := s.exportTeamWebhooks(ctx, settingsName, userID, dek, rootPath, files); err != nil {
			log.Printf("[SYNC] webhook export warning for %s: %v", settingsName, err)
		}
		if err := s.exportTeamSettings(ctx, settingsName, userID, dek, rootPath, files); err != nil {
			log.Printf("[SYNC] settings export warning for %s: %v", settingsName, err)
		}
		if err := s.exportTeamSlackbots(ctx, settingsName, dek, rootPath, files); err != nil {
			log.Printf("[SYNC] slackbot export warning for %s: %v", settingsName, err)
		}
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

	personal := isPersonalSync(settingsName, userID)
	teamPrefix := rootPath + "teams/" + settingsName + "/"
	userPrefix := rootPath + "users/" + userID + "/"

	filesWritten := 0
	for _, filePath := range paths {
		rel := strings.TrimPrefix(filePath, rootPath)
		if strings.HasSuffix(rel, ".sync-meta.yaml") {
			continue
		}

		// Route by scope: personal sync only imports user files; team sync imports team resources.
		var relevant bool
		if personal {
			relevant = strings.HasPrefix(filePath, userPrefix)
		} else {
			relevant = strings.HasPrefix(filePath, teamPrefix)
		}
		if !relevant {
			continue
		}

		content, err := ghClient.GetFile(ctx, cfg.Branch, filePath)
		if err != nil {
			log.Printf("[SYNC] Warning: failed to get %s: %v", filePath, err)
			continue
		}

		if err := s.importFileByPath(ctx, filePath, content, settingsName, userID, dek,
			teamPrefix, userPrefix); err != nil {
			log.Printf("[SYNC] Warning: failed to import %s: %v", filePath, err)
			continue
		}
		filesWritten++
	}

	return &PullResponse{
		PulledAt: time.Now(),
		Summary:  SyncSummary{FilesWritten: filesWritten},
	}, nil
}

// importFileByPath routes a single GitHub file to the right import handler.
func (s *Syncer) importFileByPath(ctx context.Context, filePath string, content []byte,
	settingsName, userID string, dek []byte, teamPrefix, userPrefix string) error {

	switch {
	// Team sync paths
	case strings.HasPrefix(filePath, teamPrefix+"schedules/"):
		return s.importScheduleFile(ctx, content, settingsName, userID, dek)
	case strings.HasPrefix(filePath, teamPrefix+"webhooks/"):
		return s.importWebhookFile(ctx, content, settingsName, userID, dek)
	case filePath == teamPrefix+"settings.yaml":
		return s.importSettingsFile(ctx, content, settingsName, userID, dek)
	case strings.HasPrefix(filePath, teamPrefix+"slackbots/"):
		return s.importSlackbotFile(ctx, content, entities.ScopeTeam, userID, settingsName, dek)
	// Personal sync paths
	case strings.HasPrefix(filePath, userPrefix+"files/"):
		return s.importUserFileRecord(ctx, content, userID, dek)
	case strings.HasPrefix(filePath, userPrefix+"schedules/"):
		return s.importUserScheduleFile(ctx, content, userID, dek)
	case strings.HasPrefix(filePath, userPrefix+"webhooks/"):
		return s.importUserWebhookFile(ctx, content, userID, dek)
	case strings.HasPrefix(filePath, userPrefix+"slackbots/"):
		return s.importSlackbotFile(ctx, content, entities.ScopeUser, userID, "", dek)
	default:
		return nil
	}
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
	for _, p := range paths {
		content, err := ghClient.GetFile(ctx, cfg.Branch, p)
		if err != nil {
			return nil, fmt.Errorf("failed to get %s during rotation: %w", p, err)
		}
		reenc, err := reencryptYAML(content, oldDEK, newDEK)
		if err != nil {
			return nil, fmt.Errorf("failed to re-encrypt %s: %w", p, err)
		}
		files[p] = reenc
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

// --- Team resource export ---

func (s *Syncer) exportTeamSchedules(ctx context.Context, settingsName, userID string, dek []byte, rootPath string, files map[string][]byte) error {
	if s.scheduleRepo == nil {
		return nil
	}
	noopSvc := infraservices.NewNoopEncryptionService()
	exporter := importexport.NewExporter(s.scheduleRepo, s.webhookRepo, s.settingsRepo, noopSvc)
	resources, err := exporter.Export(ctx, settingsName, userID, importexport.ExportOptions{
		Format:         importexport.ExportFormatYAML,
		IncludeSecrets: true,
	})
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}

	dir := rootPath + "teams/" + settingsName + "/schedules/"
	for _, sc := range resources.Schedules {
		id := sc.ID
		if id == "" {
			id = sc.Name
		}
		wrapper := &importexport.TeamResources{
			Metadata:  resources.Metadata,
			Schedules: []importexport.ScheduleImport{sc},
		}
		if err := encryptTeamResourcesFields(wrapper, dek); err != nil {
			log.Printf("[SYNC] Warning: encrypt schedule %s: %v", id, err)
			continue
		}
		data, err := yaml.Marshal(wrapper.Schedules[0])
		if err != nil {
			log.Printf("[SYNC] Warning: marshal schedule %s: %v", id, err)
			continue
		}
		files[dir+id+".yaml"] = data
	}
	return nil
}

func (s *Syncer) exportTeamWebhooks(ctx context.Context, settingsName, userID string, dek []byte, rootPath string, files map[string][]byte) error {
	if s.webhookRepo == nil {
		return nil
	}
	noopSvc := infraservices.NewNoopEncryptionService()
	exporter := importexport.NewExporter(s.scheduleRepo, s.webhookRepo, s.settingsRepo, noopSvc)
	resources, err := exporter.Export(ctx, settingsName, userID, importexport.ExportOptions{
		Format:         importexport.ExportFormatYAML,
		IncludeSecrets: true,
	})
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}

	dir := rootPath + "teams/" + settingsName + "/webhooks/"
	for _, wh := range resources.Webhooks {
		id := wh.ID
		if id == "" {
			id = wh.Name
		}
		wrapper := &importexport.TeamResources{
			Metadata: resources.Metadata,
			Webhooks: []importexport.WebhookImport{wh},
		}
		if err := encryptTeamResourcesFields(wrapper, dek); err != nil {
			log.Printf("[SYNC] Warning: encrypt webhook %s: %v", id, err)
			continue
		}
		data, err := yaml.Marshal(wrapper.Webhooks[0])
		if err != nil {
			log.Printf("[SYNC] Warning: marshal webhook %s: %v", id, err)
			continue
		}
		files[dir+id+".yaml"] = data
	}
	return nil
}

func (s *Syncer) exportTeamSettings(ctx context.Context, settingsName, userID string, dek []byte, rootPath string, files map[string][]byte) error {
	if s.settingsRepo == nil {
		return nil
	}
	noopSvc := infraservices.NewNoopEncryptionService()
	exporter := importexport.NewExporter(s.scheduleRepo, s.webhookRepo, s.settingsRepo, noopSvc)
	resources, err := exporter.Export(ctx, settingsName, userID, importexport.ExportOptions{
		Format:         importexport.ExportFormatYAML,
		IncludeSecrets: true,
	})
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	if resources.Settings == nil {
		return nil
	}

	wrapper := &importexport.TeamResources{
		Metadata: resources.Metadata,
		Settings: resources.Settings,
	}
	if err := encryptTeamResourcesFields(wrapper, dek); err != nil {
		return fmt.Errorf("encrypt settings: %w", err)
	}
	data, err := yaml.Marshal(wrapper.Settings)
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	files[rootPath+"teams/"+settingsName+"/settings.yaml"] = data
	return nil
}

// --- User resource export ---

func (s *Syncer) exportUserFiles(ctx context.Context, userID string, dek []byte, rootPath string, files map[string][]byte) error {
	if s.userFileRepo == nil {
		return nil
	}
	ufiles, err := s.userFileRepo.List(ctx, userID)
	if err != nil {
		return fmt.Errorf("list user files: %w", err)
	}

	dir := rootPath + "users/" + userID + "/files/"
	for _, f := range ufiles {
		encContent, encErr := EncryptField(dek, f.Content())
		if encErr != nil {
			log.Printf("[SYNC] Warning: encrypt file %s: %v", f.Name(), encErr)
			encContent = ""
		}
		rec := userFileRecord{
			ID:          f.ID(),
			Name:        f.Name(),
			Path:        f.Path(),
			Content:     encContent,
			Permissions: f.Permissions(),
			CreatedAt:   f.CreatedAt().Format(time.RFC3339),
			UpdatedAt:   f.UpdatedAt().Format(time.RFC3339),
		}
		data, err := yaml.Marshal(rec)
		if err != nil {
			log.Printf("[SYNC] Warning: marshal file %s: %v", f.Name(), err)
			continue
		}
		filename := f.ID()
		if filename == "" {
			filename = sanitizeFilename(f.Name())
		}
		files[dir+filename+".yaml"] = data
	}
	return nil
}

func (s *Syncer) exportUserSchedules(ctx context.Context, userID string, dek []byte, rootPath string, files map[string][]byte) error {
	if s.scheduleRepo == nil {
		return nil
	}
	schedules, err := s.scheduleRepo.List(ctx, schedule.ScheduleFilter{
		Scope:  entities.ScopeUser,
		UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("list user schedules: %w", err)
	}
	dir := rootPath + "users/" + userID + "/schedules/"
	for _, sc := range schedules {
		si := scheduleToImport(sc)
		wrapper := &importexport.TeamResources{
			Metadata:  importexport.ResourceMetadata{TeamID: userID},
			Schedules: []importexport.ScheduleImport{si},
		}
		if err := encryptTeamResourcesFields(wrapper, dek); err != nil {
			log.Printf("[SYNC] Warning: encrypt user schedule %s: %v", sc.ID, err)
			continue
		}
		data, err := yaml.Marshal(wrapper.Schedules[0])
		if err != nil {
			log.Printf("[SYNC] Warning: marshal user schedule %s: %v", sc.ID, err)
			continue
		}
		files[dir+sc.ID+".yaml"] = data
	}
	return nil
}

func (s *Syncer) exportUserWebhooks(ctx context.Context, userID string, dek []byte, rootPath string, files map[string][]byte) error {
	if s.webhookRepo == nil {
		return nil
	}
	webhooks, err := s.webhookRepo.List(ctx, portrepos.WebhookFilter{
		Scope:  entities.ScopeUser,
		UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("list user webhooks: %w", err)
	}
	dir := rootPath + "users/" + userID + "/webhooks/"
	for _, wh := range webhooks {
		wi := webhookToImport(wh)
		wrapper := &importexport.TeamResources{
			Metadata: importexport.ResourceMetadata{TeamID: userID},
			Webhooks: []importexport.WebhookImport{wi},
		}
		if err := encryptTeamResourcesFields(wrapper, dek); err != nil {
			log.Printf("[SYNC] Warning: encrypt user webhook %s: %v", wh.ID(), err)
			continue
		}
		data, err := yaml.Marshal(wrapper.Webhooks[0])
		if err != nil {
			log.Printf("[SYNC] Warning: marshal user webhook %s: %v", wh.ID(), err)
			continue
		}
		files[dir+wh.ID()+".yaml"] = data
	}
	return nil
}

func (s *Syncer) exportUserSlackbots(ctx context.Context, userID string, dek []byte, rootPath string, files map[string][]byte) error {
	if s.slackbotRepo == nil {
		return nil
	}
	bots, err := s.slackbotRepo.List(ctx, portrepos.SlackBotFilter{
		Scope:  entities.ScopeUser,
		UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("list user slackbots: %w", err)
	}
	dir := rootPath + "users/" + userID + "/slackbots/"
	for _, bot := range bots {
		rec := slackbotToRecord(bot, dek)
		data, err := yaml.Marshal(rec)
		if err != nil {
			log.Printf("[SYNC] Warning: marshal user slackbot %s: %v", bot.ID(), err)
			continue
		}
		files[dir+bot.ID()+".yaml"] = data
	}
	return nil
}

func (s *Syncer) exportTeamSlackbots(ctx context.Context, settingsName string, dek []byte, rootPath string, files map[string][]byte) error {
	if s.slackbotRepo == nil {
		return nil
	}
	bots, err := s.slackbotRepo.List(ctx, portrepos.SlackBotFilter{
		Scope:  entities.ScopeTeam,
		TeamID: settingsName,
	})
	if err != nil {
		return fmt.Errorf("list team slackbots: %w", err)
	}
	dir := rootPath + "teams/" + settingsName + "/slackbots/"
	for _, bot := range bots {
		rec := slackbotToRecord(bot, dek)
		data, err := yaml.Marshal(rec)
		if err != nil {
			log.Printf("[SYNC] Warning: marshal team slackbot %s: %v", bot.ID(), err)
			continue
		}
		files[dir+bot.ID()+".yaml"] = data
	}
	return nil
}

// --- Import handlers ---

func (s *Syncer) importScheduleFile(ctx context.Context, data []byte, settingsName, userID string, dek []byte) error {
	var sc importexport.ScheduleImport
	if err := yaml.Unmarshal(data, &sc); err != nil {
		return fmt.Errorf("unmarshal schedule: %w", err)
	}
	wrapper := &importexport.TeamResources{
		Metadata:  importexport.ResourceMetadata{TeamID: settingsName},
		Schedules: []importexport.ScheduleImport{sc},
	}
	if err := decryptTeamResourcesFields(wrapper, dek); err != nil {
		return fmt.Errorf("decrypt schedule: %w", err)
	}
	noopSvc := infraservices.NewNoopEncryptionService()
	importer := importexport.NewImporter(s.scheduleRepo, s.webhookRepo, s.settingsRepo, noopSvc)
	_, err := importer.Import(ctx, wrapper, userID, importexport.ImportOptions{
		Mode:           importexport.ImportModeUpsert,
		IDField:        "name",
		AllowPartial:   true,
		SkipValidation: true,
	})
	return err
}

func (s *Syncer) importWebhookFile(ctx context.Context, data []byte, settingsName, userID string, dek []byte) error {
	var wh importexport.WebhookImport
	if err := yaml.Unmarshal(data, &wh); err != nil {
		return fmt.Errorf("unmarshal webhook: %w", err)
	}
	wrapper := &importexport.TeamResources{
		Metadata: importexport.ResourceMetadata{TeamID: settingsName},
		Webhooks: []importexport.WebhookImport{wh},
	}
	if err := decryptTeamResourcesFields(wrapper, dek); err != nil {
		return fmt.Errorf("decrypt webhook: %w", err)
	}
	noopSvc := infraservices.NewNoopEncryptionService()
	importer := importexport.NewImporter(s.scheduleRepo, s.webhookRepo, s.settingsRepo, noopSvc)
	_, err := importer.Import(ctx, wrapper, userID, importexport.ImportOptions{
		Mode:           importexport.ImportModeUpsert,
		IDField:        "name",
		AllowPartial:   true,
		SkipValidation: true,
	})
	return err
}

func (s *Syncer) importSettingsFile(ctx context.Context, data []byte, settingsName, userID string, dek []byte) error {
	var si importexport.SettingsImport
	if err := yaml.Unmarshal(data, &si); err != nil {
		return fmt.Errorf("unmarshal settings: %w", err)
	}
	wrapper := &importexport.TeamResources{
		Metadata: importexport.ResourceMetadata{TeamID: settingsName},
		Settings: &si,
	}
	if err := decryptTeamResourcesFields(wrapper, dek); err != nil {
		return fmt.Errorf("decrypt settings: %w", err)
	}
	noopSvc := infraservices.NewNoopEncryptionService()
	importer := importexport.NewImporter(s.scheduleRepo, s.webhookRepo, s.settingsRepo, noopSvc)
	_, err := importer.Import(ctx, wrapper, userID, importexport.ImportOptions{
		Mode:           importexport.ImportModeUpsert,
		IDField:        "name",
		AllowPartial:   true,
		SkipValidation: true,
	})
	return err
}

func (s *Syncer) importUserFileRecord(ctx context.Context, data []byte, userID string, dek []byte) error {
	if s.userFileRepo == nil {
		return nil
	}
	var rec userFileRecord
	if err := yaml.Unmarshal(data, &rec); err != nil {
		return fmt.Errorf("unmarshal file record: %w", err)
	}
	plainContent := rec.Content
	if IsEncrypted(plainContent) {
		dec, err := DecryptField(dek, plainContent)
		if err != nil {
			return fmt.Errorf("decrypt file %s: %w", rec.Name, err)
		}
		plainContent = dec
	}
	uf := newUserFileFromRecord(rec, plainContent)
	if err := s.userFileRepo.Save(ctx, userID, uf); err != nil {
		return fmt.Errorf("save file %s: %w", rec.Name, err)
	}
	return nil
}

func (s *Syncer) importUserScheduleFile(ctx context.Context, data []byte, userID string, dek []byte) error {
	var si importexport.ScheduleImport
	if err := yaml.Unmarshal(data, &si); err != nil {
		return fmt.Errorf("unmarshal schedule: %w", err)
	}
	wrapper := &importexport.TeamResources{
		Metadata:  importexport.ResourceMetadata{TeamID: userID},
		Schedules: []importexport.ScheduleImport{si},
	}
	if err := decryptTeamResourcesFields(wrapper, dek); err != nil {
		return fmt.Errorf("decrypt schedule: %w", err)
	}
	si = wrapper.Schedules[0]

	// Find existing user-scoped schedule by ID or name
	var existing *schedule.Schedule
	if schedules, err := s.scheduleRepo.List(ctx, schedule.ScheduleFilter{
		Scope:  entities.ScopeUser,
		UserID: userID,
	}); err == nil {
		for _, sc := range schedules {
			if si.ID != "" && sc.ID == si.ID {
				existing = sc
				break
			}
		}
		if existing == nil {
			for _, sc := range schedules {
				if sc.Name == si.Name {
					existing = sc
					break
				}
			}
		}
	}

	id := si.ID
	if id == "" {
		id = uuid.New().String()
	}
	var sc *schedule.Schedule
	if existing != nil {
		sc = existing
	} else {
		sc = &schedule.Schedule{ID: id}
	}
	sc.Name = si.Name
	sc.UserID = userID
	sc.Scope = entities.ScopeUser
	sc.TeamID = ""
	if si.Status != "" {
		sc.Status = schedule.ScheduleStatus(si.Status)
	} else {
		sc.Status = schedule.ScheduleStatusActive
	}
	sc.ScheduledAt = si.ScheduledAt
	sc.CronExpr = si.CronExpr
	sc.Timezone = si.Timezone
	sc.SessionConfig = schedule.SessionConfig{
		Tags:        si.SessionConfig.Tags,
		Environment: si.SessionConfig.Environment,
	}
	if si.SessionConfig.Params != nil {
		sc.SessionConfig.Params = &entities.SessionParams{
			Message: si.SessionConfig.Params.InitialMessage,
		}
	}

	if existing != nil {
		return s.scheduleRepo.Update(ctx, sc)
	}
	return s.scheduleRepo.Create(ctx, sc)
}

func (s *Syncer) importUserWebhookFile(ctx context.Context, data []byte, userID string, dek []byte) error {
	var wi importexport.WebhookImport
	if err := yaml.Unmarshal(data, &wi); err != nil {
		return fmt.Errorf("unmarshal webhook: %w", err)
	}
	wrapper := &importexport.TeamResources{
		Metadata: importexport.ResourceMetadata{TeamID: userID},
		Webhooks: []importexport.WebhookImport{wi},
	}
	if err := decryptTeamResourcesFields(wrapper, dek); err != nil {
		return fmt.Errorf("decrypt webhook: %w", err)
	}
	noopSvc := infraservices.NewNoopEncryptionService()
	importer := importexport.NewImporter(s.scheduleRepo, s.webhookRepo, s.settingsRepo, noopSvc)
	_, err := importer.Import(ctx, wrapper, userID, importexport.ImportOptions{
		Mode:           importexport.ImportModeUpsert,
		IDField:        "name",
		AllowPartial:   true,
		SkipValidation: true,
	})
	return err
}

func (s *Syncer) importSlackbotFile(ctx context.Context, data []byte, scope entities.ResourceScope, userID, teamID string, dek []byte) error {
	if s.slackbotRepo == nil {
		return nil
	}
	var rec slackbotRecord
	if err := yaml.Unmarshal(data, &rec); err != nil {
		return fmt.Errorf("unmarshal slackbot: %w", err)
	}
	// Decrypt environment
	env := make(map[string]string, len(rec.Environment))
	for k, v := range rec.Environment {
		if IsEncrypted(v) {
			plain, err := DecryptField(dek, v)
			if err != nil {
				return fmt.Errorf("decrypt slackbot env %s: %w", k, err)
			}
			env[k] = plain
		} else {
			env[k] = v
		}
	}

	id := rec.ID
	if id == "" {
		id = uuid.New().String()
	}
	existing, _ := s.slackbotRepo.Get(ctx, id)

	bot := existing
	if bot == nil {
		bot = entities.NewSlackBot(id, rec.Name, userID)
	}
	bot.SetScope(scope)
	if teamID != "" {
		bot.SetTeamID(teamID)
	}
	if rec.Status != "" {
		bot.SetStatus(entities.SlackBotStatus(rec.Status))
	}
	if rec.BotTokenSecretName != "" {
		bot.SetBotTokenSecretName(rec.BotTokenSecretName)
	}
	if rec.BotTokenSecretKey != "" {
		bot.SetBotTokenSecretKey(rec.BotTokenSecretKey)
	}
	if rec.AppTokenSecretKey != "" {
		bot.SetAppTokenSecretKey(rec.AppTokenSecretKey)
	}
	bot.SetAllowedEventTypes(rec.AllowedEventTypes)
	bot.SetAllowedChannelNames(rec.AllowedChannelNames)
	bot.SetAllowedUserIDs(rec.AllowedUserIDs)
	if rec.MaxSessions > 0 {
		bot.SetMaxSessions(rec.MaxSessions)
	}
	bot.SetNotifyOnSessionCreated(rec.NotifyOnSession)
	bot.SetAllowBotMessages(rec.AllowBotMessages)
	if len(env) > 0 || len(rec.Tags) > 0 {
		sc := entities.NewWebhookSessionConfig()
		sc.SetEnvironment(env)
		sc.SetTags(rec.Tags)
		bot.SetSessionConfig(sc)
	}

	if existing != nil {
		return s.slackbotRepo.Update(ctx, bot)
	}
	return s.slackbotRepo.Create(ctx, bot)
}

// --- Encryption helpers ---

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
		return data, nil
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

// --- DTO types ---

type userFileRecord struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Path        string `yaml:"path"`
	Content     string `yaml:"content"` // always encrypted with DEK
	Permissions string `yaml:"permissions,omitempty"`
	CreatedAt   string `yaml:"created_at,omitempty"`
	UpdatedAt   string `yaml:"updated_at,omitempty"`
}

// scheduleToImport converts a schedule to ScheduleImport with plaintext environment.
// Call encryptTeamResourcesFields afterward to encrypt sensitive values.
func scheduleToImport(sc *schedule.Schedule) importexport.ScheduleImport {
	si := importexport.ScheduleImport{
		ID:          sc.ID,
		Name:        sc.Name,
		Status:      string(sc.Status),
		ScheduledAt: sc.ScheduledAt,
		CronExpr:    sc.CronExpr,
		Timezone:    sc.Timezone,
		SessionConfig: importexport.SessionConfigImport{
			Tags:        sc.SessionConfig.Tags,
			Environment: sc.SessionConfig.Environment,
		},
	}
	if sc.SessionConfig.Params != nil {
		si.SessionConfig.Params = &importexport.SessionParamsImport{
			InitialMessage: sc.SessionConfig.Params.Message,
		}
	}
	return si
}

// webhookToImport converts a Webhook entity to WebhookImport with plaintext secrets.
// Call encryptTeamResourcesFields afterward.
func webhookToImport(wh *entities.Webhook) importexport.WebhookImport {
	wi := importexport.WebhookImport{
		ID:              wh.ID(),
		Name:            wh.Name(),
		Status:          string(wh.Status()),
		WebhookType:     string(wh.WebhookType()),
		Secret:          wh.Secret(),
		SignatureHeader: wh.SignatureHeader(),
		SignatureType:   string(wh.SignatureType()),
		SignaturePrefix: wh.SignaturePrefix(),
		MaxSessions:     wh.MaxSessions(),
		Triggers:        []importexport.WebhookTriggerImport{},
	}
	if gh := wh.GitHub(); gh != nil {
		wi.GitHub = &importexport.GitHubConfigImport{
			EnterpriseURL:       gh.EnterpriseURL(),
			AllowedEvents:       gh.AllowedEvents(),
			AllowedRepositories: gh.AllowedRepositories(),
		}
	}
	if sc := wh.SessionConfig(); sc != nil {
		wi.SessionConfig = &importexport.SessionConfigImport{
			Environment: sc.Environment(),
			Tags:        sc.Tags(),
		}
	}
	for _, t := range wh.Triggers() {
		ti := importexport.WebhookTriggerImport{
			Name:        t.Name(),
			Priority:    t.Priority(),
			Enabled:     t.Enabled(),
			StopOnMatch: t.StopOnMatch(),
		}
		cond := t.Conditions()
		if cond.GitHub() != nil {
			gh := cond.GitHub()
			ti.Conditions.GitHub = &importexport.GitHubConditionsImport{
				Events:       gh.Events(),
				Actions:      gh.Actions(),
				Branches:     gh.Branches(),
				Repositories: gh.Repositories(),
				Labels:       gh.Labels(),
				Paths:        gh.Paths(),
				BaseBranches: gh.BaseBranches(),
				Draft:        gh.Draft(),
				Sender:       gh.Sender(),
			}
		}
		if tsc := t.SessionConfig(); tsc != nil {
			ti.SessionConfig = &importexport.SessionConfigImport{
				Environment: tsc.Environment(),
				Tags:        tsc.Tags(),
			}
		}
		wi.Triggers = append(wi.Triggers, ti)
	}
	return wi
}

type slackbotRecord struct {
	ID                  string            `yaml:"id"`
	Name                string            `yaml:"name"`
	UserID              string            `yaml:"user_id"`
	Scope               string            `yaml:"scope"`
	TeamID              string            `yaml:"team_id,omitempty"`
	Status              string            `yaml:"status"`
	BotTokenSecretName  string            `yaml:"bot_token_secret_name,omitempty"`
	BotTokenSecretKey   string            `yaml:"bot_token_secret_key,omitempty"`
	AppTokenSecretKey   string            `yaml:"app_token_secret_key,omitempty"`
	AllowedEventTypes   []string          `yaml:"allowed_event_types,omitempty"`
	AllowedChannelNames []string          `yaml:"allowed_channel_names,omitempty"`
	AllowedUserIDs      []string          `yaml:"allowed_user_ids,omitempty"`
	Environment         map[string]string `yaml:"environment,omitempty"`
	Tags                map[string]string `yaml:"tags,omitempty"`
	MaxSessions         int               `yaml:"max_sessions,omitempty"`
	NotifyOnSession     *bool             `yaml:"notify_on_session_created,omitempty"`
	AllowBotMessages    *bool             `yaml:"allow_bot_messages,omitempty"`
	CreatedAt           string            `yaml:"created_at,omitempty"`
	UpdatedAt           string            `yaml:"updated_at,omitempty"`
}

func slackbotToRecord(bot *entities.SlackBot, dek []byte) slackbotRecord {
	rec := slackbotRecord{
		ID:                  bot.ID(),
		Name:                bot.Name(),
		UserID:              bot.UserID(),
		Scope:               string(bot.Scope()),
		TeamID:              bot.TeamID(),
		Status:              string(bot.Status()),
		BotTokenSecretName:  bot.BotTokenSecretName(),
		BotTokenSecretKey:   bot.BotTokenSecretKey(),
		AppTokenSecretKey:   bot.AppTokenSecretKey(),
		AllowedEventTypes:   bot.AllowedEventTypes(),
		AllowedChannelNames: bot.AllowedChannelNames(),
		AllowedUserIDs:      bot.AllowedUserIDs(),
		MaxSessions:         bot.MaxSessions(),
		NotifyOnSession:     bot.RawNotifyOnSessionCreated(),
		AllowBotMessages:    bot.RawAllowBotMessages(),
		CreatedAt:           bot.CreatedAt().Format(time.RFC3339),
		UpdatedAt:           bot.UpdatedAt().Format(time.RFC3339),
	}
	if sc := bot.SessionConfig(); sc != nil {
		env := make(map[string]string, len(sc.Environment()))
		for k, v := range sc.Environment() {
			if IsSensitiveKey(k) && !IsEncrypted(v) {
				if enc, err := EncryptField(dek, v); err == nil {
					env[k] = enc
					continue
				}
			}
			env[k] = v
		}
		rec.Environment = env
		rec.Tags = sc.Tags()
	}
	return rec
}

func newUserFileFromRecord(rec userFileRecord, content string) *entities.UserFile {
	uf := entities.NewUserFile(rec.ID, rec.Name, rec.Path, content, rec.Permissions)
	if t, err := time.Parse(time.RFC3339, rec.CreatedAt); err == nil {
		uf.SetCreatedAt(t)
	}
	if t, err := time.Parse(time.RFC3339, rec.UpdatedAt); err == nil {
		uf.SetUpdatedAt(t)
	}
	return uf
}

// sanitizeFilename replaces characters that are invalid in file paths.
func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return replacer.Replace(name)
}
