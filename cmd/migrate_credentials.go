package cmd

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// migrateCredentials command flags
var (
	migrateCredsNamespace string
	migrateCredsDryRun    bool
	migrateCredsCleanup   bool
	migrateCredsVerbose   bool
)

var migrateCredentialsCmd = &cobra.Command{
	Use:   "migrate-credentials",
	Short: "Migrate legacy credential Secrets into agentapi-agent-files-* (unified format)",
	Long: `Migrate legacy Kubernetes Secrets into the unified agentapi-agent-files-* format.

Two legacy Secret types are migrated:

  1. agentapi-credentials-{name}   (label: agentapi.proxy/credentials=true)
     auth.json key  →  codex_auth  →  ~/.codex/auth.json

  2. agentapi-agent-env-{name}     (label: app.kubernetes.io/name=agentapi-agent-credentials)
     auth.json key  →  claude_credentials  →  ~/.claude/.credentials.json

Both are consolidated into a single agentapi-agent-files-{name} Secret using
the index-based KV format:  0.path / 0.content, 1.path / 1.content, ...

Phase 1 (default): Scans and reports what would be migrated without making any changes.
Phase 2 (--cleanup): Deletes the legacy Secrets after migration.

Examples:
  # Preview migration (no changes)
  agentapi-proxy helpers migrate-credentials --namespace agentapi-ui

  # Execute migration (write new Secrets)
  agentapi-proxy helpers migrate-credentials --namespace agentapi-ui --dry-run=false

  # Execute migration and delete legacy Secrets
  agentapi-proxy helpers migrate-credentials --namespace agentapi-ui --dry-run=false --cleanup`,
	RunE: runMigrateCredentials,
}

func init() {
	migrateCredentialsCmd.Flags().StringVar(&migrateCredsNamespace, "namespace", "agentapi-ui",
		"Kubernetes namespace to operate in")
	migrateCredentialsCmd.Flags().BoolVar(&migrateCredsDryRun, "dry-run", true,
		"Preview migration without writing Secrets (default: true)")
	migrateCredentialsCmd.Flags().BoolVar(&migrateCredsCleanup, "cleanup", false,
		"Delete legacy Secrets after successful migration")
	migrateCredentialsCmd.Flags().BoolVarP(&migrateCredsVerbose, "verbose", "v", false,
		"Verbose output")

	HelpersCmd.AddCommand(migrateCredentialsCmd)
}

// credMigrationEntry holds the data to be written into agentapi-agent-files-{name}.
type credMigrationEntry struct {
	name     string // canonical name (e.g. "takutakahashi")
	fileType string // sessionsettings.FileTypeCodexAuth or FileTypeClaudeCredentials
	content  []byte // raw JSON content
	source   string // source Secret name (for logging / cleanup)
}

