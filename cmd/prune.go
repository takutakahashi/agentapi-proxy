package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

// prune-stale-resources command flags
var (
	pruneNamespace string
	pruneDryRun    bool
	pruneVerbose   bool
)

var pruneStaleResourcesCmd = &cobra.Command{
	Use:   "prune-stale-resources",
	Short: "Delete stale Kubernetes resources for sessions missing their settings secret",
	Long: `Delete stale Kubernetes resources for agentapi sessions.

A session is considered stale when its settings secret
(agentapi-session-{id}-settings) no longer exists. This can happen when
the settings secret was deleted manually or a cleanup was interrupted.

The following resources are deleted for each stale session:
  - Deployment  agentapi-session-{id}
  - Service     agentapi-session-{id}-svc
  - PVC         agentapi-session-{id}-pvc               (if present)
  - Secret      agentapi-session-{id}-settings           (may already be gone)
  - Secret      agentapi-session-{id}-svc-webhook-payload (if present)
  - Secret      agentapi-session-{id}-svc-oneshot-settings (if present)

Use --dry-run to preview what would be deleted without making any changes.

Examples:
  # Preview stale sessions (no changes)
  agentapi-proxy helpers prune-stale-resources --namespace agentapi-ui --dry-run

  # Delete stale sessions
  agentapi-proxy helpers prune-stale-resources --namespace agentapi-ui

  # Delete with verbose output
  agentapi-proxy helpers prune-stale-resources --namespace agentapi-ui --verbose`,
	RunE: runPruneStaleResources,
}

func init() {
	pruneStaleResourcesCmd.Flags().StringVar(&pruneNamespace, "namespace", "agentapi-ui",
		"Kubernetes namespace to operate in")
	pruneStaleResourcesCmd.Flags().BoolVar(&pruneDryRun, "dry-run", false,
		"Show what would be deleted without actually deleting")
	pruneStaleResourcesCmd.Flags().BoolVarP(&pruneVerbose, "verbose", "v", false,
		"Verbose output")

	HelpersCmd.AddCommand(pruneStaleResourcesCmd)
}

