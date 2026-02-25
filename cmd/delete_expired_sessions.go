package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

// delete-expired-sessions command flags
var (
	expiredNamespace string
	expiredDays      int
	expiredDryRun    bool
	expiredVerbose   bool
)

var deleteExpiredSessionsCmd = &cobra.Command{
	Use:   "delete-expired-sessions",
	Short: "Delete Kubernetes resources for sessions created more than N days ago",
	Long: `Delete all Kubernetes resources associated with sessions that were created
more than the specified number of days ago.

A session's creation time is read from the annotation agentapi.proxy/created-at
on its Kubernetes Service resource.

The following resources are deleted for each expired session:
  - Deployment  agentapi-session-{id}
  - Service     agentapi-session-{id}-svc
  - PVC         agentapi-session-{id}-pvc               (if present)
  - Secret      agentapi-session-{id}-settings
  - Secret      agentapi-session-{id}-svc-webhook-payload (if present)
  - Secret      agentapi-session-{id}-svc-oneshot-settings (if present)

Use --dry-run to preview what would be deleted without making any changes.

Examples:
  # Preview sessions older than 7 days (no changes)
  agentapi-proxy helpers delete-expired-sessions --namespace agentapi-ui --days 7 --dry-run

  # Delete sessions older than 30 days
  agentapi-proxy helpers delete-expired-sessions --namespace agentapi-ui --days 30

  # Delete sessions older than 14 days with verbose output
  agentapi-proxy helpers delete-expired-sessions --namespace agentapi-ui --days 14 --verbose`,
	RunE: runDeleteExpiredSessions,
}

func init() {
	deleteExpiredSessionsCmd.Flags().StringVar(&expiredNamespace, "namespace", "agentapi-ui",
		"Kubernetes namespace to operate in")
	deleteExpiredSessionsCmd.Flags().IntVar(&expiredDays, "days", 30,
		"Delete sessions created more than this many days ago")
	deleteExpiredSessionsCmd.Flags().BoolVar(&expiredDryRun, "dry-run", false,
		"Show what would be deleted without actually deleting")
	deleteExpiredSessionsCmd.Flags().BoolVarP(&expiredVerbose, "verbose", "v", false,
		"Verbose output")

	HelpersCmd.AddCommand(deleteExpiredSessionsCmd)
}

func runDeleteExpiredSessions(cmd *cobra.Command, args []string) error {
	if expiredDays <= 0 {
		return fmt.Errorf("--days must be a positive integer")
	}

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	ctx := context.Background()
	ns := expiredNamespace
	threshold := time.Now().AddDate(0, 0, -expiredDays)

	if expiredDryRun {
		fmt.Printf("[DRY-RUN] Scanning namespace: %s (threshold: sessions created before %s)\n",
			ns, threshold.Format(time.RFC3339))
	} else {
		fmt.Printf("Scanning namespace: %s (threshold: sessions created before %s)\n",
			ns, threshold.Format(time.RFC3339))
	}

	// List all session Services
	svcList, err := client.CoreV1().Services(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=agentapi-session,app.kubernetes.io/managed-by=agentapi-proxy",
	})
	if err != nil {
		return fmt.Errorf("failed to list session services: %w", err)
	}

	fmt.Printf("Found %d session service(s)\n", len(svcList.Items))

	type expiredSession struct {
		id          string
		serviceName string
		createdAt   time.Time
	}

	var expiredSessions []expiredSession

	for _, svc := range svcList.Items {
		sessionID := svc.Labels["agentapi.proxy/session-id"]
		if sessionID == "" {
			if expiredVerbose {
				fmt.Printf("  [SKIP] Service %s: missing agentapi.proxy/session-id label\n", svc.Name)
			}
			continue
		}

		// Parse creation time from annotation
		createdAt := svc.CreationTimestamp.Time // Fall back to Kubernetes resource creation time
		if createdAtStr, ok := svc.Annotations["agentapi.proxy/created-at"]; ok {
			if parsed, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
				createdAt = parsed
			} else {
				if expiredVerbose {
					fmt.Printf("  [WARN] session %s: failed to parse created-at annotation %q: %v\n",
						sessionID, createdAtStr, err)
				}
			}
		}

		if createdAt.After(threshold) {
			// Session is still within the retention window
			if expiredVerbose {
				fmt.Printf("  [ACTIVE] session %s: created at %s (within %d-day window)\n",
					sessionID, createdAt.Format(time.RFC3339), expiredDays)
			}
			continue
		}

		fmt.Printf("  [EXPIRED] session %s: created at %s (older than %d days)\n",
			sessionID, createdAt.Format(time.RFC3339), expiredDays)
		expiredSessions = append(expiredSessions, expiredSession{
			id:          sessionID,
			serviceName: svc.Name,
			createdAt:   createdAt,
		})
	}

	if len(expiredSessions) == 0 {
		fmt.Println("\nNo expired sessions found.")
		return nil
	}

	fmt.Printf("\nFound %d expired session(s).\n", len(expiredSessions))
	if expiredDryRun {
		fmt.Println("[DRY-RUN] The following resources would be deleted:")
	} else {
		fmt.Println("Deleting expired resources...")
	}

	var deletedTotal, wouldDeleteTotal, errCount int

	for _, expired := range expiredSessions {
		id := expired.id
		svcName := expired.serviceName

		fmt.Printf("\n  Session: %s (created: %s)\n", id, expired.createdAt.Format(time.RFC3339))

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
			if expiredDryRun {
				fmt.Printf("    [DRY-RUN] Would delete %s: %s\n", r.kind, r.name)
				wouldDeleteTotal++
			} else {
				if delErr := r.del(); delErr != nil {
					if errors.IsNotFound(delErr) {
						if expiredVerbose {
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

	if expiredDryRun {
		fmt.Printf("Dry-run complete: %d resource(s) would be deleted across %d expired session(s).\n",
			wouldDeleteTotal, len(expiredSessions))
		fmt.Println("Run without --dry-run to actually delete them.")
	} else {
		fmt.Printf("Cleanup complete: %d resource(s) deleted across %d expired session(s).\n",
			deletedTotal, len(expiredSessions))
		if errCount > 0 {
			fmt.Printf("Errors encountered: %d\n", errCount)
			return fmt.Errorf("%d deletion error(s) occurred", errCount)
		}
	}

	return nil
}
