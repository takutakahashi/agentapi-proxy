package githubsync

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	infraservices "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	importexport "github.com/takutakahashi/agentapi-proxy/pkg/import"
	"github.com/takutakahashi/agentapi-proxy/pkg/startup"
	"gopkg.in/yaml.v3"
)

// Syncer orchestrates bidirectional GitHub sync for a user or team.
//
// File layout in GitHub:
//
//	<rootPath>/<settingsName>/schedules/<id>.yaml
//	<rootPath>/<settingsName>/webhooks/<id>.yaml
//	<rootPath>/<settingsName>/settings.yaml
//	<rootPath>/<settingsName>/files/<id>.yaml (personal only)
//	<rootPath>/<settingsName>/slackbots/<id>.yaml
//	<rootPath>/<settingsName>/session-profiles/<id>.yaml
//	<rootPath>/<settingsName>/.sync-meta.yaml
//
// For personal sync settingsName == userID; for team sync settingsName is the team name.
// Each settings has its own subdirectory under rootPath so multiple settings can share
// the same repository without conflicting.
type Syncer struct {
	settingsRepo       portrepos.SettingsRepository
	scheduleRepo       schedule.Manager
	webhookRepo        portrepos.WebhookRepository
	slackbotRepo       portrepos.SlackBotRepository
	userFileRepo       portrepos.UserFileRepository
	sessionProfileRepo portrepos.SessionProfileRepository
	githubAppInstallID string // fallback installation ID when no personal token
}

// NewSyncer creates a Syncer. Non-nil repos are synced.
// githubAppInstallID is used to generate a GitHub App installation token when
// a user has no personal GitHub token configured (empty string disables fallback).
func NewSyncer(
	settingsRepo portrepos.SettingsRepository,
	scheduleRepo schedule.Manager,
	webhookRepo portrepos.WebhookRepository,
	_ portrepos.MemoryRepository, // unused — kept for call-site compatibility
	_ portrepos.TaskRepository, // unused
	_ portrepos.TaskGroupRepository, // unused
	userFileRepo portrepos.UserFileRepository,
	slackbotRepo portrepos.SlackBotRepository,
	githubAppInstallID string,
) *Syncer {
	return &Syncer{
		settingsRepo:       settingsRepo,
		scheduleRepo:       scheduleRepo,
		webhookRepo:        webhookRepo,
		slackbotRepo:       slackbotRepo,
		userFileRepo:       userFileRepo,
		githubAppInstallID: githubAppInstallID,
	}
}

// SetSessionProfileRepository sets the session profile repository for sync
func (s *Syncer) SetSessionProfileRepository(repo portrepos.SessionProfileRepository) {
	s.sessionProfileRepo = repo
}

// isPersonalSync returns true when settingsName refers to the caller's own
// personal settings (as opposed to a team/shared settings name).
func isPersonalSync(settingsName, userID string) bool {
	return settingsName == userID
}