func runMigrateCredentials(_ *cobra.Command, _ []string) error {
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	ctx := context.Background()
	ns := migrateCredsNamespace

	if migrateCredsDryRun {
		fmt.Printf("[DRY-RUN] Scanning namespace: %s\n\n", ns)
	} else {
		fmt.Printf("Scanning namespace: %s\n\n", ns)
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Collect migration entries from both legacy Secret types.
	// ─────────────────────────────────────────────────────────────────────────
	var entries []credMigrationEntry

	// --- 1. agentapi-credentials-* (codex_auth) ---
	codexSecrets, err := client.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "agentapi.proxy/credentials=true",
	})
	if err != nil {
		return fmt.Errorf("failed to list agentapi-credentials-* secrets: %w", err)
	}
	fmt.Printf("Found %d agentapi-credentials-* secret(s) [codex_auth]\n", len(codexSecrets.Items))

	for _, s := range codexSecrets.Items {
		name := credNameFromSecret(&s, "agentapi-credentials-")
		content, ok := s.Data["auth.json"]
		if !ok || len(content) == 0 {
			fmt.Printf("  [SKIP] %s: no auth.json data\n", s.Name)
			continue
		}
		fmt.Printf("  [FOUND] %s → name=%q, type=codex_auth (%d bytes)\n", s.Name, name, len(content))
		entries = append(entries, credMigrationEntry{
			name:     name,
			fileType: sessionsettings.FileTypeCodexAuth,
			content:  content,
			source:   s.Name,
		})
	}

	// --- 2. agentapi-agent-env-* (claude_credentials) ---
	claudeSecrets, err := client.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=agentapi-agent-credentials",
	})
	if err != nil {
		return fmt.Errorf("failed to list agentapi-agent-env-* secrets: %w", err)
	}
	fmt.Printf("Found %d agentapi-agent-env-* secret(s) [claude_credentials]\n", len(claudeSecrets.Items))

	for _, s := range claudeSecrets.Items {
		name := credNameFromSecret(&s, "agentapi-agent-env-")
		content, ok := s.Data["auth.json"]
		if !ok || len(content) == 0 {
			fmt.Printf("  [SKIP] %s: no auth.json data\n", s.Name)
			continue
		}
		fmt.Printf("  [FOUND] %s → name=%q, type=claude_credentials (%d bytes)\n", s.Name, name, len(content))
		entries = append(entries, credMigrationEntry{
			name:     name,
			fileType: sessionsettings.FileTypeClaudeCredentials,
			content:  content,
			source:   s.Name,
		})
	}

	if len(entries) == 0 {
		fmt.Println("\nNo migration entries found. Nothing to do.")
		return nil
	}

	fmt.Printf("\nTotal entries to migrate: %d\n\n", len(entries))

	// ─────────────────────────────────────────────────────────────────────────
	// Group entries by canonical name so we write one Secret per user.
	// ─────────────────────────────────────────────────────────────────────────
	grouped := map[string][]credMigrationEntry{}
	for _, e := range entries {
		grouped[e.name] = append(grouped[e.name], e)
	}

	var migratedNames []string
	var cleanupTargets []string

	for name, group := range grouped {
		targetSecretName := credAgentFilesSecretName(name)
		fmt.Printf("=== Migrating name=%q → %s ===\n", name, targetSecretName)

		// Load current agentapi-agent-files-{name} Secret (if it exists) to
		// avoid overwriting already-present file types.
		currentFiles, createdAt := loadExistingAgentFilesSecret(ctx, client, ns, targetSecretName)
		now := time.Now().UTC().Format(time.RFC3339)
		if createdAt == "" {
			createdAt = now
		}

		// Merge each entry into the current file list.
		for _, e := range group {
			filePath, ok := sessionsettings.ManagedFileTypes[e.fileType]
			if !ok {
				fmt.Printf("  [ERROR] Unknown file type %q – skipping\n", e.fileType)
				continue
			}

			updated := false
			for i, f := range currentFiles {
				if f.Path == filePath {
					currentFiles[i].Content = string(e.content)
					updated = true
					break
				}
			}
			if !updated {
				currentFiles = append(currentFiles, sessionsettings.ManagedFile{
					Path:    filePath,
					Content: string(e.content),
				})
			}

			if migrateCredsDryRun {
				fmt.Printf("  [DRY-RUN] Would write type=%s → %s (%d bytes)\n", e.fileType, filePath, len(e.content))
			} else {
				fmt.Printf("  [WRITE] type=%s → %s (%d bytes)\n", e.fileType, filePath, len(e.content))
			}
			cleanupTargets = append(cleanupTargets, e.source)
		}

		if migrateCredsDryRun {
			fmt.Printf("  [DRY-RUN] Would upsert Secret %s/%s with %d file(s)\n", ns, targetSecretName, len(currentFiles))
		} else {
			if err := upsertAgentFilesSecret(ctx, client, ns, targetSecretName, name, currentFiles, createdAt, now); err != nil {
				fmt.Printf("  [ERROR] Failed to upsert %s: %v\n", targetSecretName, err)
				continue
			}
			fmt.Printf("  [OK] Upserted Secret %s/%s with %d file(s)\n", ns, targetSecretName, len(currentFiles))
			migratedNames = append(migratedNames, name)
		}
	}

	// ─────────────────────────────────────────────────────────────────────────
	// Phase 2: Cleanup legacy Secrets (only if --cleanup is set).
	// ─────────────────────────────────────────────────────────────────────────
	if !migrateCredsCleanup {
		if migrateCredsDryRun {
			fmt.Printf("\n[DRY-RUN] Migration complete. %d name(s) would be processed.\n", len(grouped))
		} else {
			fmt.Printf("\nMigration complete. %d name(s) processed.\n", len(migratedNames))
		}
		fmt.Println("Run with --cleanup to delete legacy Secrets after migration.")
		return nil
	}

	fmt.Printf("\n=== Cleanup: deleting %d legacy Secret(s) ===\n", len(cleanupTargets))
	deletedCount := 0
	for _, secretName := range cleanupTargets {
		if migrateCredsDryRun {
			fmt.Printf("  [DRY-RUN] Would delete: %s\n", secretName)
		} else {
			if err := client.CoreV1().Secrets(ns).Delete(ctx, secretName, metav1.DeleteOptions{}); err != nil {
				if k8serrors.IsNotFound(err) {
					if migrateCredsVerbose {
						fmt.Printf("  [SKIP] %s: already deleted\n", secretName)
					}
				} else {
					fmt.Printf("  [ERROR] Failed to delete %s: %v\n", secretName, err)
				}
			} else {
				fmt.Printf("  [DELETED] %s\n", secretName)
				deletedCount++
			}
		}
	}

	if migrateCredsDryRun {
		fmt.Printf("\n[DRY-RUN] Would delete %d legacy Secret(s).\n", len(cleanupTargets))
		fmt.Println("Run with --dry-run=false to execute the migration.")
	} else {
		fmt.Printf("\nMigration and cleanup complete. %d name(s) migrated, %d legacy Secret(s) deleted.\n",
			len(migratedNames), deletedCount)
	}

	return nil
}

