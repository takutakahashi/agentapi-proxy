package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBuildMigrationShadowValuesAvoidsSharedResourceCollisions(t *testing.T) {
	values := buildMigrationShadowValues(
		map[string]any{"fullnameOverride": "agentapi-proxy", "ingress": map[string]any{"enabled": true}, "config": map[string]any{"hostname": "api.example.com"}},
		map[string]any{"fullnameOverride": "agentapi-ui", "ingress": map[string]any{"enabled": true}},
	)
	backend := values["backend"].(map[string]any)
	frontend := values["frontend"].(map[string]any)
	if _, ok := backend["fullnameOverride"]; ok {
		t.Fatal("backend fullnameOverride was retained")
	}
	if _, ok := frontend["fullnameOverride"]; ok {
		t.Fatal("frontend fullnameOverride was retained")
	}
	assertNestedValue(t, backend, false, "ingress", "enabled")
	assertNestedValue(t, backend, false, "controlPlaneService", "create")
	assertNestedValue(t, backend, "agentapi-proxy-session", "kubernetesSession", "serviceAccountName")
	assertNestedValue(t, backend, false, "kubernetesSession", "rbac", "create")
	assertNestedValue(t, frontend, false, "ingress", "enabled")
	assertNestedValue(t, backend, "api.example.com", "config", "hostname")
}

func TestRunHelmMigratePlanReadyAndReadOnly(t *testing.T) {
	namespace := "test"
	client := fake.NewSimpleClientset(
		helmReleaseSecret(t, "agentapi-proxy", 3, "deployed", map[string]any{"github": map[string]any{"tokenRef": map[string]any{"name": "github", "key": "token"}}}),
		helmReleaseSecret(t, "agentapi-ui", 2, "deployed", map[string]any{}),
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "control", Namespace: namespace, Annotations: map[string]string{"helm.sh/resource-policy": "keep"}}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app.kubernetes.io/instance": "agentapi-proxy"}}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-proxy-session", Namespace: namespace}},
		&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-proxy-session", Namespace: namespace}},
		&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-proxy-session", Namespace: namespace}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "github", Namespace: namespace}, Data: map[string][]byte{"token": []byte("do-not-print")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-settings-alice", Namespace: namespace, UID: "settings-uid"}, Data: map[string][]byte{"settings.json": []byte("{}")}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-new", Namespace: namespace, Labels: map[string]string{"app.kubernetes.io/name": "agentapi-session"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "agent", Env: []corev1.EnvVar{{Name: "PROXY_URL", Value: "http://control.test.svc.cluster.local:8080"}}}}}},
	)
	valuesFile := filepath.Join(t.TempDir(), "shadow.yaml")
	o := &helmMigratePlanOptions{namespace: namespace, backendRelease: "agentapi-proxy", frontendRelease: "agentapi-ui", targetRelease: "ccplant", chart: "oci://ghcr.io/ccplant/charts/ccplant", version: "0.3.2", valuesOut: valuesFile, output: "text"}
	var out bytes.Buffer
	if err := runHelmMigratePlan(context.Background(), &out, client, o); err != nil {
		t.Fatalf("runHelmMigratePlan() error = %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Migration preflight: READY") {
		t.Fatalf("output does not report READY:\n%s", out.String())
	}
	if strings.Contains(out.String(), "do-not-print") {
		t.Fatal("output exposed a Secret value")
	}
	data, err := os.ReadFile(valuesFile)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := yaml.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	backend := values["backend"].(map[string]any)
	assertNestedValue(t, backend, false, "controlPlaneService", "create")
	if _, err := client.CoreV1().Services(namespace).Get(context.Background(), "control", metav1.GetOptions{}); err != nil {
		t.Fatalf("plan mutated Service/control: %v", err)
	}
}

