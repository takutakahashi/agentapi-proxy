package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

type kvStoreMigrateOptions struct {
	namespace   string
	databaseURL string
	authToken   string
	dryRun      bool
	overwrite   bool
	output      string
}

type kvStoreMigrationEntry struct {
	Kind      kvstore.Kind `json:"kind"`
	Namespace string       `json:"namespace"`
	Key       string       `json:"key"`
	Status    string       `json:"status"`
}

type kvStoreMigrationResult struct {
	DryRun    bool                    `json:"dry_run"`
	Selected  int                     `json:"selected"`
	Copied    int                     `json:"copied"`
	Updated   int                     `json:"updated"`
	Skipped   int                     `json:"skipped"`
	Conflicts int                     `json:"conflicts"`
	Entries   []kvStoreMigrationEntry `json:"entries"`
}

func newKVStoreMigrateCommand() *cobra.Command {
	o := &kvStoreMigrateOptions{}
	command := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate application KV data from Kubernetes to libSQL",
		Long: `Copy application-owned Secret and ConfigMap documents from Kubernetes to libSQL.

The command selects only resource families used by the application KV boundary;
operational resources such as Pod-mounted Secrets and Helm ConfigMaps are not
copied. Source objects are never modified or deleted.

Stop application writes before running the migration. Re-running is safe when
the source has not changed: identical destination records are skipped. A
different existing destination record is reported as a conflict unless
--overwrite is specified. Use --dry-run to inspect destination conflicts
without writing anything.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(o.databaseURL) == "" {
				return errors.New("--database-url is required (or set AGENTAPI_KV_STORE_DATABASE_URL)")
			}
			if o.output != "text" && o.output != "json" {
				return fmt.Errorf("unsupported output %q (must be text or json)", o.output)
			}
			config, err := ctrl.GetConfig()
			if err != nil {
				return fmt.Errorf("get Kubernetes config: %w", err)
			}
			client, err := kubernetes.NewForConfig(config)
			if err != nil {
				return fmt.Errorf("create Kubernetes client: %w", err)
			}
			store, err := kvstore.NewLibSQLStore(cmd.Context(), o.databaseURL, o.authToken)
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()

			result, migrateErr := migrateKubernetesKV(cmd.Context(), client, store, *o)
			if err := writeKVStoreMigrationResult(cmd.OutOrStdout(), result, o.output); err != nil {
				return err
			}
			return migrateErr
		},
	}
	flags := command.Flags()
	flags.StringVarP(&o.namespace, "namespace", "n", resolveKubernetesNamespace(), "Kubernetes namespace containing application KV data")
	flags.StringVar(&o.databaseURL, "database-url", os.Getenv("AGENTAPI_KV_STORE_DATABASE_URL"), "libSQL database URL")
	flags.StringVar(&o.authToken, "auth-token", os.Getenv("AGENTAPI_KV_STORE_AUTH_TOKEN"), "libSQL authentication token")
	flags.BoolVar(&o.dryRun, "dry-run", false, "inspect records and conflicts without writing to libSQL")
	flags.BoolVar(&o.overwrite, "overwrite", false, "replace different records that already exist in libSQL")
	flags.StringVarP(&o.output, "output", "o", "text", "output format: text or json")
	return command
}

func migrateKubernetesKV(ctx context.Context, client kubernetes.Interface, destination kvstore.Store, options kvStoreMigrateOptions) (kvStoreMigrationResult, error) {
	result := kvStoreMigrationResult{DryRun: options.dryRun, Entries: []kvStoreMigrationEntry{}}
	records, err := collectApplicationKVRecords(ctx, client, options.namespace)
	if err != nil {
		return result, err
	}
	result.Selected = len(records)

	for _, source := range records {
		entry := kvStoreMigrationEntry{Kind: source.Kind, Namespace: source.Namespace, Key: source.Key}
		existing, getErr := destination.Get(ctx, source.Kind, source.Namespace, source.Key)
		switch {
		case errors.Is(getErr, kvstore.ErrNotFound):
			if options.dryRun {
				entry.Status = "would-copy"
				result.Copied++
			} else if _, createErr := destination.Create(ctx, source); createErr != nil {
				return result, fmt.Errorf("copy %s/%s: %w", source.Kind, source.Key, createErr)
			} else {
				entry.Status = "copied"
				result.Copied++
			}
		case getErr != nil:
			return result, fmt.Errorf("read destination %s/%s: %w", source.Kind, source.Key, getErr)
		case bytes.Equal(existing.Value, source.Value):
			entry.Status = "skipped"
			result.Skipped++
		case options.overwrite:
			if options.dryRun {
				entry.Status = "would-update"
				result.Updated++
			} else {
				source.Version = existing.Version
				if _, updateErr := destination.Update(ctx, source); updateErr != nil {
					return result, fmt.Errorf("update %s/%s: %w", source.Kind, source.Key, updateErr)
				}
				entry.Status = "updated"
				result.Updated++
			}
		default:
			entry.Status = "conflict"
			result.Conflicts++
		}
		result.Entries = append(result.Entries, entry)
	}
	if result.Conflicts > 0 {
		return result, fmt.Errorf("migration found %d conflicting destination record(s); rerun with --overwrite after review", result.Conflicts)
	}
	return result, nil
}

func collectApplicationKVRecords(ctx context.Context, client kubernetes.Interface, namespace string) ([]kvstore.Record, error) {
	secrets, err := client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list Kubernetes Secrets: %w", err)
	}
	configMaps, err := client.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list Kubernetes ConfigMaps: %w", err)
	}

	records := make([]kvstore.Record, 0, len(secrets.Items)+len(configMaps.Items))
	for i := range secrets.Items {
		object := &secrets.Items[i]
		if !isApplicationKVSecret(object) {
			continue
		}
		value, marshalErr := json.Marshal(object)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal Secret/%s: %w", object.Name, marshalErr)
		}
		records = append(records, kvstore.Record{Kind: kvstore.KindSecret, Namespace: namespace, Key: object.Name, Value: value})
	}
	for i := range configMaps.Items {
		object := &configMaps.Items[i]
		if !isApplicationKVConfigMap(object) {
			continue
		}
		value, marshalErr := json.Marshal(object)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal ConfigMap/%s: %w", object.Name, marshalErr)
		}
		records = append(records, kvstore.Record{Kind: kvstore.KindConfigMap, Namespace: namespace, Key: object.Name, Value: value})
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Kind == records[j].Kind {
			return records[i].Key < records[j].Key
		}
		return records[i].Kind < records[j].Kind
	})
	return records, nil
}

var applicationSecretLabels = []string{
	"agentapi.proxy/api-token",
	"agentapi.proxy/credentials",
	"agentapi.proxy/personal-api-key",
	"agentapi.proxy/schedule",
	"agentapi.proxy/session-profile",
	"agentapi.proxy/session-route",
	"agentapi.proxy/settings",
	"agentapi.proxy/slackbot",
	"agentapi.proxy/team-config",
	"agentapi.proxy/user-files",
	"agentapi.proxy/webhook",
}

func isApplicationKVSecret(secret *corev1.Secret) bool {
	for _, key := range applicationSecretLabels {
		if secret.Labels[key] == "true" {
			return true
		}
	}
	// The pre-v2 schedule store was a fixed-name Secret without labels.
	return secret.Name == "agentapi-schedules"
}

var applicationConfigMapTypes = map[string]struct{}{
	"memory":              {},
	"sandbox-domains":     {},
	"sandbox-policy":      {},
	"slack-channel-cache": {},
	"task":                {},
	"task-group":          {},
	"user-team-mapping":   {},
}

func isApplicationKVConfigMap(configMap *corev1.ConfigMap) bool {
	switch configMap.Name {
	case "agentapi-session-shares", "agentapi-user-team-mapping", "agentapi-slack-channel-cache":
		// Include fixed-name stores created before their identifying labels were
		// introduced or repaired by a subsequent write.
		return true
	}
	if configMap.Labels["agentapi.proxy/shares"] == "true" || configMap.Labels["agentapi.proxy/oauth-state"] == "true" {
		return true
	}
	_, ok := applicationConfigMapTypes[configMap.Labels["agentapi.proxy/type"]]
	return ok
}

func writeKVStoreMigrationResult(w io.Writer, result kvStoreMigrationResult, output string) error {
	if output == "json" {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	mode := "migration"
	if result.DryRun {
		mode = "dry run"
	}
	for _, entry := range result.Entries {
		if _, err := fmt.Fprintf(w, "%-12s %s/%s\n", entry.Status, entry.Kind, entry.Key); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "%s: selected=%d copied=%d updated=%d skipped=%d conflicts=%d\n",
		mode, result.Selected, result.Copied, result.Updated, result.Skipped, result.Conflicts)
	return err
}
