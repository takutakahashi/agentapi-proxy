package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesystemAssetStoreSaveHTML(t *testing.T) {
	root := t.TempDir()
	store := NewFilesystemAssetStore(root, "https://assets.example.com")

	asset, err := store.SaveHTML(context.Background(), "user-1", "<h1>Hello</h1>")
	if err != nil {
		t.Fatalf("SaveHTML returned error: %v", err)
	}
	if asset.ID == "" {
		t.Fatal("asset ID is empty")
	}
	if !strings.HasPrefix(asset.URL, "https://assets.example.com/assets/") {
		t.Fatalf("unexpected URL: %s", asset.URL)
	}
	if !strings.HasSuffix(asset.URL, "/index.html") {
		t.Fatalf("unexpected URL suffix: %s", asset.URL)
	}

	path := filepath.Join(root, "assets", asset.ID, "index.html")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved asset: %v", err)
	}
	if string(data) != "<h1>Hello</h1>" {
		t.Fatalf("unexpected saved HTML: %q", string(data))
	}
}

func TestJoinURL(t *testing.T) {
	got := joinURL("https://example.com/base/", "assets/id/index.html")
	want := "https://example.com/base/assets/id/index.html"
	if got != want {
		t.Fatalf("joinURL() = %q, want %q", got, want)
	}
}
