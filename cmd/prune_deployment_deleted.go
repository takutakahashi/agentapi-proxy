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

// prune-orphaned-resources command flags
var (
	orphanedNamespace string
	orphanedDryRun    bool
	orphanedVerbose   bool
)

var pruneOrphanedResourcesCmd = &cobra.Command{
	Use:   "prune-orphaned-resources",
	Short: "Delete stale Kubernetes resources left behind after a session Deployment was deleted",
	Long: `Delete stale Kubernetes resources for agentapi sessions whose Deployment no longer exists.

A session is considered orphaned when its Deployment
(agentapi-session-{id}) no longer exists but other associated resources
(Service, PVC, Secrets) are still present. This can happen when a
Deployment is deleted manually or an incomplete cleanup leaves residual objects.

The following resources are deleted for each orphaned session:
  - Deployment  agentapi-session-{id}               (may already be gone)
  - Service     agentapi-session-{id}-svc
  - PVC         agentapi-session-{id}-pvc            (if present)
  - Secret      agentapi-session-{id}-settings        (if present)
  - Secret      agentapi-session-{id}-svc-webhook-payload (if present)
  - Secret      agentapi-session-{id}-svc-oneshot-settings (if present)

Use --dry-run to preview what would be deleted without making any changes.

Examples:
  # Preview orphaned sessions (no changes)
  agentapi-proxy helpers prune-orphaned-resources --namespace agentapi-ui --dry-run

  # Delete orphaned sessions
  agentapi-proxy helpers prune-orphaned-resources --namespace agentapi-ui

  # Delete with verbose output
  agentapi-proxy helpers prune-orphaned-resources --namespace agentapi-ui --verbose`,
	RunE: runPruneOrphanedResources,
}

func init() {
	pruneOrphanedResourcesCmd.Flags().StringVar(&orphanedNamespace, "namespace", "agentapi-ui",
		"Kubernetes namespace to operate in")
	pruneOrphanedResourcesCmd.Flags().BoolVar(&orphanedDryRun, "dry-run", false,
		"Show what would be deleted without actually deleting")
	pruneOrphanedResourcesCmd.Flags().BoolVarP(&orphanedVerbose, "verbose", "v", false,
		"Verbose output")

	HelpersCmd.AddCommand(pruneOrphanedResourcesCmd)
}

func runPruneOrphanedResources(cmd *cobra.Command, args []string) error {
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	ctx := context.Background()
	ns := orphanedNamespace

	if orphanedDryRun {
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

	// Step 2: Identify orphaned sessions by checking for their Deployment
	type orphanedSession struct {
		id          string
		serviceName string
	}

	var orphanedSessions []orphanedSession

	for _, svc := range svcList.Items {
		sessionID := svc.Labels["agentapi.proxy/session-id"]
		if sessionID == "" {
			if orphanedVerbose {
				fmt.Printf("  [SKIP] Service %s: missing agentapi.proxy/session-id label\n", svc.Name)
			}
			continue
		}

		deploymentName := fmt.Sprintf("agentapi-session-%s", sessionID)
		_, getErr := client.AppsV1().Deployments(ns).Get(ctx, deploymentName, metav1.GetOptions{})
		if getErr == nil {
			// Deployment exists -> active session, skip
			if orphanedVerbose {
				fmt.Printf("  [ACTIVE] session %s: Deployment %s found\n", sessionID, deploymentName)
			}
			continue
		}

		if !errors.IsNotFound(getErr) {
			fmt.Printf("  [WARN] session %s: error checking Deployment %s: %v\n", sessionID, deploymentName, getErr)
			continue
		}

		// Deployment not found -> orphaned session
		fmt.Printf("  [ORPHANED] session %s: Deployment %s not found\n", sessionID, deploymentName)
		orphanedSessions = append(orphanedSessions, orphanedSession{
			id:          sessionID,
			serviceName: svc.Name,
		})
	}

	if len(orphanedSessions) == 0 {
		fmt.Println("\nNo orphaned sessions found.")
		return nil
	}

	fmt.Printf("\nFound %d orphaned session(s).\n", len(orphanedSessions))
	if orphanedDryRun {
		fmt.Println("[DRY-RUN] The following resources would be deleted:")
	} else {
		fmt.Println("Deleting orphaned resources...")
	}

	var deletedTotal, wouldDeleteTotal, errCount int

	for _, orphaned := range orphanedSessions {
		id := orphaned.id
		svcName := orphaned.serviceName

		fmt.Printf("\n  Session: %s\n", id)

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
				kind: "Deployment",
				name: deploymentName,
				del: func() error {
					return client.AppsV1().Deployments(ns).Delete(ctx, deploymentName, metav1.DeleteOptions{})
				},
			},
			{
				kind: "Service",
				name: svcName,
				del: func() error {
					return client.CoreV1().Services(ns).Delete(ctx, svcName, metav1.DeleteOptions{})
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
			if orphanedDryRun {
				fmt.Printf("    [DRY-RUN] Would delete %s: %s\n", r.kind, r.name)
				wouldDeleteTotal++
			} else {
				if delErr := r.del(); delErr != nil {
					if errors.IsNotFound(delErr) {
						if orphanedVerbose {
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

	if orphanedDryRun {
		fmt.Printf("Dry-run complete: %d resource(s) would be deleted across %d orphaned session(s).\n",
			wouldDeleteTotal, len(orphanedSessions))
		fmt.Println("Run without --dry-run to actually delete them.")
	} else {
		fmt.Printf("Pruning complete: %d resource(s) deleted across %d orphaned session(s).\n",
			deletedTotal, len(orphanedSessions))
		if errCount > 0 {
			fmt.Printf("Errors encountered: %d\n", errCount)
			return fmt.Errorf("%d deletion error(s) occurred", errCount)
		}
	}

	return nil
}
