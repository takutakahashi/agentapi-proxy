package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCollectApplicationKVRecordsExcludesOperationalResources(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "test", Labels: map[string]string{"agentapi.proxy/settings": "true"}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "subscriptions", Namespace: "test", Labels: map[string]string{"app.kubernetes.io/component": "notification-subscription", "agentapi.proxy/user-id": "user"}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "helm-release", Namespace: "test", Labels: map[string]string{"owner": "helm"}}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "memory", Namespace: "test", Labels: map[string]string{"agentapi.proxy/type": "memory"}}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-shares", Namespace: "test"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "server-config", Namespace: "test", Labels: map[string]string{"app.kubernetes.io/name": "agentapi-proxy"}}},
	)

	records, err := collectApplicationKVRecords(context.Background(), client, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("got %d records, want 3: %#v", len(records), records)
	}
	if records[0].Kind != kvstore.KindConfigMap || records[0].Key != "agentapi-session-shares" {
		t.Fatalf("unexpected first record: %#v", records[0])
	}
	if records[1].Kind != kvstore.KindConfigMap || records[1].Key != "memory" {
		t.Fatalf("unexpected second record: %#v", records[1])
	}
	if records[2].Kind != kvstore.KindSecret || records[2].Key != "settings" {
		t.Fatalf("unexpected third record: %#v", records[2])
	}
}

func TestMigrateKubernetesKVIsIdempotent(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "test", Labels: map[string]string{"agentapi.proxy/settings": "true"}}, Data: map[string][]byte{"settings.json": []byte(`{"name":"user"}`)}},
	)
	store := newMemoryKVStore()
	options := kvStoreMigrateOptions{namespace: "test"}

	first, err := migrateKubernetesKV(context.Background(), client, store, options)
	if err != nil {
		t.Fatal(err)
	}
	if first.Copied != 1 || first.Skipped != 0 {
		t.Fatalf("unexpected first result: %#v", first)
	}
	second, err := migrateKubernetesKV(context.Background(), client, store, options)
	if err != nil {
		t.Fatal(err)
	}
	if second.Copied != 0 || second.Skipped != 1 {
		t.Fatalf("unexpected second result: %#v", second)
	}
}

