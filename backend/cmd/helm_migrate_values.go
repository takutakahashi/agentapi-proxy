package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

type helmMigrateValuesOptions struct {
	input                 string
	output                string
	namespace             string
	release               string
	workerControlSecret   string
	managerInternalSecret string
	encryptionSecret      string
	provisionerSecret     string
	migrateSecrets        bool
	force                 bool
	kubeClient            kubernetes.Interface
}

func newHelmMigrateValuesCommand() *cobra.Command {
	o := &helmMigrateValuesOptions{}
	cmd := &cobra.Command{
		Use:   "migrate-values",
		Short: "Convert legacy proxy values into separated API, worker, and session-manager values",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHelmMigrateValues(cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), o)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.input, "input", "f", "-", "legacy values YAML file, or - for stdin")
	f.StringVarP(&o.output, "output", "o", "-", "migrated values YAML file, or - for stdout")
	f.StringVarP(&o.namespace, "namespace", "n", "default", "target Kubernetes namespace")
	f.StringVar(&o.release, "release", "agentapi-proxy", "target Helm release name")
	f.StringVar(&o.workerControlSecret, "worker-control-secret", "agentapi-worker-control", "Secret shared by API and worker")
	f.StringVar(&o.managerInternalSecret, "manager-internal-secret", "agentapi-session-manager-internal", "Secret shared by API and session-manager")
	f.StringVar(&o.encryptionSecret, "encryption-secret", "agentapi-application-encryption", "application encryption Secret")
	f.StringVar(&o.provisionerSecret, "provisioner-secret", "agentapi-provisioner-token", "session provisioner Secret")
	f.BoolVar(&o.migrateSecrets, "migrate-secrets", false, "create or migrate role Secrets in the target Kubernetes namespace")
	f.BoolVar(&o.force, "force", false, "replace existing api, worker, and sessionManager sections")
	return cmd
}