// credNameFromSecret extracts the canonical name from a legacy Secret.
// It first checks well-known annotations, then falls back to stripping the prefix.
func credNameFromSecret(s *corev1.Secret, prefix string) string {
	// Check annotations first (populated by the credentials controller).
	if s.Annotations != nil {
		if v := s.Annotations["agentapi.proxy/credentials-name"]; v != "" {
			return v
		}
	}
	// Fall back to stripping the secret name prefix.
	name := strings.TrimPrefix(s.Name, prefix)
	if name == "" {
		return s.Name
	}
	return name
}

// credAgentFilesSecretName returns the canonical agentapi-agent-files-{sanitized} name.
func credAgentFilesSecretName(name string) string {
	sanitized := credSanitizeSecretName(name)
	const prefix = "agentapi-agent-files-"
	maxLen := 253 - len(prefix)
	if len(sanitized) > maxLen {
		sanitized = sanitized[:maxLen]
	}
	return prefix + sanitized
}

// credSanitizeSecretName mirrors the repository's sanitizeSecretName without
// importing the internal package.
func credSanitizeSecretName(s string) string {
	sanitized := strings.ToLower(s)
	re := regexp.MustCompile(`[^a-z0-9-]`)
	sanitized = re.ReplaceAllString(sanitized, "-")
	sanitized = strings.Trim(sanitized, "-")
	re = regexp.MustCompile(`-+`)
	sanitized = re.ReplaceAllString(sanitized, "-")
	return sanitized
}

// loadExistingAgentFilesSecret reads the current agentapi-agent-files-{name} Secret
// and returns its managed files and the original createdAt annotation.
// Returns empty values if the Secret does not exist.
func loadExistingAgentFilesSecret(ctx context.Context, client kubernetes.Interface, ns, secretName string) ([]sessionsettings.ManagedFile, string) {
	existing, err := client.CoreV1().Secrets(ns).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, ""
	}
	files := sessionsettings.SecretDataToFiles(existing.Data)
	createdAt := ""
	if existing.Annotations != nil {
		createdAt = existing.Annotations["agentapi.proxy/credentials-created-at"]
	}
	return files, createdAt
}

// upsertAgentFilesSecret creates or updates the agentapi-agent-files-{name} Secret.
func upsertAgentFilesSecret(
	ctx context.Context,
	client kubernetes.Interface,
	ns, secretName, credName string,
	files []sessionsettings.ManagedFile,
	createdAt, updatedAt string,
) error {
	labelValue := credSanitizeLabelValue(credName)
	secretData := sessionsettings.FilesToSecretData(files)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: ns,
			Labels: map[string]string{
				"agentapi.proxy/credentials":      "true",
				"agentapi.proxy/credentials-name": labelValue,
			},
			Annotations: map[string]string{
				"agentapi.proxy/credentials-name":       credName,
				"agentapi.proxy/credentials-created-at": createdAt,
				"agentapi.proxy/credentials-updated-at": updatedAt,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: secretData,
	}

	_, err := client.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			_, err = client.CoreV1().Secrets(ns).Update(ctx, secret, metav1.UpdateOptions{})
			return err
		}
		return err
	}
	return nil
}

// credSanitizeLabelValue mirrors sanitizeLabelValue without importing internal package.
func credSanitizeLabelValue(s string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_.-]`)
	sanitized := re.ReplaceAllString(s, "-")
	sanitized = strings.Trim(sanitized, "-_.")
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
	}
	sanitized = strings.TrimRight(sanitized, "-_.")
	return sanitized
}
