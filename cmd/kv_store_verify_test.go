package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

func TestVerifyKVStoresMatch(t *testing.T) {
	ctx := context.Background()
	primary, secondary := newMemoryKVStore(), newMemoryKVStore()
	value := []byte(`{"metadata":{"name":"task","namespace":"test","labels":{"agentapi.proxy/type":"task"}}}`)
	record := kvstore.Record{Kind: kvstore.KindConfigMap, Namespace: "test", Key: "task", Value: value}
	_, _ = primary.Create(ctx, record)
	_, _ = secondary.Create(ctx, record)
	result, err := verifyKVStores(ctx, primary, secondary, "test")
	if err != nil || result.Matched != 1 || result.mismatchCount() != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestVerifyKVStoresReportsAllMismatchTypes(t *testing.T) {
	ctx := context.Background()
	primary, secondary := newMemoryKVStore(), newMemoryKVStore()
	configMap := func(key, value string) kvstore.Record {
		return kvstore.Record{Kind: kvstore.KindConfigMap, Namespace: "test", Key: key, Value: []byte(`{"metadata":{"name":"` + key + `","namespace":"test","labels":{"agentapi.proxy/type":"task"}},"data":{"task.json":"` + value + `"}}`)}
	}
	_, _ = primary.Create(ctx, configMap("primary-only", "value"))
	_, _ = secondary.Create(ctx, configMap("secondary-only", "value"))
	_, _ = primary.Create(ctx, configMap("different", "primary"))
	_, _ = secondary.Create(ctx, configMap("different", "secondary"))

	result, err := verifyKVStores(ctx, primary, secondary, "test")
	if !strings.Contains(err.Error(), "3 mismatched") || result.MissingPrimary != 1 || result.MissingSecondary != 1 || result.Different != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestWriteKVStoreVerificationResultJSON(t *testing.T) {
	var output bytes.Buffer
	result := kvStoreVerificationResult{Matched: 1, Entries: []kvStoreVerificationEntry{}}
	if err := writeKVStoreVerificationResult(&output, result, "json"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"matched": 1`) {
		t.Fatalf("output = %s", output.String())
	}
}

func TestVerifyKVStoresReturnsCollectionError(t *testing.T) {
	broken := &failingListStore{memoryKVStore: newMemoryKVStore()}
	_, err := verifyKVStores(context.Background(), broken, newMemoryKVStore(), "test")
	if err == nil {
		t.Fatal("verifyKVStores() succeeded")
	}
}

type failingListStore struct {
	*memoryKVStore
}

func (s *failingListStore) List(context.Context, kvstore.Query) ([]kvstore.Record, error) {
	return nil, errors.New("list failed")
}