func runHelmMigrateValues(stdin io.Reader, stdout, stderr io.Writer, o *helmMigrateValuesOptions) error {
	if strings.TrimSpace(o.namespace) == "" || strings.TrimSpace(o.release) == "" {
		return errors.New("namespace and release must not be empty")
	}
	if o.migrateSecrets && len(nonEmptyStrings(o.workerControlSecret, o.managerInternalSecret, o.encryptionSecret, o.provisionerSecret)) != 4 {
		return errors.New("all Secret names must be non-empty when --migrate-secrets is set")
	}
	data, err := readValuesInput(stdin, o.input)
	if err != nil {
		return err
	}
	var values map[string]any
	if err := yaml.Unmarshal(data, &values); err != nil {
		return fmt.Errorf("parse legacy values: %w", err)
	}
	if values == nil {
		values = map[string]any{}
	}
	for _, key := range []string{"api", "worker", "sessionManager"} {
		if _, exists := values[key]; exists && !o.force {
			return fmt.Errorf("values already contain %q; rerun with --force to replace separated role sections", key)
		}
	}

	legacyKV := cloneMap(nestedMap(values, "config", "kvStore"))
	if len(legacyKV) == 0 {
		legacyKV = map[string]any{"backend": "kubernetes", "namespace": o.namespace}
	}
	if stringValue(legacyKV["backend"]) == "kubernetes" {
		legacyKV["namespace"] = o.namespace
	} else if stringValue(legacyKV["namespace"]) == "" {
		// Before role separation, libSQL-backed API repositories used the
		// Kubernetes session namespace as their logical KV namespace. Preserve
		// that lookup boundary or existing resources appear to disappear after
		// migration even though they remain in the same database.
		legacyNamespace := stringValue(nestedValue(values, "kubernetesSession", "namespace"))
		if legacyNamespace == "" {
			legacyNamespace = o.namespace
		}
		legacyKV["namespace"] = legacyNamespace
	}
	legacyRedis := migratedRedis(values)
	image := cloneMap(nestedMap(values, "image"))
	serviceName := stringValue(values["fullnameOverride"])
	if serviceName == "" {
		serviceName = o.release
	}

	api := map[string]any{
		"replicaCount":  valueOr(values["replicaCount"], 1),
		"image":         cloneMap(image),
		"kvStore":       cloneMap(legacyKV),
		"redis":         cloneMap(legacyRedis),
		"encryption":    map[string]any{"keySecretRef": secretRef(o.encryptionSecret, "encryption-key")},
		"workerControl": map[string]any{"tokenSecretRef": secretRef(o.workerControlSecret, "token")},
		"sessionManager": map[string]any{
			"url":            fmt.Sprintf("http://%s-session-manager.%s.svc.cluster.local:8080", serviceName, o.namespace),
			"tokenSecretRef": secretRef(o.managerInternalSecret, "token"),
		},
	}
	copyKeys(api, values, "imagePullSecrets", "podAnnotations", "podLabels", "podSecurityContext", "securityContext", "resources", "nodeSelector", "tolerations", "affinity", "env", "envFrom")

	workerEnabled := legacyWorkerEnabled(values)
	worker := map[string]any{
		"enabled":      workerEnabled,
		"replicaCount": 1,
		"image":        cloneMap(image),
		"kvStore":      cloneMap(legacyKV),
		"redis":        cloneMap(legacyRedis),
		"controlApi": map[string]any{
			"url":            fmt.Sprintf("http://%s.%s.svc.cluster.local:8080", serviceName, o.namespace),
			"tokenSecretRef": secretRef(o.workerControlSecret, "token"),
		},
		"schedule":        migratedWorkerSection(values, "scheduleWorker", "schedule"),
		"slackbotCleanup": migratedWorkerSection(values, "slackbotCleanupWorker", "slackbotCleanup"),
		"stockInventory":  migratedWorkerSection(values, "stockInventoryWorker", "stockInventory"),
		"slack":           migratedSlack(values),
	}
	copyKeys(worker, values, "imagePullSecrets", "podAnnotations", "podLabels", "podSecurityContext", "securityContext", "resources", "nodeSelector", "tolerations", "affinity", "env", "envFrom")

	managerEnabled := boolValue(nestedValue(values, "kubernetesSession", "enabled"))
	manager := map[string]any{
		"enabled":            managerEnabled,
		"replicaCount":       1,
		"port":               8080,
		"image":              cloneMap(image),
		"internalApi":        map[string]any{"tokenSecretRef": secretRef(o.managerInternalSecret, "token")},
		"encryption":         map[string]any{"keySecretRef": secretRef(o.encryptionSecret, "encryption-key")},
		"kvStore":            cloneMap(legacyKV),
		"redis":              cloneMap(legacyRedis),
		"kubernetesSession":  cloneMap(nestedMap(values, "kubernetesSession")),
		"sessionPersistence": cloneMap(nestedMap(values, "sessionPersistence")),
		"sessionControl":     cloneMap(nestedMap(values, "sessionControl")),
		"scia":               migratedManagerSCIA(values),
		"github":             cloneMap(nestedMap(values, "github")),
	}
	copyKeys(manager, values, "imagePullSecrets", "podAnnotations", "podLabels", "podSecurityContext", "securityContext", "resources", "nodeSelector", "tolerations", "affinity", "env", "envFrom")
	provisioner := ensureMap(nestedMap(manager, "kubernetesSession"), "provisioner")
	if stringValue(nestedValue(provisioner, "tokenSecretRef", "name")) == "" {
		provisioner["tokenSecretRef"] = secretRef(o.provisionerSecret, "provisioner-token")
	}

	values["api"], values["worker"], values["sessionManager"] = api, worker, manager
	if o.migrateSecrets {
		client := o.kubeClient
		if client == nil {
			restConfig, configErr := config.GetConfig()
			if configErr != nil {
				return fmt.Errorf("load Kubernetes configuration: %w", configErr)
			}
			client, configErr = kubernetes.NewForConfig(restConfig)
			if configErr != nil {
				return fmt.Errorf("create Kubernetes client: %w", configErr)
			}
		}
		if err := migrateRoleSecrets(context.Background(), client, values, o, stderr); err != nil {
			return err
		}
	}
	result, err := yaml.Marshal(values)
	if err != nil {
		return fmt.Errorf("encode migrated values: %w", err)
	}
	if o.output == "-" {
		if _, err := stdout.Write(result); err != nil {
			return fmt.Errorf("write migrated values: %w", err)
		}
	} else {
		if err := os.WriteFile(o.output, result, 0o600); err != nil {
			return fmt.Errorf("write migrated values %s: %w", o.output, err)
		}
	}
	if _, err := fmt.Fprintf(stderr, "Migrated legacy values: worker.enabled=%t sessionManager.enabled=%t\n", workerEnabled, managerEnabled); err != nil {
		return fmt.Errorf("write migration status: %w", err)
	}
	secretNames := nonEmptyStrings(o.workerControlSecret, o.managerInternalSecret, o.encryptionSecret, o.provisionerSecret)
	if len(secretNames) > 0 {
		if o.migrateSecrets {
			if _, err := fmt.Fprintf(stderr, "Migrated Secrets %q in namespace %q.\n", strings.Join(secretNames, `", "`), o.namespace); err != nil {
				return fmt.Errorf("write Secret migration status: %w", err)
			}
		} else {
			if _, err := fmt.Fprintf(stderr, "Ensure Secrets %q exist before upgrade, or rerun with --migrate-secrets.\n", strings.Join(secretNames, `", "`)); err != nil {
				return fmt.Errorf("write Secret migration warning: %w", err)
			}
		}
	}
	if workerEnabled || managerEnabled {
		if _, err := fmt.Fprintf(stderr, "Verify image %q contains the worker and session-manager subcommands before upgrade.\n", imageReference(image)); err != nil {
			return fmt.Errorf("write image migration warning: %w", err)
		}
	}
	return nil
}

