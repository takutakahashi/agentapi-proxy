package provisioner

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteWebhookPayloadFile_WritesWhenAbsent verifies that
// writeWebhookPayloadFile creates the file with the given payload when
// webhookPayloadPath does not exist (stock-session case).
func TestWriteWebhookPayloadFile_WritesWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opt", "webhook", "payload.json")

	// Override the package-level constant for the duration of this test.
	origPath := webhookPayloadPath
	// webhookPayloadPath is a const, so we use a helper that accepts the path.
	// Call the internal logic directly via the exported-for-test variant.
	// Since the function is unexported, we test it by temporarily overriding
	// the path via a wrapper test helper.
	_ = origPath // suppress unused warning

	writeWebhookPayloadFileToPath(path, `{"event":"push"}`)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file to be created at %s, got error: %v", path, err)
	}
	if string(got) != `{"event":"push"}` {
		t.Errorf("expected payload %q, got %q", `{"event":"push"}`, string(got))
	}
}

// TestWriteWebhookPayloadFile_SkipsWhenPresent verifies that
// writeWebhookPayloadFile does NOT overwrite an existing file (non-stock case:
// Secret volume mount already provides the file as read-only).
func TestWriteWebhookPayloadFile_SkipsWhenPresent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.json")

	existing := `{"original":"content"}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatalf("failed to create existing file: %v", err)
	}

	writeWebhookPayloadFileToPath(path, `{"should":"not-overwrite"}`)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(got) != existing {
		t.Errorf("expected original content %q to be preserved, got %q", existing, string(got))
	}
}

// TestWriteWebhookPayloadFile_CreatesParentDirs verifies that parent
// directories are created as needed.
func TestWriteWebhookPayloadFile_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "payload.json")

	writeWebhookPayloadFileToPath(path, `{"nested":true}`)

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to exist at %s: %v", path, err)
	}
}