func runPruneStaleResources(cmd *cobra.Command, args []string) error {
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	ctx := context.Background()
	ns := pruneNamespace

	if pruneDryRun {
		fmt.Printf("[DRY-RUN] Scanning namespace: %s\n", ns)
	} else {
		fmt.Printf("Scanning namespace: %s\n", ns)
	}

	// Step 1: List all session Services
	svcList, err := client.CoreV1().Services(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=agentapi-session,app.kubernetes.io/managed-by=agentapi-proxy",
	})
	if err != nil {
		return fmt.Errorf("failed to list session services: %w", err)
	}

	fmt.Printf("Found %d session service(s)\n", len(svcList.Items))

	// Step 2: Identify stale sessions by checking for their settings secret
	type staleSession struct {
		id          string
		serviceName string
	}

	var staleSessions []staleSession

	for _, svc := range svcList.Items {
		sessionID := svc.Labels["agentapi.proxy/session-id"]
		if sessionID == "" {
			if pruneVerbose {
				fmt.Printf("  [SKIP] Service %s: missing agentapi.proxy/session-id label\n", svc.Name)
			}
			continue
		}

		settingsSecretName := fmt.Sprintf("agentapi-session-%s-settings", sessionID)
		_, getErr := client.CoreV1().Secrets(ns).Get(ctx, settingsSecretName, metav1.GetOptions{})
		if getErr == nil {
			// Settings secret exists -> active session, skip
			if pruneVerbose {
				fmt.Printf("  [ACTIVE] session %s: settings secret %s found\n", sessionID, settingsSecretName)
			}
			continue
		}

		if !errors.IsNotFound(getErr) {
			fmt.Printf("  [WARN] session %s: error checking settings secret %s: %v\n", sessionID, settingsSecretName, getErr)
			continue
		}

		// Settings secret not found -> stale session
		fmt.Printf("  [STALE] session %s: settings secret %s not found\n", sessionID, settingsSecretName)
		staleSessions = append(staleSessions, staleSession{
			id:          sessionID,
			serviceName: svc.Name,
		})
	}

	if len(staleSessions) == 0 {
		fmt.Println("\nNo stale sessions found.")
		return nil
	}

	fmt.Printf("\nFound %d stale session(s).\n", len(staleSessions))
	if pruneDryRun {
		fmt.Println("[DRY-RUN] The following resources would be deleted:")
	} else {
		fmt.Println("Deleting stale resources...")
	}

	var deletedTotal, wouldDeleteTotal, errCount int

	for _, stale := range staleSessions {
		id := stale.id
		svcName := stale.serviceName

		fmt.Printf("\n  Session: %s\n", id)

		// Build the list of resources to clean up for this session.
		// Each entry is evaluated immediately, so id/svcName are correctly bound.
		type resource struct {
			kind string
			name string
			del  func() error
		}

		deploymentName := fmt.Sprintf("agentapi-session-%s", id)
		pvcName := fmt.Sprintf("agentapi-session-%s-pvc", id)
		settingsSecretName := fmt.Sprintf("agentapi-session-%s-settings", id)
		webhookSecretName := fmt.Sprintf("%s-webhook-payload", svcName)
		oneshotSecretName := fmt.Sprintf("%s-oneshot-settings", svcName)

		resources := []resource{
			{
				kind: "Service",
				name: svcName,
				del: func() error {
					return client.CoreV1().Services(ns).Delete(ctx, svcName, metav1.DeleteOptions{})
				},
			},
			{
				kind: "Deployment",
				name: deploymentName,
				del: func() error {
					return client.AppsV1().Deployments(ns).Delete(ctx, deploymentName, metav1.DeleteOptions{})
				},
			},
			{
				kind: "PVC",
				name: pvcName,
				del: func() error {
					return client.CoreV1().PersistentVolumeClaims(ns).Delete(ctx, pvcName, metav1.DeleteOptions{})
				},
			},
			{
				kind: "Secret (settings)",
				name: settingsSecretName,
				del: func() error {
					return client.CoreV1().Secrets(ns).Delete(ctx, settingsSecretName, metav1.DeleteOptions{})
				},
			},
			{
				kind: "Secret (webhook-payload)",
				name: webhookSecretName,
				del: func() error {
					return client.CoreV1().Secrets(ns).Delete(ctx, webhookSecretName, metav1.DeleteOptions{})
				},
			},
			{
				kind: "Secret (oneshot-settings)",
				name: oneshotSecretName,
				del: func() error {
					return client.CoreV1().Secrets(ns).Delete(ctx, oneshotSecretName, metav1.DeleteOptions{})
				},
			},
		}

		for _, r := range resources {
			if pruneDryRun {
				fmt.Printf("    [DRY-RUN] Would delete %s: %s\n", r.kind, r.name)
				wouldDeleteTotal++
			} else {
				if delErr := r.del(); delErr != nil {
					if errors.IsNotFound(delErr) {
						if pruneVerbose {
							fmt.Printf("    [SKIP] %s %s: not found\n", r.kind, r.name)
						}
					} else {
						fmt.Printf("    [ERROR] Failed to delete %s %s: %v\n", r.kind, r.name, delErr)
						errCount++
					}
				} else {
					fmt.Printf("    [DELETED] %s: %s\n", r.kind, r.name)
					deletedTotal++
				}
			}
		}
	}

	fmt.Println()

	if pruneDryRun {
		fmt.Printf("Dry-run complete: %d resource(s) would be deleted across %d stale session(s).\n",
			wouldDeleteTotal, len(staleSessions))
		fmt.Println("Run without --dry-run to actually delete them.")
	} else {
		fmt.Printf("Pruning complete: %d resource(s) deleted across %d stale session(s).\n",
			deletedTotal, len(staleSessions))
		if errCount > 0 {
			fmt.Printf("Errors encountered: %d\n", errCount)
			return fmt.Errorf("%d deletion error(s) occurred", errCount)
		}
	}

	return nil
}
