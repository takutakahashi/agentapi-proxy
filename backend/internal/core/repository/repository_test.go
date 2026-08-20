package repository

import "testing"

func TestExtractInfoIncludesCheckoutTags(t *testing.T) {
	info, err := ExtractInfo(map[string]string{
		"repository": "https://github.com/owner/repo.git",
		"branch":     " feature/test ",
		"pr":         " 123 ",
	}, "session-id")
	if err != nil {
		t.Fatalf("ExtractInfo returned error: %v", err)
	}
	if info == nil {
		t.Fatal("expected repository info")
	}
	if info.FullName != "owner/repo" {
		t.Fatalf("FullName = %q, want owner/repo", info.FullName)
	}
	if info.CloneDir != "session-id" {
		t.Fatalf("CloneDir = %q, want session-id", info.CloneDir)
	}
	if info.Branch != "feature/test" {
		t.Fatalf("Branch = %q, want feature/test", info.Branch)
	}
	if info.PR != "123" {
		t.Fatalf("PR = %q, want 123", info.PR)
	}
}

func TestExtractInfoSupportsPRNumberAliases(t *testing.T) {
	info, err := ExtractInfo(map[string]string{
		"repository": "owner/repo",
		"pr_number":  "456",
	}, "session-id")
	if err != nil {
		t.Fatalf("ExtractInfo returned error: %v", err)
	}
	if info.PR != "456" {
		t.Fatalf("PR = %q, want 456", info.PR)
	}
}