// resolveToken returns the GitHub token to use for sync operations.
// If personalToken is non-empty it is returned directly.
// Otherwise a short-lived GitHub App installation token is generated using
// GITHUB_APP_ID / GITHUB_APP_PEM env vars and the installation ID resolved as follows:
//  1. The proxy-level git_sync.github_app.installation_id config value
//  2. The GITHUB_INSTALLATION_ID environment variable (set by Helm from github.app.installationId)
func (s *Syncer) resolveToken(personalToken, repoFullName string) (string, error) {
	if personalToken != "" {
		return personalToken, nil
	}

	// Resolve installation ID: explicit config → GITHUB_INSTALLATION_ID env var (Helm-injected)
	installID := s.githubAppInstallID
	if installID == "" {
		installID = os.Getenv("GITHUB_INSTALLATION_ID")
	}
	if installID == "" {
		return "", fmt.Errorf("github_token is not configured and no GitHub App fallback is set (git_sync.github_app.installation_id or GITHUB_INSTALLATION_ID)")
	}

	appID := os.Getenv("GITHUB_APP_ID")
	if appID == "" {
		return "", fmt.Errorf("github_token is not configured and GITHUB_APP_ID env var is not set")
	}
	pemPath := os.Getenv("GITHUB_APP_PEM_PATH")
	token, err := startup.GenerateGitHubAppTokenForRepository(appID, installID, pemPath, repoFullName)
	if err != nil {
		return "", fmt.Errorf("github_token is not configured; GitHub App token generation failed: %w", err)
	}
	log.Printf("[SYNC] Using GitHub App installation token (installation_id=%s)", installID)
	return token, nil
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

	token, err := s.resolveToken(cfg.GitHubToken, cfg.RepoFullName)
	if err != nil {
		return nil, fmt.Errorf("no GitHub token for %q: %w", settingsName, err)
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
		if err := s.exportPersonalSettings(ctx, userID, dek, rootPath, files); err != nil {
			log.Printf("[SYNC] personal settings export warning for %s: %v", settingsName, err)
		}
		if err := s.exportUserSessionProfiles(ctx, userID, dek, rootPath, files); err != nil {
			log.Printf("[SYNC] user session profile export warning for %s: %v", settingsName, err)
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
		if err := s.exportTeamSessionProfiles(ctx, settingsName, dek, rootPath, files); err != nil {
			log.Printf("[SYNC] team session profile export warning for %s: %v", settingsName, err)
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
		files[rootPath+settingsName+"/.sync-meta.yaml"] = metaBytes
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

	pushedAt := time.Now()

	// Persist last push time so the periodic worker can determine sync direction.
	cfg.LastPushedAt = pushedAt
	settings.SetGitSync(cfg)
	if saveErr := s.settingsRepo.Save(ctx, settings); saveErr != nil {
		log.Printf("[SYNC] Warning: failed to save LastPushedAt for %s: %v", settingsName, saveErr)
	}

	return &PushResponse{
		CommitSHA: sha,
		PushedAt:  pushedAt,
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

	token, err := s.resolveToken(cfg.GitHubToken, cfg.RepoFullName)
	if err != nil {
		return nil, fmt.Errorf("no GitHub token for %q: %w", settingsName, err)
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
	settingsPrefix := rootPath + settingsName + "/"

	ghClient, err := NewGitHubSyncClient(ctx, token, cfg.RepoFullName)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}

	paths, err := ghClient.ListFiles(ctx, cfg.Branch, settingsPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list GitHub files: %w", err)
	}

	personal := isPersonalSync(settingsName, userID)

	filesWritten := 0
	for _, filePath := range paths {
		content, err := ghClient.GetFile(ctx, cfg.Branch, filePath)
		if err != nil {
			log.Printf("[SYNC] Warning: failed to get %s: %v", filePath, err)
			continue
		}

		if err := s.importFileByPath(ctx, filePath, content, settingsName, userID, dek,
			settingsPrefix, personal); err != nil {
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
// settingsPrefix is "{rootPath}/{settingsName}/"; personal distinguishes user vs team sync.
func (s *Syncer) importFileByPath(ctx context.Context, filePath string, content []byte,
	settingsName, userID string, dek []byte, settingsPrefix string, personal bool) error {

	rel := strings.TrimPrefix(filePath, settingsPrefix)

	switch {
	case rel == ".sync-meta.yaml":
		return nil
	case strings.HasPrefix(rel, "schedules/"):
		if personal {
			return s.importUserScheduleFile(ctx, content, userID, dek)
		}
		return s.importScheduleFile(ctx, content, settingsName, userID, dek)
	case strings.HasPrefix(rel, "webhooks/"):
		if personal {
			return s.importUserWebhookFile(ctx, content, userID, dek)
		}
		return s.importWebhookFile(ctx, content, settingsName, userID, dek)
	case rel == "settings.yaml":
		return s.importSettingsFile(ctx, content, settingsName, userID, dek)
	case strings.HasPrefix(rel, "files/"):
		if personal {
			return s.importUserFileRecord(ctx, content, userID, dek)
		}
		return nil
	case strings.HasPrefix(rel, "slackbots/"):
		scope := entities.ScopeTeam
		teamID := settingsName
		if personal {
			scope = entities.ScopeUser
			teamID = ""
		}
		return s.importSlackbotFile(ctx, content, scope, userID, teamID, dek)
	case strings.HasPrefix(rel, "session-profiles/"):
		scope := entities.ScopeTeam
		teamID := settingsName
		if personal {
			scope = entities.ScopeUser
			teamID = ""
		}
		return s.importSessionProfileFile(ctx, content, scope, userID, teamID, dek)
	default:
		return nil
	}
}

// resolveSyncDirection determines whether to push or pull for a tenant by comparing
// the remote .sync-meta.yaml syncedAt timestamp with the local LastPushedAt.
// Returns "pull" when GitHub is newer, "push" otherwise.
func (s *Syncer) resolveSyncDirection(ctx context.Context, cfg *entities.GitSyncConfig, settingsName string) (string, error) {
	token, err := s.resolveToken(cfg.GitHubToken, cfg.RepoFullName)
	if err != nil {
		return "", err
	}
	ghClient, err := NewGitHubSyncClient(ctx, token, cfg.RepoFullName)
	if err != nil {
		return "", err
	}
	rootPath := strings.TrimRight(cfg.RootPath, "/") + "/"
	metaPath := rootPath + settingsName + "/.sync-meta.yaml"

	var remoteSyncedAt time.Time
	if metaBytes, metaErr := ghClient.GetFile(ctx, cfg.Branch, metaPath); metaErr == nil {
		var meta SyncMeta
		if parseErr := yaml.Unmarshal(metaBytes, &meta); parseErr == nil {
			remoteSyncedAt = meta.SyncedAt
		}
	}

	if !remoteSyncedAt.IsZero() && remoteSyncedAt.After(cfg.LastPushedAt) {
		return "pull", nil
	}
	return "push", nil
}

// SyncAll syncs all settings that have GitHub sync enabled.
// The direction (push/pull) is determined automatically per tenant by comparing
// the remote .sync-meta.yaml syncedAt against the local LastPushedAt:
// if GitHub is newer → pull; otherwise → push.
// Each tenant is processed independently; errors are captured in results without aborting others.
func (s *Syncer) SyncAll(ctx context.Context, deleteOrphans bool, commitMessage string) (*SyncAllResponse, error) {
	allSettings, err := s.settingsRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}

	resp := &SyncAllResponse{SyncedAt: time.Now()}
	for _, settings := range allSettings {
		cfg := settings.GitSync()
		if cfg == nil || !cfg.Enabled {
			continue
		}
		name := settings.Name()
		result := SyncAllResult{SettingsName: name}

		direction, dirErr := s.resolveSyncDirection(ctx, cfg, name)
		if dirErr != nil {
			result.Error = "direction check: " + dirErr.Error()
			result.Direction = "unknown"
			log.Printf("[SYNC] SyncAll direction error for %s: %v", name, dirErr)
			resp.Results = append(resp.Results, result)
			continue
		}
		result.Direction = direction

		switch direction {
		case "pull":
			pullResp, pullErr := s.Pull(ctx, name, name, deleteOrphans)
			if pullErr != nil {
				result.Error = "pull: " + pullErr.Error()
				log.Printf("[SYNC] SyncAll pull error for %s: %v", name, pullErr)
			} else {
				result.Pull = pullResp
			}
		default:
			pushResp, pushErr := s.Push(ctx, name, name, commitMessage)
			if pushErr != nil {
				result.Error = "push: " + pushErr.Error()
				log.Printf("[SYNC] SyncAll push error for %s: %v", name, pushErr)
			} else {
				result.Push = pushResp
			}
		}

		resp.Results = append(resp.Results, result)
	}
	return resp, nil
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

	rotateToken, err := s.resolveToken(cfg.GitHubToken, cfg.RepoFullName)
	if err != nil {
		return nil, fmt.Errorf("no GitHub token for %q: %w", settingsName, err)
	}

	rootPath := strings.TrimRight(cfg.RootPath, "/") + "/"
	settingsPath := rootPath + settingsName + "/"

	ghClient, err := NewGitHubSyncClient(ctx, rotateToken, cfg.RepoFullName)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}

	paths, err := ghClient.ListFiles(ctx, cfg.Branch, settingsPath)
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

	dir := rootPath + settingsName + "/schedules/"
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

	dir := rootPath + settingsName + "/webhooks/"
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
	files[rootPath+settingsName+"/settings.yaml"] = data
	return nil
}

// exportPersonalSettings exports personal settings (identified by userID) to
// {rootPath}/{userID}/settings.yaml. GitSync configuration is excluded from the
// export by the underlying exporter, so re-importing on another instance will
// not overwrite the target's sync configuration.
func (s *Syncer) exportPersonalSettings(ctx context.Context, userID string, dek []byte, rootPath string, files map[string][]byte) error {
	return s.exportTeamSettings(ctx, userID, userID, dek, rootPath, files)
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

	dir := rootPath + userID + "/files/"
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
	dir := rootPath + userID + "/schedules/"
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
	dir := rootPath + userID + "/webhooks/"
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
	dir := rootPath + userID + "/slackbots/"
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
	dir := rootPath + settingsName + "/slackbots/"
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
			Message:     si.SessionConfig.Params.InitialMessage,
			GithubToken: si.SessionConfig.Params.GitHubToken,
		}
	}

	if existing != nil {
		return s.scheduleRepo.Update(ctx, sc)
	}
	return s.scheduleRepo.Create(ctx, sc)
}

func (s *Syncer) importUserWebhookFile(ctx context.Context, data []byte, userID string, dek []byte) error {
	if s.webhookRepo == nil {
		return nil
	}
	var wi importexport.WebhookImport
	if err := yaml.Unmarshal(data, &wi); err != nil {
		return fmt.Errorf("unmarshal webhook: %w", err)
	}
	// Decrypt all sensitive fields (Secret, Environment maps) via the shared helper.
	// Cannot use the generic Importer here because it hardcodes ScopeTeam.
	wrapper := &importexport.TeamResources{
		Metadata: importexport.ResourceMetadata{TeamID: userID},
		Webhooks: []importexport.WebhookImport{wi},
	}
	if err := decryptTeamResourcesFields(wrapper, dek); err != nil {
		return fmt.Errorf("decrypt webhook: %w", err)
	}
	return s.upsertUserWebhook(ctx, wrapper.Webhooks[0], userID)
}

// upsertUserWebhook creates or updates a personal (ScopeUser) webhook from a WebhookImport.
// It avoids the generic Importer which hardcodes ScopeTeam.
func (s *Syncer) upsertUserWebhook(ctx context.Context, wi importexport.WebhookImport, userID string) error {
	existing, _ := s.webhookRepo.List(ctx, portrepos.WebhookFilter{
		Scope:  entities.ScopeUser,
		UserID: userID,
	})

	// Match by ID first, then by name.
	var found *entities.Webhook
	for _, w := range existing {
		if wi.ID != "" && w.ID() == wi.ID {
			found = w
			break
		}
	}
	if found == nil {
		for _, w := range existing {
			if w.Name() == wi.Name {
				found = w
				break
			}
		}
	}

	var wh *entities.Webhook
	if found != nil {
		wh = found
	} else {
		id := wi.ID
		if id == "" {
			id = uuid.New().String()
		}
		wh = entities.NewWebhook(id, wi.Name, userID, entities.WebhookType(wi.WebhookType))
	}

	wh.SetScope(entities.ScopeUser)
	wh.SetName(wi.Name)
	if wi.Status != "" {
		wh.SetStatus(entities.WebhookStatus(wi.Status))
	}
	if wi.Secret != "" {
		wh.SetSecret(wi.Secret)
	}
	if wi.SignatureHeader != "" {
		wh.SetSignatureHeader(wi.SignatureHeader)
	}
	if wi.SignatureType != "" {
		wh.SetSignatureType(entities.WebhookSignatureType(wi.SignatureType))
	}
	if wi.SignaturePrefix != "" {
		wh.SetSignaturePrefix(wi.SignaturePrefix)
	}
	if wi.MaxSessions > 0 {
		wh.SetMaxSessions(wi.MaxSessions)
	}
	if wi.GitHub != nil {
		gh := entities.NewWebhookGitHubConfig()
		gh.SetEnterpriseURL(wi.GitHub.EnterpriseURL)
		gh.SetAllowedEvents(wi.GitHub.AllowedEvents)
		gh.SetAllowedRepositories(wi.GitHub.AllowedRepositories)
		wh.SetGitHub(gh)
	}
	if wi.SessionConfig != nil {
		sc := entities.NewWebhookSessionConfig()
		sc.SetEnvironment(wi.SessionConfig.Environment)
		sc.SetTags(wi.SessionConfig.Tags)
		wh.SetSessionConfig(sc)
	}

	triggers := make([]entities.WebhookTrigger, 0, len(wi.Triggers))
	for _, ti := range wi.Triggers {
		t := entities.NewWebhookTrigger(uuid.New().String(), ti.Name)
		t.SetPriority(ti.Priority)
		t.SetEnabled(ti.Enabled)
		t.SetStopOnMatch(ti.StopOnMatch)
		var cond entities.WebhookTriggerConditions
		if ti.Conditions.GitHub != nil {
			ghCond := entities.NewWebhookGitHubConditions()
			ghCond.SetEvents(ti.Conditions.GitHub.Events)
			ghCond.SetActions(ti.Conditions.GitHub.Actions)
			ghCond.SetBranches(ti.Conditions.GitHub.Branches)
			ghCond.SetRepositories(ti.Conditions.GitHub.Repositories)
			ghCond.SetLabels(ti.Conditions.GitHub.Labels)
			ghCond.SetPaths(ti.Conditions.GitHub.Paths)
			ghCond.SetBaseBranches(ti.Conditions.GitHub.BaseBranches)
			ghCond.SetDraft(ti.Conditions.GitHub.Draft)
			ghCond.SetSender(ti.Conditions.GitHub.Sender)
			cond.SetGitHub(ghCond)
		}
		if ti.Conditions.GoTemplate != "" {
			cond.SetGoTemplate(ti.Conditions.GoTemplate)
		}
		t.SetConditions(cond)
		if ti.SessionConfig != nil {
			tsc := entities.NewWebhookSessionConfig()
			tsc.SetEnvironment(ti.SessionConfig.Environment)
			tsc.SetTags(ti.SessionConfig.Tags)
			t.SetSessionConfig(tsc)
		}
		triggers = append(triggers, t)
	}
	wh.SetTriggers(triggers)

	if found != nil {
		return s.webhookRepo.Update(ctx, wh)
	}
	return s.webhookRepo.Create(ctx, wh)
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

// encryptTeamResourcesFields encrypts all fields tagged gitsync:"encrypt" or
// gitsync:"encrypt-values" using the fixed DEK, then clears companion metadata fields.
func encryptTeamResourcesFields(r *importexport.TeamResources, dek []byte) error {
	if err := encryptTaggedFields(r, dek); err != nil {
		return err
	}
	clearCompanionFields(r)
	return nil
}

// decryptTeamResourcesFields decrypts all enc:v1: values in fields tagged
// gitsync:"encrypt" or gitsync:"encrypt-values".
func decryptTeamResourcesFields(r *importexport.TeamResources, dek []byte) error {
	return decryptTaggedFields(r, dek)
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
			GitHubToken:    sc.SessionConfig.Params.GithubToken,
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
		if cond.GoTemplate() != "" {
			ti.Conditions.GoTemplate = cond.GoTemplate()
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

// sessionProfileRecord is the YAML on-disk representation of a SessionProfile.
type sessionProfileRecord struct {
	ID                     string                      `yaml:"id"`
	Name                   string                      `yaml:"name"`
	Description            string                      `yaml:"description,omitempty"`
	UserID                 string                      `yaml:"user_id"`
	Scope                  string                      `yaml:"scope"`
	TeamID                 string                      `yaml:"team_id,omitempty"`
	IsDefault              bool                        `yaml:"is_default,omitempty"`
	SelectorTags           map[string]string           `yaml:"selector_tags,omitempty"`
	Environment            map[string]string           `yaml:"environment,omitempty"`
	Tags                   map[string]string           `yaml:"tags,omitempty"`
	InitialMessageTemplate string                      `yaml:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                      `yaml:"reuse_message_template,omitempty"`
	ReuseSession           bool                        `yaml:"reuse_session,omitempty"`
	MemoryKey              map[string]string           `yaml:"memory_key,omitempty"`
	Params                 *sessionProfileParamsRecord `yaml:"params,omitempty"`
	GitHubToken            string                      `yaml:"github_token,omitempty"`
	InitialMessage         string                      `yaml:"initial_message,omitempty"`
	CreatedAt              string                      `yaml:"created_at,omitempty"`
	UpdatedAt              string                      `yaml:"updated_at,omitempty"`
}

type sessionProfileParamsRecord struct {
	Message                  string                             `yaml:"message,omitempty"`
	GitHubToken              string                             `yaml:"github_token,omitempty"`
	AgentType                string                             `yaml:"agent_type,omitempty"`
	Slack                    *sessionProfileSlackParamsRecord   `yaml:"slack,omitempty"`
	Oneshot                  bool                               `yaml:"oneshot,omitempty"`
	InitialMessageWaitSecond *int                               `yaml:"initial_message_wait_second,omitempty"`
	ManagerID                string                             `yaml:"manager_id,omitempty"`
	CycleMessage             string                             `yaml:"cycle_message,omitempty"`
	CycleMaxCount            int                                `yaml:"cycle_max_count,omitempty"`
	RepoFullName             string                             `yaml:"repo_full_name,omitempty"`
	Sandbox                  *sessionProfileSandboxParamsRecord `yaml:"sandbox,omitempty"`
}

type sessionProfileSlackParamsRecord struct {
	Channel            string `yaml:"channel,omitempty"`
	ThreadTS           string `yaml:"thread_ts,omitempty"`
	BotTokenSecretName string `yaml:"bot_token_secret_name,omitempty"`
	BotTokenSecretKey  string `yaml:"bot_token_secret_key,omitempty"`
}

type sessionProfileSandboxParamsRecord struct {
	Enabled        bool     `yaml:"enabled,omitempty"`
	AllowedDomains []string `yaml:"allowed_domains,omitempty"`
	DeniedDomains  []string `yaml:"denied_domains,omitempty"`
}

func sessionProfileToRecord(p *entities.SessionProfile, dek []byte) (sessionProfileRecord, error) {
	cfg := p.Config()
	env := make(map[string]string, len(cfg.Environment()))
	for k, v := range cfg.Environment() {
		if !IsEncrypted(v) {
			enc, err := EncryptField(dek, v)
			if err != nil {
				return sessionProfileRecord{}, fmt.Errorf("encrypt session profile env %s: %w", k, err)
			}
			env[k] = enc
			continue
		}
		env[k] = v
	}
	rec := sessionProfileRecord{
		ID:                     p.ID(),
		Name:                   p.Name(),
		Description:            p.Description(),
		UserID:                 p.UserID(),
		Scope:                  string(p.Scope()),
		TeamID:                 p.TeamID(),
		IsDefault:              p.IsDefault(),
		SelectorTags:           p.SelectorTags(),
		Environment:            env,
		Tags:                   cfg.Tags(),
		InitialMessageTemplate: cfg.InitialMessageTemplate(),
		ReuseMessageTemplate:   cfg.ReuseMessageTemplate(),
		ReuseSession:           cfg.ReuseSession(),
		MemoryKey:              cfg.MemoryKey(),
		CreatedAt:              p.CreatedAt().Format(time.RFC3339),
		UpdatedAt:              p.UpdatedAt().Format(time.RFC3339),
	}
	if params := cfg.Params(); params != nil {
		paramsRec := sessionParamsToRecord(params)
		if paramsRec.GitHubToken != "" && !IsEncrypted(paramsRec.GitHubToken) {
			enc, err := EncryptField(dek, paramsRec.GitHubToken)
			if err != nil {
				return sessionProfileRecord{}, fmt.Errorf("encrypt session profile github_token: %w", err)
			}
			paramsRec.GitHubToken = enc
		}
		rec.Params = paramsRec
	}
	return rec, nil
}

func sessionParamsToRecord(p *entities.SessionParams) *sessionProfileParamsRecord {
	if p == nil {
		return nil
	}
	rec := &sessionProfileParamsRecord{
		Message:                  p.Message,
		GitHubToken:              p.GithubToken,
		AgentType:                p.AgentType,
		Oneshot:                  p.Oneshot,
		InitialMessageWaitSecond: p.InitialMessageWaitSecond,
		ManagerID:                p.ManagerID,
		CycleMessage:             p.CycleMessage,
		CycleMaxCount:            p.CycleMaxCount,
		RepoFullName:             p.RepoFullName,
	}
	if p.Slack != nil {
		rec.Slack = &sessionProfileSlackParamsRecord{
			Channel:            p.Slack.Channel,
			ThreadTS:           p.Slack.ThreadTS,
			BotTokenSecretName: p.Slack.BotTokenSecretName,
			BotTokenSecretKey:  p.Slack.BotTokenSecretKey,
		}
	}
	if p.Sandbox != nil {
		rec.Sandbox = &sessionProfileSandboxParamsRecord{
			Enabled:        p.Sandbox.Enabled,
			AllowedDomains: p.Sandbox.AllowedDomains,
			DeniedDomains:  p.Sandbox.DeniedDomains,
		}
	}
	return rec
}

func sessionParamsFromRecord(rec *sessionProfileParamsRecord) *entities.SessionParams {
	if rec == nil {
		return nil
	}
	params := &entities.SessionParams{
		Message:                  rec.Message,
		GithubToken:              rec.GitHubToken,
		AgentType:                rec.AgentType,
		Oneshot:                  rec.Oneshot,
		InitialMessageWaitSecond: rec.InitialMessageWaitSecond,
		ManagerID:                rec.ManagerID,
		CycleMessage:             rec.CycleMessage,
		CycleMaxCount:            rec.CycleMaxCount,
		RepoFullName:             rec.RepoFullName,
	}
	if rec.Slack != nil {
		params.Slack = &entities.SlackParams{
			Channel:            rec.Slack.Channel,
			ThreadTS:           rec.Slack.ThreadTS,
			BotTokenSecretName: rec.Slack.BotTokenSecretName,
			BotTokenSecretKey:  rec.Slack.BotTokenSecretKey,
		}
	}
	if rec.Sandbox != nil {
		params.Sandbox = &entities.SandboxParams{
			Enabled:        rec.Sandbox.Enabled,
			AllowedDomains: rec.Sandbox.AllowedDomains,
			DeniedDomains:  rec.Sandbox.DeniedDomains,
		}
	}
	return params
}

func (s *Syncer) exportUserSessionProfiles(ctx context.Context, userID string, dek []byte, rootPath string, files map[string][]byte) error {
	if s.sessionProfileRepo == nil {
		return nil
	}
	profiles, err := s.sessionProfileRepo.List(ctx, portrepos.SessionProfileFilter{
		Scope:  entities.ScopeUser,
		UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("list user session profiles: %w", err)
	}
	dir := rootPath + userID + "/session-profiles/"
	for _, p := range profiles {
		rec, err := sessionProfileToRecord(p, dek)
		if err != nil {
			return fmt.Errorf("encrypt user session profile %s: %w", p.ID(), err)
		}
		data, err := yaml.Marshal(rec)
		if err != nil {
			log.Printf("[SYNC] Warning: marshal user session profile %s: %v", p.ID(), err)
			continue
		}
		files[dir+p.ID()+".yaml"] = data
	}
	return nil
}

func (s *Syncer) exportTeamSessionProfiles(ctx context.Context, settingsName string, dek []byte, rootPath string, files map[string][]byte) error {
	if s.sessionProfileRepo == nil {
		return nil
	}
	profiles, err := s.sessionProfileRepo.List(ctx, portrepos.SessionProfileFilter{
		Scope:  entities.ScopeTeam,
		TeamID: settingsName,
	})
	if err != nil {
		return fmt.Errorf("list team session profiles: %w", err)
	}
	dir := rootPath + settingsName + "/session-profiles/"
	for _, p := range profiles {
		rec, err := sessionProfileToRecord(p, dek)
		if err != nil {
			return fmt.Errorf("encrypt team session profile %s: %w", p.ID(), err)
		}
		data, err := yaml.Marshal(rec)
		if err != nil {
			log.Printf("[SYNC] Warning: marshal team session profile %s: %v", p.ID(), err)
			continue
		}
		files[dir+p.ID()+".yaml"] = data
	}
	return nil
}

func (s *Syncer) importSessionProfileFile(ctx context.Context, data []byte, scope entities.ResourceScope, userID, teamID string, dek []byte) error {
	if s.sessionProfileRepo == nil {
		return nil
	}
	var rec sessionProfileRecord
	if err := yaml.Unmarshal(data, &rec); err != nil {
		return fmt.Errorf("unmarshal session profile: %w", err)
	}

	// Decrypt environment values
	env := make(map[string]string, len(rec.Environment))
	for k, v := range rec.Environment {
		if IsEncrypted(v) {
			plain, err := DecryptField(dek, v)
			if err != nil {
				return fmt.Errorf("decrypt session profile env %s: %w", k, err)
			}
			env[k] = plain
		} else {
			env[k] = v
		}
	}

	// Decrypt GitHub token if encrypted
	params := rec.Params
	if params != nil && IsEncrypted(params.GitHubToken) {
		paramsCopy := *params
		plain, err := DecryptField(dek, paramsCopy.GitHubToken)
		if err != nil {
			return fmt.Errorf("decrypt session profile github_token: %w", err)
		}
		paramsCopy.GitHubToken = plain
		params = &paramsCopy
	}
	if params == nil && (rec.InitialMessage != "" || rec.GitHubToken != "") {
		githubToken := rec.GitHubToken
		if IsEncrypted(githubToken) {
			plain, err := DecryptField(dek, githubToken)
			if err != nil {
				return fmt.Errorf("decrypt session profile github_token: %w", err)
			}
			githubToken = plain
		}
		params = &sessionProfileParamsRecord{
			Message:     rec.InitialMessage,
			GitHubToken: githubToken,
		}
	}

	id := rec.ID
	if id == "" {
		id = uuid.New().String()
	}
	existing, _ := s.sessionProfileRepo.Get(ctx, id)

	ownerID := userID
	if rec.UserID != "" {
		ownerID = rec.UserID
	}

	var p *entities.SessionProfile
	if existing != nil {
		p = existing
	} else {
		p = entities.NewSessionProfile(id, rec.Name, ownerID)
	}
	p.SetName(rec.Name)
	p.SetDescription(rec.Description)
	p.SetScope(scope)
	p.SetTeamID(teamID)
	p.SetIsDefault(rec.IsDefault)
	p.SetSelectorTags(rec.SelectorTags)

	cfg := entities.NewSessionProfileConfig()
	if len(env) > 0 {
		cfg.SetEnvironment(env)
	}
	if len(rec.Tags) > 0 {
		cfg.SetTags(rec.Tags)
	}
	cfg.SetInitialMessageTemplate(rec.InitialMessageTemplate)
	cfg.SetReuseMessageTemplate(rec.ReuseMessageTemplate)
	cfg.SetReuseSession(rec.ReuseSession)
	if len(rec.MemoryKey) > 0 {
		cfg.SetMemoryKey(rec.MemoryKey)
	}
	if params != nil {
		cfg.SetParams(sessionParamsFromRecord(params))
	}
	p.SetConfig(cfg)

	if existing != nil {
		return s.sessionProfileRepo.Update(ctx, p)
	}
	return s.sessionProfileRepo.Create(ctx, p)
}
