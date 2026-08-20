package kvstore

import (
	"context"
	"errors"
	"testing"
)

type failingStore struct {
	*memoryStore
	createErr error
	updateErr error
	deleteErr error
}

type normalizingStore struct{ *memoryStore }

func (s *normalizingStore) Create(ctx context.Context, record Record) (Record, error) {
	record.Value = append(record.Value, []byte("-canonical")...)
	return s.memoryStore.Create(ctx, record)
}

func (s *normalizingStore) Update(ctx context.Context, record Record) (Record, error) {
	record.Value = append(record.Value, []byte("-canonical")...)
	return s.memoryStore.Update(ctx, record)
}

func (s *failingStore) Create(ctx context.Context, record Record) (Record, error) {
	if s.createErr != nil {
		return Record{}, s.createErr
	}
	return s.memoryStore.Create(ctx, record)
}

func (s *failingStore) Update(ctx context.Context, record Record) (Record, error) {
	if s.updateErr != nil {
		return Record{}, s.updateErr
	}
	return s.memoryStore.Update(ctx, record)
}

func (s *failingStore) Delete(ctx context.Context, kind Kind, namespace, key string, version int64) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return s.memoryStore.Delete(ctx, kind, namespace, key, version)
}

func TestReplicatedStoreCreateWritesBoth(t *testing.T) {
	primary, secondary := newMemoryStore(), newMemoryStore()
	store, err := NewReplicatedStore(primary, secondary, ReplicationModeRollback)
	if err != nil {
		t.Fatal(err)
	}
	record := Record{Kind: KindSecret, Namespace: "ns", Key: "key", Value: []byte("value")}
	created, err := store.Create(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || len(primary.records) != 1 || len(secondary.records) != 1 {
		t.Fatalf("create was not replicated: created=%+v primary=%v secondary=%v", created, primary.records, secondary.records)
	}
}

func TestReplicatedStoreMirrorsPrimaryCanonicalValue(t *testing.T) {
	ctx := context.Background()
	primary := &normalizingStore{memoryStore: newMemoryStore()}
	secondary := newMemoryStore()
	store, _ := NewReplicatedStore(primary, secondary, ReplicationModeRollback)
	created, err := store.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: "key", Value: []byte("value")})
	if err != nil {
		t.Fatal(err)
	}
	secondaryRecord, err := secondary.Get(ctx, created.Kind, created.Namespace, created.Key)
	if err != nil {
		t.Fatal(err)
	}
	if string(created.Value) != "value-canonical" || string(secondaryRecord.Value) != string(created.Value) {
		t.Fatalf("primary=%q secondary=%q", created.Value, secondaryRecord.Value)
	}
}

func TestReplicatedStoreCreateRollsBackPrimary(t *testing.T) {
	primary := newMemoryStore()
	secondary := &failingStore{memoryStore: newMemoryStore(), createErr: errors.New("secondary unavailable")}
	store, _ := NewReplicatedStore(primary, secondary, ReplicationModeRollback)
	_, err := store.Create(context.Background(), Record{Kind: KindSecret, Namespace: "ns", Key: "key"})
	if err == nil {
		t.Fatal("Create() succeeded, want error")
	}
	if len(primary.records) != 0 {
		t.Fatalf("primary was not rolled back: %v", primary.records)
	}
}

func TestReplicatedStoreUpdateRollsBackPrimary(t *testing.T) {
	ctx := context.Background()
	primary := newMemoryStore()
	secondaryMemory := newMemoryStore()
	original := Record{Kind: KindConfigMap, Namespace: "ns", Key: "key", Value: []byte("old")}
	created, _ := primary.Create(ctx, original)
	_, _ = secondaryMemory.Create(ctx, original)
	secondary := &failingStore{memoryStore: secondaryMemory, updateErr: errors.New("secondary unavailable")}
	store, _ := NewReplicatedStore(primary, secondary, ReplicationModeRollback)
	created.Value = []byte("new")
	if _, err := store.Update(ctx, created); err == nil {
		t.Fatal("Update() succeeded, want error")
	}
	got, _ := primary.Get(ctx, original.Kind, original.Namespace, original.Key)
	if string(got.Value) != "old" || got.Version != 3 {
		t.Fatalf("primary rollback = %+v, want old value at version 3", got)
	}
}

func TestReplicatedStoreUpdateRepairsMissingSecondary(t *testing.T) {
	ctx := context.Background()
	primary, secondary := newMemoryStore(), newMemoryStore()
	created, _ := primary.Create(ctx, Record{Kind: KindConfigMap, Namespace: "ns", Key: "key", Value: []byte("old")})
	store, _ := NewReplicatedStore(primary, secondary, ReplicationModeRollback)
	created.Value = []byte("new")
	if _, err := store.Update(ctx, created); err != nil {
		t.Fatal(err)
	}
	got, err := secondary.Get(ctx, created.Kind, created.Namespace, created.Key)
	if err != nil || string(got.Value) != "new" {
		t.Fatalf("secondary = %+v, err=%v", got, err)
	}
}

func TestReplicatedStoreBestEffortKeepsPrimary(t *testing.T) {
	primary := newMemoryStore()
	secondary := &failingStore{memoryStore: newMemoryStore(), createErr: errors.New("secondary unavailable")}
	store, _ := NewReplicatedStore(primary, secondary, ReplicationModeBestEffort)
	if _, err := store.Create(context.Background(), Record{Kind: KindSecret, Namespace: "ns", Key: "key"}); err != nil {
		t.Fatal(err)
	}
	if len(primary.records) != 1 {
		t.Fatal("best-effort create did not retain primary")
	}
}

func TestReplicatedStoreReportsInDoubtWhenRollbackFails(t *testing.T) {
	primaryMemory := newMemoryStore()
	primary := &failingStore{memoryStore: primaryMemory, deleteErr: errors.New("rollback unavailable")}
	secondary := &failingStore{memoryStore: newMemoryStore(), createErr: errors.New("secondary unavailable")}
	store, _ := NewReplicatedStore(primary, secondary, ReplicationModeRollback)
	_, err := store.Create(context.Background(), Record{Kind: KindSecret, Namespace: "ns", Key: "key"})
	if !errors.Is(err, ErrInDoubt) {
		t.Fatalf("Create() error = %v, want ErrInDoubt", err)
	}
}
