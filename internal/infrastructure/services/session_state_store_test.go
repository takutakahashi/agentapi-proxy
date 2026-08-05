package services

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestVolumeSessionStateStore(t *testing.T) {
	store, err := newVolumeSessionStateStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	want := []byte("snapshot")
	if err := store.Save(ctx, "session-1", bytes.NewReader(want)); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = got.Close() }()
	gotBytes, err := io.ReadAll(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotBytes) != string(want) {
		t.Fatalf("got %q", gotBytes)
	}
	if err := store.Save(ctx, "../escape", bytes.NewReader(want)); err == nil {
		t.Fatal("expected invalid id error")
	}
}
