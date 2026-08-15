package cmd

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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