func TestMigrateKubernetesKVConflictAndOverwrite(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "memory", Namespace: "test", Labels: map[string]string{"agentapi.proxy/type": "memory"}}, Data: map[string]string{"memory.json": "source"}},
	)
	store := newMemoryKVStore()
	_, err := store.Create(context.Background(), kvstore.Record{Kind: kvstore.KindConfigMap, Namespace: "test", Key: "memory", Value: []byte("destination")})
	if err != nil {
		t.Fatal(err)
	}

	result, err := migrateKubernetesKV(context.Background(), client, store, kvStoreMigrateOptions{namespace: "test"})
	if err == nil || result.Conflicts != 1 {
		t.Fatalf("expected one conflict, got result=%#v err=%v", result, err)
	}
	result, err = migrateKubernetesKV(context.Background(), client, store, kvStoreMigrateOptions{namespace: "test", overwrite: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 1 {
		t.Fatalf("expected one update, got %#v", result)
	}
}

func TestMigrateKubernetesKVDryRunDoesNotWrite(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-schedules", Namespace: "test"}},
	)
	store := newMemoryKVStore()
	result, err := migrateKubernetesKV(context.Background(), client, store, kvStoreMigrateOptions{namespace: "test", dryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Copied != 1 || result.Entries[0].Status != "would-copy" {
		t.Fatalf("unexpected dry-run result: %#v", result)
	}
	if _, err := store.Get(context.Background(), kvstore.KindSecret, "test", "agentapi-schedules"); !errors.Is(err, kvstore.ErrNotFound) {
		t.Fatalf("dry-run wrote a record: %v", err)
	}
}

func TestMigrateKubernetesKVToLocalLibSQLFile(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "test", Labels: map[string]string{"agentapi.proxy/settings": "true"}}, Data: map[string][]byte{"settings.json": []byte(`{"name":"local-e2e"}`)}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "memory", Namespace: "test", Labels: map[string]string{"agentapi.proxy/type": "memory"}}, Data: map[string]string{"memory.json": `{"title":"migrate me"}`}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "notification-subscriptions-user", Namespace: "test", Labels: map[string]string{"app.kubernetes.io/component": "notification-subscription"}}},
	)
	databaseURL := "file://" + filepath.Join(t.TempDir(), "migration.db")
	store, err := kvstore.NewLibSQLStore(ctx, databaseURL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	result, err := migrateKubernetesKV(ctx, client, store, kvStoreMigrateOptions{namespace: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Selected != 2 || result.Copied != 2 {
		t.Fatalf("unexpected migration result: %#v", result)
	}
	for _, identity := range []struct {
		kind kvstore.Kind
		key  string
	}{{kvstore.KindSecret, "settings"}, {kvstore.KindConfigMap, "memory"}} {
		if _, err := store.Get(ctx, identity.kind, "test", identity.key); err != nil {
			t.Fatalf("read migrated %s/%s: %v", identity.kind, identity.key, err)
		}
	}
	if _, err := store.Get(ctx, kvstore.KindSecret, "test", "notification-subscriptions-user"); !errors.Is(err, kvstore.ErrNotFound) {
		t.Fatalf("operational Secret was migrated: %v", err)
	}

	second, err := migrateKubernetesKV(ctx, client, store, kvStoreMigrateOptions{namespace: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Skipped != 2 {
		t.Fatalf("expected idempotent skips, got %#v", second)
	}
}

func TestMigrateConfiguredStorePair(t *testing.T) {
	ctx := context.Background()
	source, destination := newMemoryKVStore(), newMemoryKVStore()
	value := []byte(`{"metadata":{"name":"memory","namespace":"test","labels":{"agentapi.proxy/type":"memory"}},"data":{"memory.json":"source"}}`)
	if _, err := source.Create(ctx, kvstore.Record{Kind: kvstore.KindConfigMap, Namespace: "test", Key: "memory", Value: value}); err != nil {
		t.Fatal(err)
	}
	result, err := migrateKVStores(ctx, source, destination, kvStoreMigrateOptions{namespace: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Copied != 1 {
		t.Fatalf("result = %#v", result)
	}
	got, err := destination.Get(ctx, kvstore.KindConfigMap, "test", "memory")
	if err != nil || string(got.Value) != string(value) {
		t.Fatalf("destination record = %#v, err=%v", got, err)
	}
}

func TestMigrateConfiguredStorePairRewritesDestinationNamespace(t *testing.T) {
	ctx := context.Background()
	source, destination := newMemoryKVStore(), newMemoryKVStore()
	value := []byte(`{"metadata":{"name":"memory","namespace":"logical","labels":{"agentapi.proxy/type":"memory"}},"data":{"memory.json":"source"}}`)
	if _, err := source.Create(ctx, kvstore.Record{Kind: kvstore.KindConfigMap, Namespace: "logical", Key: "memory", Value: value}); err != nil {
		t.Fatal(err)
	}
	result, err := migrateKVStores(ctx, source, destination, kvStoreMigrateOptions{namespace: "logical", destinationNamespace: "runtime"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Copied != 1 || result.Entries[0].Namespace != "runtime" {
		t.Fatalf("result = %#v", result)
	}
	if _, err := destination.Get(ctx, kvstore.KindConfigMap, "runtime", "memory"); err != nil {
		t.Fatalf("destination record: %v", err)
	}
	if _, err := destination.Get(ctx, kvstore.KindConfigMap, "logical", "memory"); !errors.Is(err, kvstore.ErrNotFound) {
		t.Fatalf("logical namespace record err=%v, want not found", err)
	}
}

func TestMigrationStoreConfigsUseReplicatedEnvironment(t *testing.T) {
	options := kvStoreMigrateOptions{
		primaryBackend:       "kubernetes",
		secondaryBackend:     "libsql",
		secondaryDatabaseURL: "http://libsql:8080",
		secondaryAuthToken:   "token",
	}
	primary, secondary, err := options.storeConfigs()
	if err != nil {
		t.Fatal(err)
	}
	if primary.backend != "kubernetes" || secondary.backend != "libsql" || secondary.databaseURL != "http://libsql:8080" || secondary.authToken != "token" {
		t.Fatalf("primary=%#v secondary=%#v", primary, secondary)
	}
}

func TestMigrationStoreConfigsPreserveLegacyFlags(t *testing.T) {
	primary, secondary, err := (kvStoreMigrateOptions{legacyDatabaseURL: "file:///tmp/legacy.db", legacyAuthToken: "token"}).storeConfigs()
	if err != nil {
		t.Fatal(err)
	}
	if primary.backend != "kubernetes" || secondary.backend != "libsql" || secondary.databaseURL != "file:///tmp/legacy.db" {
		t.Fatalf("primary=%#v secondary=%#v", primary, secondary)
	}
}

type memoryKVStore struct {
	records map[string]kvstore.Record
}

func newMemoryKVStore() *memoryKVStore { return &memoryKVStore{records: map[string]kvstore.Record{}} }
func (s *memoryKVStore) Close() error  { return nil }

func memoryKVKey(kind kvstore.Kind, namespace, key string) string {
	return string(kind) + "/" + namespace + "/" + key
}

func (s *memoryKVStore) Create(_ context.Context, record kvstore.Record) (kvstore.Record, error) {
	key := memoryKVKey(record.Kind, record.Namespace, record.Key)
	if _, ok := s.records[key]; ok {
		return kvstore.Record{}, kvstore.ErrConflict
	}
	record.Version = 1
	s.records[key] = record
	return record, nil
}

func (s *memoryKVStore) Update(_ context.Context, record kvstore.Record) (kvstore.Record, error) {
	key := memoryKVKey(record.Kind, record.Namespace, record.Key)
	existing, ok := s.records[key]
	if !ok || existing.Version != record.Version {
		return kvstore.Record{}, kvstore.ErrConflict
	}
	record.Version++
	s.records[key] = record
	return record, nil
}

func (s *memoryKVStore) Get(_ context.Context, kind kvstore.Kind, namespace, key string) (kvstore.Record, error) {
	record, ok := s.records[memoryKVKey(kind, namespace, key)]
	if !ok {
		return kvstore.Record{}, kvstore.ErrNotFound
	}
	return record, nil
}

func (s *memoryKVStore) Delete(_ context.Context, kind kvstore.Kind, namespace, key string, version int64) error {
	mapKey := memoryKVKey(kind, namespace, key)
	record, ok := s.records[mapKey]
	if !ok {
		return kvstore.ErrNotFound
	}
	if record.Version != version {
		return kvstore.ErrConflict
	}
	delete(s.records, mapKey)
	return nil
}

func (s *memoryKVStore) List(_ context.Context, query kvstore.Query) ([]kvstore.Record, error) {
	var records []kvstore.Record
	for _, record := range s.records {
		if record.Kind == query.Kind && record.Namespace == query.Namespace {
			records = append(records, record)
		}
	}
	return records, nil
}
