package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRunHelmMigrateValuesSeparatesLegacyRoles(t *testing.T) {
	input := `
replicaCount: 2
fullnameOverride: legacy-backend
image: {repository: example/backend, tag: old}
config:
  webhook: {baseUrl: https://api.example.com}
  kvStore:
    backend: libsql
    namespace: logical
    databaseUrlSecretRef: {name: shared-db, key: url}
externalRedis: {addr: redis.example:6379}
kubernetesSession:
  enabled: true
  namespace: sessions
  slackIntegration:
    botToken: {secretName: slack-legacy}
  provisioner:
    tokenSecretRef: {name: existing-provisioner, key: token}
scheduleWorker:
  enabled: true
  checkInterval: 10s
slackbotCleanupWorker: {enabled: true, sessionTTL: 24h}
stockInventoryWorker: {enabled: false}
sessionPersistence: {backend: s3, s3: {bucket: sessions}}
github: {tokenRef: {name: github, key: token}}
ingress: {enabled: true}
envFrom: [{secretRef: {name: runtime-env}}]
env: [{name: AWS_REGION, value: test-region}]
`
	var stdout, stderr bytes.Buffer
	options := &helmMigrateValuesOptions{
		input: "-", output: "-", namespace: "customer", release: "backend",
		workerControlSecret: "worker-token", managerInternalSecret: "manager-token",
		encryptionSecret: "encryption", provisionerSecret: "fallback-provisioner",
	}
	if err := runHelmMigrateValues(strings.NewReader(input), &stdout, &stderr, options); err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := yaml.Unmarshal(stdout.Bytes(), &values); err != nil {
		t.Fatal(err)
	}
	if !boolValue(nestedValue(values, "worker", "enabled")) {
		t.Fatal("worker was not enabled")
	}
	if !boolValue(nestedValue(values, "sessionManager", "enabled")) {
		t.Fatal("session manager was not enabled")
	}
	if got := stringValue(nestedValue(values, "api", "kvStore", "databaseUrlSecretRef", "name")); got != "shared-db" {
		t.Fatalf("api KV secret=%q", got)
	}
	if got := stringValue(nestedValue(values, "worker", "schedule", "checkInterval")); got != "10s" {
		t.Fatalf("schedule interval=%q", got)
	}
	if got := stringValue(nestedValue(values, "sessionManager", "kubernetesSession", "provisioner", "tokenSecretRef", "name")); got != "existing-provisioner" {
		t.Fatalf("provisioner secret=%q", got)
	}
	if got := stringValue(nestedValue(values, "api", "sessionManager", "url")); got != "http://legacy-backend-session-manager.customer.svc.cluster.local:8080" {
		t.Fatalf("manager URL=%q", got)
	}
	if got := stringValue(nestedValue(values, "worker", "slack", "tokenSecretRef", "name")); got != "slack-legacy" {
		t.Fatalf("Slack secret=%q", got)
	}
	if !boolValue(nestedValue(values, "ingress", "enabled")) {
		t.Fatal("legacy ingress values were not preserved")
	}
	for _, role := range []string{"api", "worker", "sessionManager"} {
		roleValues := nestedMap(values, role)
		if len(roleValues["env"].([]any)) != 1 || len(roleValues["envFrom"].([]any)) != 1 {
			t.Fatalf("%s did not inherit legacy env/envFrom: %#v", role, roleValues)
		}
	}
	if !strings.Contains(stderr.String(), "Ensure Secrets") {
		t.Fatalf("missing credential warning: %s", stderr.String())
	}
}

func TestRunHelmMigrateValuesRemapsKubernetesNamespace(t *testing.T) {
	input := `config: {kvStore: {backend: kubernetes, namespace: old}}
kubernetesSession: {enabled: false}
`
	var stdout bytes.Buffer
	o := &helmMigrateValuesOptions{input: "-", output: "-", namespace: "target", release: "backend", workerControlSecret: "w", managerInternalSecret: "m", encryptionSecret: "e", provisionerSecret: "p"}
	if err := runHelmMigrateValues(strings.NewReader(input), &stdout, &bytes.Buffer{}, o); err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := yaml.Unmarshal(stdout.Bytes(), &values); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"api", "worker", "sessionManager"} {
		if got := stringValue(nestedValue(values, role, "kvStore", "namespace")); got != "target" {
			t.Fatalf("%s namespace=%q", role, got)
		}
	}
}

func TestRunHelmMigrateValuesPreservesLegacyLibSQLLogicalNamespace(t *testing.T) {
	input := `config: {kvStore: {backend: libsql, databaseUrlSecretRef: {name: db, key: url}}}
kubernetesSession: {enabled: true, namespace: existing-resources}
`
	var stdout bytes.Buffer
	o := &helmMigrateValuesOptions{input: "-", output: "-", namespace: "release-namespace", release: "backend", workerControlSecret: "w", managerInternalSecret: "m", encryptionSecret: "e", provisionerSecret: "p"}
	if err := runHelmMigrateValues(strings.NewReader(input), &stdout, &bytes.Buffer{}, o); err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := yaml.Unmarshal(stdout.Bytes(), &values); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"api", "worker", "sessionManager"} {
		if got := stringValue(nestedValue(values, role, "kvStore", "namespace")); got != "existing-resources" {
			t.Fatalf("%s namespace=%q", role, got)
		}
	}
}

