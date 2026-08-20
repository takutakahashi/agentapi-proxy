package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"
)

const compatibleChart = `apiVersion: v2
name: ccplant
dependencies:
  - name: agentapi-proxy
    alias: backend
    version: 0.3.0
    condition: backend.enabled
  - name: agentapi-ui
    alias: frontend
    version: 0.1.0
    condition: frontend.enabled
`

func TestControlPlaneCommandIsTopLevelNotHelmSubcommand(t *testing.T) {
	if ControlPlaneCmd.Use != "control-plane" {
		t.Fatalf("ControlPlaneCmd.Use = %q", ControlPlaneCmd.Use)
	}
	for _, command := range HelmCmd.Commands() {
		if command.Name() == "control-plane" {
			t.Fatal("control-plane must not be registered below the helm command")
		}
	}
}

func TestControlPlaneCommandIncludesGenerate(t *testing.T) {
	command := newControlPlaneCommand()
	generate, _, err := command.Find([]string{"generate"})
	if err != nil {
		t.Fatal(err)
	}
	if generate.Name() != "generate" {
		t.Fatalf("command = %q", generate.Name())
	}
}

type helmValuesRunner struct {
	values string
	args   []string
}

func (r *helmValuesRunner) Run(_ context.Context, stdout, _ io.Writer, name string, args ...string) error {
	r.args = append([]string{name}, args...)
	_, err := io.WriteString(stdout, r.values)
	return err
}

func TestGenerateKubernetesInstallConfigFromHelmValuesAndSecret(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "custom-cookie", Namespace: "production"},
		Data:       map[string][]byte{"key": []byte("existing-secret")},
	})
	runner := &helmValuesRunner{values: `
global:
  hostname: app.example.com
  apiHostname: api.example.com
  ingress:
    className: nginx
    tls:
      enabled: true
backend:
  image:
    tag: v1.2.3
  kubernetesSession:
    pvc:
      enabled: true
      storageClass: fast
      storageSize: 25Gi
frontend:
  cookieEncryptionSecret:
    secretName: custom-cookie
    secretKey: key
`}
	cfg, err := generateKubernetesInstallConfig(context.Background(), client, runner, "production", "plant", "oci://example/chart")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(runner.args, " "); got != "helm get values plant --namespace production --all --output yaml" {
		t.Fatalf("helm command = %q", got)
	}
	if cfg.Metadata.Name != "plant" || cfg.Spec.Version != "1.2.3" || cfg.Spec.Frontend.Hostname != "app.example.com" || cfg.Spec.Backend.Hostname != "api.example.com" {
		t.Fatalf("unexpected generated config: %#v", cfg)
	}
	if !cfg.Spec.TLS || !cfg.Spec.Persistence.Enabled || cfg.Spec.Persistence.Size != "25Gi" {
		t.Fatalf("unexpected generated spec: %#v", cfg.Spec)
	}
	if cfg.Spec.Secrets.CookieEncryptionKey != "existing-secret" {
		t.Fatalf("cookie key = %q", cfg.Spec.Secrets.CookieEncryptionKey)
	}
	if target := cfg.Target.Kubernetes; target.Namespace != "production" || target.IngressClass != "nginx" || target.StorageClass != "fast" || target.Chart != "oci://example/chart" {
		t.Fatalf("unexpected target: %#v", target)
	}
}

func TestGenerateKubernetesInstallConfigRequiresCookieSecret(t *testing.T) {
	runner := &helmValuesRunner{values: `global: {hostname: app.example.com, apiHostname: api.example.com}`}
	_, err := generateKubernetesInstallConfig(context.Background(), kubernetesfake.NewSimpleClientset(), runner, "ccplant", "ccplant", defaultCCPlantChart)
	if err == nil || !strings.Contains(err.Error(), "read cookie Secret") {
		t.Fatalf("error = %v", err)
	}
}

