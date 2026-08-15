package cmd

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDiagnoseHelmReleaseChecksReferencedSecretData(t *testing.T) {
	replicas := int32(1)
	values := map[string]any{
		"config": map[string]any{
			"oauth": map[string]any{
				"clientSecretRef": map[string]any{"name": "auth", "key": "client-secret"},
			},
		},
		"envFrom": []any{map[string]any{
			"secretRef": map[string]any{"name": "runtime-env"},
		}},
		"ingress": map[string]any{
			"tls": []any{map[string]any{"secretName": "tls-cert"}},
		},
	}
	client := fake.NewSimpleClientset(
		helmReleaseSecret(t, "example", 1, "deployed", values),
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "test"},
			Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: replicas},
		},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "auth", Namespace: "test"}, Data: map[string][]byte{"client-secret": []byte("present")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "runtime-env", Namespace: "test"}, Data: map[string][]byte{"TOKEN": []byte("present")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "tls-cert", Namespace: "test"}, Data: map[string][]byte{"tls.crt": []byte("present")}},
	)

	findings, err := diagnoseHelmRelease(context.Background(), client, "test", "example")
	if err != nil {
		t.Fatalf("diagnoseHelmRelease() error = %v", err)
	}
	for _, finding := range findings {
		if !finding.OK && !finding.Warning {
			t.Errorf("unexpected failed finding: %+v", finding)
		}
	}
}

func TestInspectSensitiveLiteralsWarnsWithoutExposingValues(t *testing.T) {
	values := map[string]any{
		"github": map[string]any{"token": "github-secret-value"},
		"env": []any{
			map[string]any{"name": "ADMIN_TOKEN", "value": "admin-secret-value"},
			map[string]any{"name": "PUBLIC_URL", "value": "https://example.com"},
		},
	}

	findings := inspectSensitiveLiterals(values)
	if len(findings) != 2 {
		t.Fatalf("len(findings) = %d, want 2", len(findings))
	}
	joined := findingMessages(findings)
	for _, finding := range findings {
		if !finding.Warning {
			t.Errorf("finding.Warning = false, want true: %+v", finding)
		}
	}
	for _, secret := range []string{"github-secret-value", "admin-secret-value"} {
		if strings.Contains(joined, secret) {
			t.Errorf("findings exposed secret value %q", secret)
		}
	}
}

func TestDiagnoseHelmReleaseReportsMissingAndEmptySecretValues(t *testing.T) {
	values := map[string]any{
		"github": map[string]any{
			"tokenRef": map[string]any{"name": "auth", "key": "github-token"},
		},
		"vapid": map[string]any{
			"privateKeyRef": map[string]any{"name": "auth", "key": "vapid-private-key"},
		},
		"externalRedis": map[string]any{
			"existingSecret":            "missing",
			"existingSecretPasswordKey": "redis-password",
		},
	}
	client := fake.NewSimpleClientset(
		helmReleaseSecret(t, "example", 2, "deployed", values),
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "auth", Namespace: "test"}, Data: map[string][]byte{
			"github-token":     []byte(""),
			"unrelated-secret": []byte("present"),
		}},
	)

	findings, err := diagnoseHelmRelease(context.Background(), client, "test", "example")
	if err != nil {
		t.Fatalf("diagnoseHelmRelease() error = %v", err)
	}
	joined := findingMessages(findings)
	for _, expected := range []string{
		`Secret "auth" key "github-token" is empty`,
		`Secret "auth" is missing key "vapid-private-key"`,
		`Secret "missing" does not exist`,
	} {
		if !strings.Contains(joined, expected) {
			t.Errorf("findings do not contain %q:\n%s", expected, joined)
		}
	}
	for _, finding := range findings {
		if strings.Contains(finding.Subject, "tokenRef") || strings.Contains(finding.Subject, "privateKeyRef") || strings.Contains(finding.Subject, "existingSecret") {
			if !finding.Warning {
				t.Errorf("optional Secret finding should be a warning: %+v", finding)
			}
		}
	}
}

func TestDiagnoseHelmReleaseFailsForEnabledCookieEncryptionSecret(t *testing.T) {
	values := map[string]any{
		"cookieEncryptionSecret": map[string]any{
			"enabled":    true,
			"secretName": "cookie-secret",
			"secretKey":  "cookie-encryption-secret",
		},
	}
	client := fake.NewSimpleClientset(helmReleaseSecret(t, "agentapi-ui", 1, "deployed", values))

	findings, err := diagnoseHelmRelease(context.Background(), client, "test", "agentapi-ui")
	if err != nil {
		t.Fatalf("diagnoseHelmRelease() error = %v", err)
	}
	foundRequiredFailure := false
	for _, finding := range findings {
		if strings.Contains(finding.Subject, "cookieEncryptionSecret") && !finding.OK && !finding.Warning {
			foundRequiredFailure = true
		}
	}
	if !foundRequiredFailure {
		t.Fatalf("required cookie encryption Secret failure was not reported: %+v", findings)
	}
}

func TestLoadLatestHelmReleaseUsesHighestRevision(t *testing.T) {
	client := fake.NewSimpleClientset(
		helmReleaseSecret(t, "example", 9, "superseded", map[string]any{}),
		helmReleaseSecret(t, "example", 10, "deployed", map[string]any{}),
	)

	release, secretName, err := loadLatestHelmRelease(context.Background(), client, "test", "example")
	if err != nil {
		t.Fatalf("loadLatestHelmRelease() error = %v", err)
	}
	if release.Version != 10 {
		t.Fatalf("release.Version = %d, want 10", release.Version)
	}
	if !strings.HasSuffix(secretName, ".v10") {
		t.Fatalf("secretName = %q, want revision 10", secretName)
	}
}

func helmReleaseSecret(t *testing.T, name string, revision int, status string, values map[string]any) *corev1.Secret {
	t.Helper()
	release := helmStoredRelease{Name: name, Version: revision, Info: helmReleaseInfo{Status: status}, Config: values}
	payload, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	encoded := make([]byte, base64.StdEncoding.EncodedLen(compressed.Len()))
	base64.StdEncoding.Encode(encoded, compressed.Bytes())
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sh.helm.release.v1." + name + ".v" + strconv.Itoa(revision),
			Namespace: "test",
			Labels: map[string]string{
				"owner":   "helm",
				"name":    name,
				"version": strconv.Itoa(revision),
				"status":  status,
			},
		},
		Data: map[string][]byte{"release": encoded},
	}
}

func findingMessages(findings []doctorFinding) string {
	var builder strings.Builder
	for _, finding := range findings {
		builder.WriteString(finding.Message)
		builder.WriteByte('\n')
	}
	return builder.String()
}
