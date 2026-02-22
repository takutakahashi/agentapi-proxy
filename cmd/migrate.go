package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

// migrateSettingsJSON is the JSON representation of settings stored in agentapi-settings-* Secrets.
// It mirrors the private settingsJSON in the repositories package.
type migrateSettingsJSON struct {
	Name                 string                             `json:"name"`
	Bedrock              *migrateBedrockJSON                `json:"bedrock,omitempty"`
	MCPServers           map[string]*migrateMCPServerJSON   `json:"mcp_servers,omitempty"`
	Marketplaces         map[string]*migrateMarketplaceJSON `json:"marketplaces,omitempty"`
	ClaudeCodeOAuthToken string                             `json:"claude_code_oauth_token,omitempty"`
	AuthMode             string                             `json:"auth_mode,omitempty"`
	EnabledPlugins       []string                           `json:"enabled_plugins,omitempty"`
	EnvVars              map[string]string                  `json:"env_vars,omitempty"`
	CreatedAt            time.Time                          `json:"created_at"`
	UpdatedAt            time.Time                          `json:"updated_at"`
}

type migrateBedrockJSON struct {
	Enabled         bool   `json:"enabled"`
	Model           string `json:"model,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	RoleARN         string `json:"role_arn,omitempty"`
	Profile         string `json:"profile,omitempty"`
}

type migrateMCPServerJSON struct {
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type migrateMarketplaceJSON struct {
	URL string `json:"url"`
}

// migrate command flags
var (
	migrateNamespace string
	migrateCleanup   bool
	migrateDryRun    bool
	migrateVerbose   bool
)

// migrate verify subcommand flags
var (
	migrateVerifyNamespace string
	migrateVerifyVerbose   bool
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate derived Secrets into agentapi-settings-* (unified source)",
	Long: `Migrate and verify that derived Kubernetes Secrets are no longer needed.

This command verifies that agentapi-settings-* Secrets already contain all
configuration data (MCP servers, marketplaces, env vars, credentials) that
was previously split into separate derived Secrets:

  - mcp-servers-{name}    (labeled agentapi.proxy/mcp-servers=true)
  - marketplaces-{name}   (labeled agentapi.proxy/marketplaces=true)
  - agent-env-{name}      (labeled agentapi.proxy/env=true)

Phase 1 (default): Lists all agentapi-settings-* Secrets and reports their
contents so you can verify all data is present.

Phase 2 (--cleanup): Deletes derived Secrets that are labeled as managed by
agentapi-proxy settings. Use --dry-run to preview before deleting.

Examples:
  # Step 1: Verify settings data is complete (no deletions)
  agentapi-proxy helpers migrate --namespace agentapi-ui

  # Step 1 (subcommand form): Verify settings data is complete
  agentapi-proxy helpers migrate verify --namespace agentapi-ui

  # Step 2: Preview what would be deleted
  agentapi-proxy helpers migrate --namespace agentapi-ui --cleanup --dry-run

  # Step 3: Delete derived Secrets after verification
  agentapi-proxy helpers migrate --namespace agentapi-ui --cleanup`,
	RunE: runMigrate,
}

var migrateVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify that agentapi-settings-* Secrets contain all required data",
	Long: `Verify that agentapi-settings-* Secrets already contain all configuration data
that was previously split into separate derived Secrets.

This command lists all agentapi-settings-* Secrets and reports their contents
so you can confirm that all data (MCP servers, marketplaces, env vars, credentials)
is present before running cleanup.

Examples:
  # Verify settings data is complete
  agentapi-proxy helpers migrate verify --namespace agentapi-ui

  # Verbose output showing details of each Secret
  agentapi-proxy helpers migrate verify --namespace agentapi-ui --verbose`,
	RunE: runMigrateVerify,
}

func init() {
	migrateCmd.Flags().StringVar(&migrateNamespace, "namespace", "agentapi-ui",
		"Kubernetes namespace to operate in")
	migrateCmd.Flags().BoolVar(&migrateCleanup, "cleanup", false,
		"Delete derived Secrets (mcp-servers-*, marketplaces-*, agent-env-*) after verification")
	migrateCmd.Flags().BoolVar(&migrateDryRun, "dry-run", false,
		"Show cleanup targets without deleting (use with --cleanup)")
	migrateCmd.Flags().BoolVarP(&migrateVerbose, "verbose", "v", false,
		"Verbose output")

	migrateVerifyCmd.Flags().StringVar(&migrateVerifyNamespace, "namespace", "agentapi-ui",
		"Kubernetes namespace to operate in")
	migrateVerifyCmd.Flags().BoolVarP(&migrateVerifyVerbose, "verbose", "v", false,
		"Verbose output")

	migrateCmd.AddCommand(migrateVerifyCmd)
	HelpersCmd.AddCommand(migrateCmd)
}

// runVerifyPhase runs the Phase 1 verification logic against agentapi-settings-* Secrets.
// namespace and verbose are passed explicitly so the logic can be reused by both
// runMigrate (--namespace / --verbose flags) and runMigrateVerify (its own flags).
func runVerifyPhase(ctx context.Context, client *kubernetes.Clientset, namespace string, verbose bool) error {
	fmt.Printf("=== Phase 1: Verifying agentapi-settings-* Secrets (namespace: %s) ===\n", namespace)

	settingsList, err := client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "agentapi.proxy/settings=true",
	})
	if err != nil {
		return fmt.Errorf("failed to list settings secrets: %w", err)
	}

	fmt.Printf("Found %d agentapi-settings-* secret(s)\n", len(settingsList.Items))

	for _, secret := range settingsList.Items {
		rawJSON, ok := secret.Data["settings.json"]
		if !ok {
			fmt.Printf("  [WARN] %s: missing settings.json key\n", secret.Name)
			continue
		}

		var sj migrateSettingsJSON
		if err := json.Unmarshal(rawJSON, &sj); err != nil {
			fmt.Printf("  [WARN] %s: failed to parse settings.json: %v\n", secret.Name, err)
			continue
		}

		// Build a summary of what's present in this secret
		var parts []string
		if len(sj.MCPServers) > 0 {
			serverNames := make([]string, 0, len(sj.MCPServers))
			for k := range sj.MCPServers {
				serverNames = append(serverNames, k)
			}
			parts = append(parts, fmt.Sprintf("mcp_servers=%d(%s)", len(sj.MCPServers), strings.Join(serverNames, ",")))
		} else {
			parts = append(parts, "mcp_servers=0")
		}

		if len(sj.Marketplaces) > 0 {
			marketplaceNames := make([]string, 0, len(sj.Marketplaces))
			for k := range sj.Marketplaces {
				marketplaceNames = append(marketplaceNames, k)
			}
			parts = append(parts, fmt.Sprintf("marketplaces=%d(%s)", len(sj.Marketplaces), strings.Join(marketplaceNames, ",")))
		} else {
			parts = append(parts, "marketplaces=0")
		}

		if len(sj.EnvVars) > 0 {
			parts = append(parts, fmt.Sprintf("env_vars=%d", len(sj.EnvVars)))
		} else {
			parts = append(parts, "env_vars=0")
		}

		if sj.AuthMode != "" {
			parts = append(parts, fmt.Sprintf("auth_mode=%s", sj.AuthMode))
		}
		if sj.ClaudeCodeOAuthToken != "" {
			parts = append(parts, "oauth_token=set")
		}
		if sj.Bedrock != nil && sj.Bedrock.Enabled {
			parts = append(parts, "bedrock=enabled")
		}
		if len(sj.EnabledPlugins) > 0 {
			parts = append(parts, fmt.Sprintf("plugins=%d", len(sj.EnabledPlugins)))
		}

		fmt.Printf("  [OK] %s (name=%s): %s\n", secret.Name, sj.Name, strings.Join(parts, ", "))

		if verbose && len(sj.MCPServers) > 0 {
			for serverName, server := range sj.MCPServers {
				fmt.Printf("       MCP: %s (type=%s", serverName, server.Type)
				if server.URL != "" {
					fmt.Printf(", url=%s", server.URL)
				}
				if server.Command != "" {
					fmt.Printf(", cmd=%s", server.Command)
				}
				fmt.Println(")")
			}
		}

		if verbose && len(sj.EnvVars) > 0 {
			envKeys := make([]string, 0, len(sj.EnvVars))
			for k := range sj.EnvVars {
				envKeys = append(envKeys, k)
			}
			fmt.Printf("       EnvVars: %s\n", strings.Join(envKeys, ", "))
		}
	}

	return nil
}

func runMigrateVerify(cmd *cobra.Command, args []string) error {
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return runVerifyPhase(context.Background(), client, migrateVerifyNamespace, migrateVerifyVerbose)
}

func runMigrate(cmd *cobra.Command, args []string) error {
	// Build Kubernetes client
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	ctx := context.Background()

	// -------------------------------------------------------------------------
	// Phase 1: Verify agentapi-settings-* Secrets
	// -------------------------------------------------------------------------
	if err := runVerifyPhase(ctx, client, migrateNamespace, migrateVerbose); err != nil {
		return err
	}

	// -------------------------------------------------------------------------
	// Phase 2: Cleanup derived Secrets (only if --cleanup is set)
	// -------------------------------------------------------------------------
	if !migrateCleanup {
		fmt.Println("\nPhase 2 (cleanup) skipped.")
		fmt.Println("Run with --cleanup to delete derived Secrets.")
		fmt.Println("Run with --cleanup --dry-run to preview what would be deleted.")
		return nil
	}

	fmt.Println("\n=== Phase 2: Cleanup derived Secrets ===")
	if migrateDryRun {
		fmt.Println("[DRY-RUN] The following Secrets would be deleted:")
	}

	// Label selectors for each type of derived Secret
	derivedLabelSelectors := []struct {
		typeLabel   string
		description string
	}{
		{"agentapi.proxy/mcp-servers=true", "MCP servers secrets (mcp-servers-*)"},
		{"agentapi.proxy/marketplaces=true", "Marketplace secrets (marketplaces-*)"},
		{"agentapi.proxy/env=true", "Environment variable secrets (agent-env-*)"},
	}

	totalDeleted := 0
	totalWouldDelete := 0

	for _, derived := range derivedLabelSelectors {
		// Must be managed by settings to be safe to delete
		labelSelector := "agentapi.proxy/managed-by=settings," + derived.typeLabel

		secrets, err := client.CoreV1().Secrets(migrateNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return fmt.Errorf("failed to list %s: %w", derived.description, err)
		}

		if len(secrets.Items) == 0 {
			if migrateVerbose {
				fmt.Printf("  No %s found\n", derived.description)
			}
			continue
		}

		fmt.Printf("  %s: %d secret(s) found\n", derived.description, len(secrets.Items))
		for _, secret := range secrets.Items {
			if migrateDryRun {
				fmt.Printf("    [DRY-RUN] Would delete: %s\n", secret.Name)
				totalWouldDelete++
			} else {
				if err := client.CoreV1().Secrets(migrateNamespace).Delete(ctx, secret.Name, metav1.DeleteOptions{}); err != nil {
					fmt.Printf("    [ERROR] Failed to delete %s: %v\n", secret.Name, err)
				} else {
					fmt.Printf("    [DELETED] %s\n", secret.Name)
					totalDeleted++
				}
			}
		}
	}

	if migrateDryRun {
		fmt.Printf("\nDry-run complete: %d secret(s) would be deleted.\n", totalWouldDelete)
		fmt.Println("Run without --dry-run to actually delete them.")
	} else {
		fmt.Printf("\nCleanup complete: %d secret(s) deleted.\n", totalDeleted)
	}

	return nil
}