func TestDefaultInstallConfigSeparatesPortableSpecFromTarget(t *testing.T) {
	cfg, err := defaultInstallConfig("kubernetes")
	if err != nil {
		t.Fatalf("defaultInstallConfig: %v", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(data)
	for _, want := range []string{"apiVersion: ccplant.io/v1alpha1", "kind: ControlPlane", "version: latest", "spec:", "target:", "type: kubernetes", "kubernetes:"} {
		if !strings.Contains(text, want) {
			t.Errorf("config missing %q:\n%s", want, text)
		}
	}
}

func TestControlPlaneVersionMapsToEveryTarget(t *testing.T) {
	cfg, err := defaultInstallConfig("compose")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Spec.Version = "0.4.2"
	manifest, err := generateComposeManifest(cfg, cfg.Target.Compose)
	if err != nil {
		t.Fatal(err)
	}
	for _, image := range []string{"ccplant-backend:v0.4.2", "ccplant-frontend:v0.4.2"} {
		if !strings.Contains(string(manifest), image) {
			t.Errorf("manifest missing %q", image)
		}
	}
	if got := chartVersion(cfg.Spec.Version, ""); got != "0.4.2" {
		t.Fatalf("chartVersion = %q", got)
	}
	if got := chartVersion(cfg.Spec.Version, "v0.4.1"); got != "0.4.1" {
		t.Fatalf("override chartVersion = %q", got)
	}
}

func TestInstallConfigSupportsComposeTarget(t *testing.T) {
	cfg, err := defaultInstallConfig("compose")
	if err != nil {
		t.Fatalf("defaultInstallConfig: %v", err)
	}
	manifest, err := generateComposeManifest(cfg, cfg.Target.Compose)
	if err != nil {
		t.Fatalf("generateComposeManifest: %v", err)
	}
	text := string(manifest)
	for _, want := range []string{"services:", "backend:", "frontend:", "3000:3000", "8080:8080", "volumes:"} {
		if !strings.Contains(text, want) {
			t.Errorf("manifest missing %q:\n%s", want, text)
		}
	}
}

func TestValidateInstallConfigRejectsUnknownTarget(t *testing.T) {
	cfg, err := defaultInstallConfig("compose")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Target.Type = "unknown"
	if err := validateInstallConfig(cfg); err == nil {
		t.Fatal("expected unknown target error")
	}
}

func TestWriteNewFileAtomicallyPreservesInput(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "control-plane.yaml")
	output := filepath.Join(directory, "control-plane.sops.yaml")
	if err := os.WriteFile(input, []byte("plain"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeNewFileAtomically(output, []byte("encrypted"), 0o600); err != nil {
		t.Fatal(err)
	}
	plain, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "plain" {
		t.Fatalf("input content = %q", plain)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "encrypted" {
		t.Fatalf("content = %q", data)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestEncryptedControlPlanePath(t *testing.T) {
	for input, want := range map[string]string{"control-plane.yaml": "control-plane.sops.yaml", "config.yml": "config.sops.yml", "config": "config.sops.yaml"} {
		if got := encryptedControlPlanePath(input); got != want {
			t.Errorf("encryptedControlPlanePath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestHasSOPSMetadata(t *testing.T) {
	if hasSOPSMetadata([]byte("kind: ControlPlane\n")) {
		t.Fatal("plain file detected as encrypted")
	}
	if !hasSOPSMetadata([]byte("kind: ControlPlane\nsops:\n  version: 3.9.0\n")) {
		t.Fatal("SOPS metadata was not detected")
	}
}

type recordedInstallCommand struct {
	name string
	args []string
}
type fakeInstallRunner struct {
	commands []recordedInstallCommand
	fail     map[string]error
}

func (f *fakeInstallRunner) Run(_ context.Context, stdout, _ io.Writer, name string, args ...string) error {
	key := strings.Join(append([]string{name}, args...), " ")
	f.commands = append(f.commands, recordedInstallCommand{name: name, args: append([]string(nil), args...)})
	if err := f.fail[key]; err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "version" {
		_, _ = io.WriteString(stdout, "v3.16.2")
	}
	if len(args) > 1 && args[0] == "show" {
		_, _ = io.WriteString(stdout, compatibleChart)
	}
	return nil
}

func installTestClient(objects ...runtime.Object) kubernetes.Interface {
	objects = append(objects,
		&networkingv1.IngressClass{ObjectMeta: metav1.ObjectMeta{Name: "nginx", Annotations: map[string]string{"ingressclass.kubernetes.io/is-default-class": "true"}}},
		&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "standard", Annotations: map[string]string{"storageclass.kubernetes.io/is-default-class": "true"}}},
	)
	client := kubernetesfake.NewSimpleClientset(objects...)
	client.Discovery().(*fake.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.30.0"}
	return client
}

func defaultInstallOptions() *helmInstallOptions {
	return &helmInstallOptions{release: "ccplant", namespace: "ccplant", chart: "chart", version: "1.2.3", hostname: "app.example.com", apiHostname: "api.example.com", ingressClass: "nginx", cookieSecretName: defaultCookieSecretName, cookieSecretKey: defaultCookieSecretKey, persistenceSize: "10Gi", timeout: time.Minute, createNamespace: true, persistence: true}
}

func TestRunHelmInstallCreatesNamespaceSecretAndInstalls(t *testing.T) {
	client := installTestClient()
	runner := &fakeInstallRunner{}
	var output bytes.Buffer
	if err := runHelmInstall(context.Background(), &output, io.Discard, client, runner, defaultInstallOptions()); err != nil {
		t.Fatalf("runHelmInstall: %v", err)
	}
	secret, err := client.CoreV1().Secrets("ccplant").Get(context.Background(), defaultCookieSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("cookie secret: %v", err)
	}
	if len(secret.StringData[defaultCookieSecretKey]) != 64 {
		t.Fatalf("secret length = %d", len(secret.StringData[defaultCookieSecretKey]))
	}
	last := runner.commands[len(runner.commands)-1]
	joined := strings.Join(last.args, " ")
	for _, want := range []string{"install ccplant chart", "--create-namespace", "--values"} {
		if !strings.Contains(joined, want) {
			t.Errorf("helm args %q missing %q", joined, want)
		}
	}
}

func TestGenerateInstallValuesBuildsCompleteBackendFrontendConfiguration(t *testing.T) {
	o := defaultInstallOptions()
	o.ingressClass = "nginx"
	o.storageClass = "fast"
	o.tls = true
	data, err := generateInstallValues(o)
	if err != nil {
		t.Fatalf("generateInstallValues: %v", err)
	}
	text := string(data)
	for _, want := range []string{"backend:", "frontend:", "enabled: true", "storageClass: fast", "storageSize: 10Gi", "publicUrl: https://app.example.com", "ccplant-backend-tls", "ccplant-frontend-tls"} {
		if !strings.Contains(text, want) {
			t.Errorf("generated values missing %q:\n%s", want, text)
		}
	}
}

func TestResolveClusterDefaultsRequiresStorageForPersistence(t *testing.T) {
	client := kubernetesfake.NewSimpleClientset(&networkingv1.IngressClass{ObjectMeta: metav1.ObjectMeta{Name: "nginx"}})
	o := defaultInstallOptions()
	o.persistence = true
	err := resolveClusterDefaults(context.Background(), client, o)
	if err == nil || !strings.Contains(err.Error(), "StorageClass") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunHelmInstallUsesUpgradeAndPreservesSecret(t *testing.T) {
	existing := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: defaultCookieSecretName, Namespace: "ccplant"}, Data: map[string][]byte{defaultCookieSecretKey: []byte("existing")}}
	release := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "sh.helm.release.v1.ccplant.v1", Namespace: "ccplant", Labels: map[string]string{"owner": "helm", "name": "ccplant"}}}
	client := installTestClient(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ccplant"}}, existing, release)
	runner := &fakeInstallRunner{}
	if err := runHelmInstall(context.Background(), io.Discard, io.Discard, client, runner, defaultInstallOptions()); err != nil {
		t.Fatalf("runHelmInstall: %v", err)
	}
	joined := strings.Join(runner.commands[len(runner.commands)-1].args, " ")
	if !strings.HasPrefix(joined, "upgrade ccplant chart") {
		t.Fatalf("helm args = %q", joined)
	}
	if strings.Contains(joined, "--create-namespace") {
		t.Fatalf("upgrade unexpectedly creates namespace: %q", joined)
	}
}

func TestRunHelmInstallRejectsIncompatibleChartBeforeMutation(t *testing.T) {
	client := installTestClient()
	runner := &fakeInstallRunner{}
	runner.fail = map[string]error{"helm show chart chart --version 1.2.3": errors.New("not found")}
	err := runHelmInstall(context.Background(), io.Discard, io.Discard, client, runner, defaultInstallOptions())
	if err == nil || !strings.Contains(err.Error(), "load chart metadata") {
		t.Fatalf("error = %v", err)
	}
	if _, err := client.CoreV1().Namespaces().Get(context.Background(), "ccplant", metav1.GetOptions{}); err == nil {
		t.Fatal("namespace mutated before preflight completed")
	}
}

func TestValidateUmbrellaChartRequiresBothComponents(t *testing.T) {
	metadata := &umbrellaChartMetadata{APIVersion: "v2", Name: "ccplant"}
	if err := validateUmbrellaChart(metadata); err == nil {
		t.Fatal("expected missing dependency error")
	}
}

func TestExistingCookieSecretMustContainConfiguredKey(t *testing.T) {
	client := installTestClient(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ccplant"}}, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: defaultCookieSecretName, Namespace: "ccplant"}})
	err := ensureCookieSecret(context.Background(), client, "ccplant", defaultCookieSecretName, defaultCookieSecretKey, "")
	if err == nil || !strings.Contains(err.Error(), "does not contain") {
		t.Fatalf("error = %v", err)
	}
}