func readValuesInput(stdin io.Reader, path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(stdin)
		return data, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read legacy values %s: %w", path, err)
	}
	return data, nil
}

func migratedRedis(values map[string]any) map[string]any {
	redis := cloneMap(nestedMap(values, "externalRedis"))
	if len(redis) == 0 {
		redis = map[string]any{}
	}
	if _, ok := redis["db"]; !ok {
		redis["db"] = 0
	}
	if _, ok := redis["tlsEnabled"]; !ok {
		redis["tlsEnabled"] = false
	}
	if _, ok := redis["dialTimeout"]; !ok {
		redis["dialTimeout"] = "5s"
	}
	if _, ok := redis["readTimeout"]; !ok {
		redis["readTimeout"] = "3s"
	}
	if _, ok := redis["writeTimeout"]; !ok {
		redis["writeTimeout"] = "3s"
	}
	return redis
}

func migratedWorkerSection(values map[string]any, legacy, current string) map[string]any {
	section := cloneMap(nestedMap(values, legacy))
	if len(section) == 0 {
		section = cloneMap(nestedMap(values, "config", legacy))
	}
	if len(section) == 0 {
		section = cloneMap(nestedMap(values, "worker", current))
	}
	if len(section) == 0 {
		section = map[string]any{"enabled": false}
	}
	return section
}

func legacyWorkerEnabled(values map[string]any) bool {
	for _, key := range []string{"scheduleWorker", "slackbotCleanupWorker", "stockInventoryWorker"} {
		if boolValue(nestedValue(values, key, "enabled")) || boolValue(nestedValue(values, "config", key, "enabled")) {
			return true
		}
	}
	return stringValue(nestedValue(values, "kubernetesSession", "slackIntegration", "botToken", "secretName")) != ""
}

func migratedSlack(values map[string]any) map[string]any {
	name := stringValue(nestedValue(values, "kubernetesSession", "slackIntegration", "botToken", "secretName"))
	return map[string]any{"baseUrl": stringValue(nestedValue(values, "config", "webhook", "baseUrl")), "tokenSecretRef": map[string]any{"name": name, "appTokenKey": "app-token", "botTokenKey": "bot-token"}, "dryRun": false}
}

func migratedManagerSCIA(values map[string]any) map[string]any {
	scia := cloneMap(nestedMap(values, "scia"))
	delete(scia, "image")
	delete(scia, "replicaCount")
	delete(scia, "service")
	delete(scia, "resources")
	delete(scia, "podAnnotations")
	delete(scia, "podLabels")
	delete(scia, "nodeSelector")
	delete(scia, "tolerations")
	delete(scia, "affinity")
	return scia
}

func secretRef(name, key string) map[string]any { return map[string]any{"name": name, "key": key} }

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	data, _ := yaml.Marshal(source)
	var result map[string]any
	_ = yaml.Unmarshal(data, &result)
	return result
}

func nestedMap(root map[string]any, path ...string) map[string]any {
	value := nestedValue(root, path...)
	result, _ := value.(map[string]any)
	return result
}

func nestedValue(root map[string]any, path ...string) any {
	var current any = root
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[key]
	}
	return current
}

func ensureMap(root map[string]any, key string) map[string]any {
	if value, ok := root[key].(map[string]any); ok {
		return value
	}
	value := map[string]any{}
	root[key] = value
	return value
}

func copyKeys(destination, source map[string]any, keys ...string) {
	for _, key := range keys {
		if value, ok := source[key]; ok {
			destination[key] = value
		}
	}
}

func valueOr(value, fallback any) any {
	if value == nil {
		return fallback
	}
	return value
}
func stringValue(value any) string { result, _ := value.(string); return result }
func boolValue(value any) bool     { result, _ := value.(bool); return result }

func nonEmptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result
}

func imageReference(image map[string]any) string {
	repository, tag := stringValue(image["repository"]), stringValue(image["tag"])
	if tag == "" {
		return repository
	}
	return repository + ":" + tag
}
