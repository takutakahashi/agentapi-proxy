package sessionsettings

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/takutakahashi/agentapi-proxy/pkg/startup"
)

// SetupOptions configures the Setup behavior.
type SetupOptions struct {
	// InputPath is the path to the session settings YAML file.
	// Defaults to /session-settings/settings.yaml.
	InputPath string

	// CompileOptions controls file generation (Compile step).
	CompileOptions CompileOptions

	// CredentialsFile is the path to the credentials.json mounted from Secret (optional).
	CredentialsFile string

	// ClaudeMDFile is the path to CLAUDE.md to copy into ~/.claude/CLAUDE.md (optional).
	ClaudeMDFile string

	// NotificationSubscriptions is the source directory for notification subscription files (optional).
	NotificationSubscriptions string

	// NotificationsDir is the destination directory for notification files (optional).
	NotificationsDir string

	// RegisterMarketplaces registers cloned marketplace repos via claude CLI.
	RegisterMarketplaces bool

	// PEMOutputPath is where GITHUB_APP_PEM content is written.
	// Defaults to /tmp/github-app/app.pem.
	PEMOutputPath string
}

// DefaultSetupOptions returns the default Setup options.
func DefaultSetupOptions() SetupOptions {
	return SetupOptions{
		InputPath:      "/session-settings/settings.yaml",
		CompileOptions: DefaultCompileOptions(),
		PEMOutputPath:  "/tmp/github-app/app.pem",
	}
}

// Setup runs the full init-container setup sequence for a session Pod:
//  1. write-pem  : writes GITHUB_APP_PEM env var to a file on disk
//  2. clone-repo : clones the repository (if session.repository is set)
//  3. compile    : generates all Claude config files from settings.yaml
//  4. sync-extra : copies credentials, CLAUDE.md, notification subscriptions
func Setup(opts SetupOptions) error {
	if opts.InputPath == "" {
		opts.InputPath = DefaultSetupOptions().InputPath
	}
	if opts.PEMOutputPath == "" {
		opts.PEMOutputPath = DefaultSetupOptions().PEMOutputPath
	}

	log.Printf("[SETUP] Reading settings from %s", opts.InputPath)
	settings, err := LoadSettings(opts.InputPath)
	if err != nil {
		return fmt.Errorf("failed to load session settings: %w", err)
	}

	// 1. Write GitHub App PEM to disk so git/gh can use it
	if err := writePEM(settings, opts.PEMOutputPath); err != nil {
		// Non-fatal: not all sessions use GitHub App auth
		log.Printf("[SETUP] Warning: write-pem skipped: %v", err)
	}

	// 2. Clone repository if configured
	if settings.Repository != nil && settings.Repository.FullName != "" {
		if err := cloneRepo(settings); err != nil {
			// Non-fatal: log warning and continue with the rest of setup
			log.Printf("[SETUP] Warning: clone-repo failed: %v", err)
		}
	}

	// 3. Compile settings.yaml → config files
	if err := Compile(opts.CompileOptions); err != nil {
		return fmt.Errorf("compile failed: %w", err)
	}

	// 4. Copy credentials, CLAUDE.md, notification subscriptions
	if err := syncExtra(settings, opts); err != nil {
		return fmt.Errorf("sync-extra failed: %w", err)
	}

	log.Printf("[SETUP] Setup completed successfully")
	return nil
}

// writePEM writes the GITHUB_APP_PEM value from Env to a file on disk.
// This replaces the write-pem init container.
func writePEM(settings *SessionSettings, pemOutputPath string) error {
	pem := settings.Env["GITHUB_APP_PEM"]
	if pem == "" {
		return fmt.Errorf("GITHUB_APP_PEM is not set in session env, skipping")
	}

	pemDir := filepath.Dir(pemOutputPath)
	if err := os.MkdirAll(pemDir, 0700); err != nil {
		return fmt.Errorf("failed to create PEM directory: %w", err)
	}

	if err := os.WriteFile(pemOutputPath, []byte(pem), 0600); err != nil {
		return fmt.Errorf("failed to write PEM file: %w", err)
	}

	log.Printf("[SETUP] Wrote GitHub App PEM to %s", pemOutputPath)
	return nil
}

// cloneRepo sets up GitHub auth and clones the repository.
// This replaces the clone-repo init container.
func cloneRepo(settings *SessionSettings) error {
	repo := settings.Repository

	// Set environment variables from session env so git/gh tools pick them up
	for k, v := range settings.Env {
		if err := os.Setenv(k, v); err != nil {
			log.Printf("[SETUP] Warning: failed to set env %s: %v", k, err)
		}
	}

	log.Printf("[SETUP] Setting up GitHub auth for repo: %s", repo.FullName)
	if err := startup.SetupGitHubAuth(repo.FullName); err != nil {
		// Non-fatal for public repos
		log.Printf("[SETUP] Warning: GitHub auth setup failed: %v", err)
	}

	cloneDir := repo.CloneDir
	if cloneDir == "" {
		cloneDir = filepath.Join("/home/agentapi/workdir", repo.FullName)
	}

	// Skip clone if already present
	if _, err := os.Stat(filepath.Join(cloneDir, ".git")); err == nil {
		log.Printf("[SETUP] Repository already cloned at %s, skipping", cloneDir)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(cloneDir), 0755); err != nil {
		return fmt.Errorf("failed to create parent dir for clone: %w", err)
	}

	log.Printf("[SETUP] Cloning %s into %s", repo.FullName, cloneDir)
	cmd := exec.Command("git", "clone", "--depth", "1",
		fmt.Sprintf("https://github.com/%s.git", repo.FullName),
		cloneDir,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}

	log.Printf("[SETUP] Cloned %s to %s", repo.FullName, cloneDir)
	return nil
}

// syncExtra handles credentials, CLAUDE.md, and notification subscriptions.
// This replaces the corresponding parts of the sync-config init container.
func syncExtra(settings *SessionSettings, opts SetupOptions) error {
	outputDir := opts.CompileOptions.OutputDir
	if outputDir == "" {
		outputDir = DefaultCompileOptions().OutputDir
	}

	syncOpts := startup.SyncOptions{
		OutputDir:                 outputDir,
		CredentialsFile:           opts.CredentialsFile,
		ClaudeMDFile:              opts.ClaudeMDFile,
		NotificationSubscriptions: opts.NotificationSubscriptions,
		NotificationsDir:          opts.NotificationsDir,
		RegisterMarketplaces:      opts.RegisterMarketplaces,
	}

	// Set env so marketplace clone / claude CLI picks up GITHUB_TOKEN etc.
	for k, v := range settings.Env {
		if err := os.Setenv(k, v); err != nil {
			log.Printf("[SETUP] Warning: failed to set env %s: %v", k, err)
		}
	}

	return startup.SyncExtra(syncOpts)
}