func TestRunHelmMigrateValuesRefusesExistingRoleValues(t *testing.T) {
	o := &helmMigrateValuesOptions{input: "-", output: "-", namespace: "target", release: "backend"}
	err := runHelmMigrateValues(strings.NewReader("api: {replicaCount: 3}\n"), &bytes.Buffer{}, &bytes.Buffer{}, o)
	if err == nil || !strings.Contains(err.Error(), "already contain") {
		t.Fatalf("error=%v", err)
	}
}

func TestRunHelmMigrateValuesMigratesSecrets(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "provisioner", Namespace: "target"},
		Data:       map[string][]byte{"token": []byte("legacy-provisioner")},
	})
	o := &helmMigrateValuesOptions{
		input: "-", output: "-", namespace: "target", release: "backend",
		workerControlSecret: "worker", managerInternalSecret: "manager",
		encryptionSecret: "encryption", provisionerSecret: "provisioner",
		migrateSecrets: true, kubeClient: client,
	}
	input := `config: {encryption: {key: legacy-encryption}}
kubernetesSession: {enabled: true}
scheduleWorker: {enabled: true}
`
	if err := runHelmMigrateValues(strings.NewReader(input), &bytes.Buffer{}, &bytes.Buffer{}, o); err != nil {
		t.Fatal(err)
	}

	assertSecretKey := func(name, key, want string) {
		t.Helper()
		secret, err := client.CoreV1().Secrets("target").Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := string(secret.Data[key]); got != want {
			t.Fatalf("Secret %s key %s=%q, want %q", name, key, got, want)
		}
	}
	assertSecretKey("encryption", "encryption-key", "legacy-encryption")
	assertSecretKey("provisioner", "token", "legacy-provisioner")
	assertSecretKey("provisioner", "provisioner-token", "legacy-provisioner")

	for _, name := range []string{"worker", "manager"} {
		secret, err := client.CoreV1().Secrets("target").Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(secret.Data["token"]) != 64 {
			t.Fatalf("Secret %s generated token length=%d", name, len(secret.Data["token"]))
		}
	}
}

func TestRunHelmMigrateValuesDoesNotOverwriteExistingSecretKeys(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "target"}, Data: map[string][]byte{"token": []byte("keep-worker")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "manager", Namespace: "target"}, Data: map[string][]byte{"token": []byte("keep-manager")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "encryption", Namespace: "target"}, Data: map[string][]byte{"encryption-key": []byte("keep-encryption")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "provisioner", Namespace: "target"}, Data: map[string][]byte{"provisioner-token": []byte("keep-provisioner")}},
	)
	o := &helmMigrateValuesOptions{
		input: "-", output: "-", namespace: "target", release: "backend",
		workerControlSecret: "worker", managerInternalSecret: "manager",
		encryptionSecret: "encryption", provisionerSecret: "provisioner",
		migrateSecrets: true, kubeClient: client,
	}
	if err := runHelmMigrateValues(strings.NewReader("kubernetesSession: {enabled: true}\n"), &bytes.Buffer{}, &bytes.Buffer{}, o); err != nil {
		t.Fatal(err)
	}

	for name, expectation := range map[string][2]string{
		"worker": {"token", "keep-worker"}, "manager": {"token", "keep-manager"},
		"encryption": {"encryption-key", "keep-encryption"}, "provisioner": {"provisioner-token", "keep-provisioner"},
	} {
		secret, err := client.CoreV1().Secrets("target").Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := string(secret.Data[expectation[0]]); got != expectation[1] {
			t.Fatalf("Secret %s was overwritten: %q", name, got)
		}
	}
}

func TestRunHelmMigrateValuesRejectsConflictingMigratedSecrets(t *testing.T) {
	for _, test := range []struct {
		name      string
		input     string
		secrets   []corev1.Secret
		wantError string
	}{
		{
			name:      "encryption",
			input:     "config: {encryption: {key: legacy}}\nkubernetesSession: {enabled: true}\n",
			secrets:   []corev1.Secret{{ObjectMeta: metav1.ObjectMeta{Name: "encryption", Namespace: "target"}, Data: map[string][]byte{"encryption-key": []byte("current")}}},
			wantError: "conflicts with legacy config.encryption.key",
		},
		{
			name:      "provisioner",
			input:     "kubernetesSession: {enabled: true}\n",
			secrets:   []corev1.Secret{{ObjectMeta: metav1.ObjectMeta{Name: "provisioner", Namespace: "target"}, Data: map[string][]byte{"token": []byte("legacy"), "provisioner-token": []byte("current")}}},
			wantError: `keys "token" and "provisioner-token" conflict`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			objects := make([]runtime.Object, len(test.secrets))
			for i := range test.secrets {
				objects[i] = &test.secrets[i]
			}
			client := fake.NewSimpleClientset(objects...)
			o := &helmMigrateValuesOptions{
				input: "-", output: "-", namespace: "target", release: "backend",
				workerControlSecret: "worker", managerInternalSecret: "manager",
				encryptionSecret: "encryption", provisionerSecret: "provisioner",
				migrateSecrets: true, kubeClient: client,
			}
			err := runHelmMigrateValues(strings.NewReader(test.input), &bytes.Buffer{}, &bytes.Buffer{}, o)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error=%v, want containing %q", err, test.wantError)
			}
			if _, getErr := client.CoreV1().Secrets("target").Get(context.Background(), "worker", metav1.GetOptions{}); !apierrors.IsNotFound(getErr) {
				t.Fatalf("worker Secret was created before conflict validation: %v", getErr)
			}
		})
	}
}