func TestRunHelmMigratePlanBlocksMissingControlAndOwnedRuntimeSecret(t *testing.T) {
	namespace := "test"
	client := fake.NewSimpleClientset(
		helmReleaseSecret(t, "agentapi-proxy", 1, "deployed", map[string]any{}), helmReleaseSecret(t, "agentapi-ui", 1, "deployed", map[string]any{}),
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-proxy-session", Namespace: namespace}}, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-proxy-session", Namespace: namespace}}, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-proxy-session", Namespace: namespace}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-settings-alice", Namespace: namespace, Annotations: map[string]string{"meta.helm.sh/release-name": "agentapi-proxy"}}},
	)
	o := &helmMigratePlanOptions{namespace: namespace, backendRelease: "agentapi-proxy", frontendRelease: "agentapi-ui", targetRelease: "ccplant", chart: "chart", version: "0.3.2", valuesOut: filepath.Join(t.TempDir(), "values.yaml"), output: "text"}
	var out bytes.Buffer
	err := runHelmMigratePlan(context.Background(), &out, client, o)
	if err == nil {
		t.Fatal("runHelmMigratePlan() error = nil, want blocker")
	}
	for _, want := range []string{"Migration preflight: BLOCKED", "control Service: missing", "Secret/agentapi-settings-alice"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestValidateHelmMigratePlanOptionsRequiresExactVersion(t *testing.T) {
	base := helmMigratePlanOptions{namespace: "test", backendRelease: "backend", frontendRelease: "frontend", targetRelease: "target", output: "text"}
	for _, version := range []string{"", "latest", "0.3", "main"} {
		o := base
		o.version = version
		if err := validateHelmMigratePlanOptions(&o); err == nil {
			t.Errorf("version %q was accepted", version)
		}
	}
	base.version = "v0.3.2"
	if err := validateHelmMigratePlanOptions(&base); err != nil {
		t.Fatalf("valid version rejected: %v", err)
	}
}

func TestRunHelmMigrateVerifyChecksShadowAndSharedLeaderLease(t *testing.T) {
	namespace := "test"
	holder := "agentapi-proxy-old-pod"
	client := fake.NewSimpleClientset(
		helmReleaseSecret(t, "ccplant", 1, "deployed", map[string]any{"backend": map[string]any{"scheduleWorker": map[string]any{"enabled": true}}}),
		readyDeployment("ccplant-backend", namespace, 1), readyDeployment("ccplant-frontend", namespace, 1),
		readyEndpoints("ccplant-backend", namespace), readyEndpoints("ccplant-frontend", namespace),
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "control", Namespace: namespace}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app.kubernetes.io/name": "agentapi-proxy", "app.kubernetes.io/instance": "agentapi-proxy", "app.kubernetes.io/component": "proxy"}}},
		&coordinationv1.Lease{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-schedule-worker", Namespace: namespace}, Spec: coordinationv1.LeaseSpec{HolderIdentity: &holder}},
		&coordinationv1.Lease{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-allocator", Namespace: namespace}, Spec: coordinationv1.LeaseSpec{HolderIdentity: &holder}},
	)
	o := &helmMigratePlanOptions{namespace: namespace, backendRelease: "agentapi-proxy", targetRelease: "ccplant", output: "text"}
	probes := 0
	probe := func(_ context.Context, service, path string) error {
		probes++
		if service != "ccplant-backend" || path != "/health" {
			t.Fatalf("unexpected probe %s%s", service, path)
		}
		return nil
	}
	var out bytes.Buffer
	if err := runHelmMigrateVerify(context.Background(), &out, client, o, probe); err != nil {
		t.Fatalf("runHelmMigrateVerify() error = %v\n%s", err, out.String())
	}
	if probes != 1 {
		t.Fatalf("probe calls = %d, want 1", probes)
	}
	for _, want := range []string{"Shadow verification: PASS", "Phase: shadow", "agentapi-schedule-worker", "agentapi-session-allocator"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestRunHelmMigrateVerifyDetectsCutoverAndProbeFailure(t *testing.T) {
	namespace := "test"
	holder := "new-pod"
	client := fake.NewSimpleClientset(
		helmReleaseSecret(t, "ccplant", 1, "deployed", map[string]any{"backend": map[string]any{}}),
		readyDeployment("ccplant-backend", namespace, 1), readyDeployment("ccplant-frontend", namespace, 1),
		readyEndpoints("ccplant-backend", namespace), readyEndpoints("ccplant-frontend", namespace),
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "control", Namespace: namespace}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app.kubernetes.io/name": "backend", "app.kubernetes.io/instance": "ccplant", "app.kubernetes.io/component": "proxy"}}},
		&coordinationv1.Lease{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-allocator", Namespace: namespace}, Spec: coordinationv1.LeaseSpec{HolderIdentity: &holder}},
	)
	o := &helmMigratePlanOptions{namespace: namespace, backendRelease: "agentapi-proxy", targetRelease: "ccplant", output: "text"}
	var out bytes.Buffer
	err := runHelmMigrateVerify(context.Background(), &out, client, o, func(context.Context, string, string) error { return errors.New("HTTP 503") })
	if err == nil {
		t.Fatal("runHelmMigrateVerify() error = nil, want probe failure")
	}
	for _, want := range []string{"Shadow verification: FAIL", "Phase: cutover", "HTTP 503"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestMigrationCommandsUseUmbrellaBackendSelector(t *testing.T) {
	o := &helmMigratePlanOptions{namespace: "test", backendRelease: "agentapi-proxy", frontendRelease: "agentapi-ui", targetRelease: "ccplant", chart: "chart", version: "0.3.2", valuesOut: "values.yaml"}
	joined := strings.Join(migrationCommands(o, 0), "\n")
	if !strings.Contains(joined, `app.kubernetes.io/name":"backend`) {
		t.Fatalf("commands do not use umbrella backend selector:\n%s", joined)
	}
}

func readyDeployment(name, namespace string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}, Spec: appsv1.DeploymentSpec{Replicas: &replicas}, Status: appsv1.DeploymentStatus{ReadyReplicas: replicas}}
}

func readyEndpoints(name, namespace string) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Name: name + "-test", Namespace: namespace, Labels: map[string]string{"kubernetes.io/service-name": name}}, AddressType: discoveryv1.AddressTypeIPv4, Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}}}}
}

func assertNestedValue(t *testing.T, root map[string]any, want any, path ...string) {
	t.Helper()
	current := root
	for _, key := range path[:len(path)-1] {
		next, ok := current[key].(map[string]any)
		if !ok {
			t.Fatalf("%s is not a map", strings.Join(path, "."))
		}
		current = next
	}
	if got := current[path[len(path)-1]]; got != want {
		t.Fatalf("%s = %#v, want %#v", strings.Join(path, "."), got, want)
	}
}
