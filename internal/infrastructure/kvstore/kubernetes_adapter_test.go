package kvstore

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type memoryStore struct{ records map[string]Record }

func newMemoryStore() *memoryStore { return &memoryStore{records: map[string]Record{}} }
func recordKey(kind Kind, namespace, key string) string {
	return string(kind) + "/" + namespace + "/" + key
}
func (s *memoryStore) Create(_ context.Context, r Record) (Record, error) {
	k := recordKey(r.Kind, r.Namespace, r.Key)
	if _, ok := s.records[k]; ok {
		return Record{}, ErrConflict
	}
	r.Version = 1
	s.records[k] = r
	return r, nil
}
func (s *memoryStore) Update(_ context.Context, r Record) (Record, error) {
	k := recordKey(r.Kind, r.Namespace, r.Key)
	old, ok := s.records[k]
	if !ok || old.Version != r.Version {
		return Record{}, ErrConflict
	}
	r.Version++
	s.records[k] = r
	return r, nil
}
func (s *memoryStore) Get(_ context.Context, kind Kind, namespace, key string) (Record, error) {
	r, ok := s.records[recordKey(kind, namespace, key)]
	if !ok {
		return Record{}, ErrNotFound
	}
	return r, nil
}
func (s *memoryStore) Delete(_ context.Context, kind Kind, namespace, key string) error {
	k := recordKey(kind, namespace, key)
	if _, ok := s.records[k]; !ok {
		return ErrNotFound
	}
	delete(s.records, k)
	return nil
}
func (s *memoryStore) List(_ context.Context, q Query) ([]Record, error) {
	var out []Record
	for _, r := range s.records {
		if r.Kind == q.Kind && r.Namespace == q.Namespace {
			out = append(out, r)
		}
	}
	return out, nil
}
func (s *memoryStore) Close() error { return nil }

func TestAdapterPersistsSecretAndConfigMap(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset()
	store := newMemoryStore()
	client := NewKubernetesAdapter(kube, store)
	if _, err := client.CoreV1().Secrets("ns").Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-settings-test", Labels: map[string]string{"app": "test"}}, Data: map[string][]byte{"key": []byte("value")}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().ConfigMaps("ns").Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-task-test"}, Data: map[string]string{"key": "value"}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(store.records) != 2 {
		t.Fatalf("stored %d records, want 2", len(store.records))
	}
	if _, err := kube.CoreV1().Secrets("ns").Get(ctx, "agentapi-settings-test", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("Secret must not be written to Kubernetes: %v", err)
	}
	if _, err := kube.CoreV1().ConfigMaps("ns").Get(ctx, "agentapi-task-test", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("ConfigMap must not be written to Kubernetes: %v", err)
	}
}

func TestAdapterCreateConflictIsAlreadyExists(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	client := NewKubernetesAdapter(fake.NewSimpleClientset(), store)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "existing"}}
	if _, err := client.CoreV1().Secrets("ns").Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Secrets("ns").Create(ctx, secret, metav1.CreateOptions{}); !apierrors.IsAlreadyExists(err) {
		t.Fatalf("Create() error = %v, want AlreadyExists", err)
	}
}

func TestAdapterDoesNotReadLegacyKubernetesKV(t *testing.T) {
	ctx := context.Background()
	legacy := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-task-legacy", Namespace: "ns"}, Data: map[string]string{"old": "data"}}
	kube := fake.NewSimpleClientset(legacy)
	store := newMemoryStore()
	client := NewKubernetesAdapter(kube, store)
	got, err := client.CoreV1().ConfigMaps("ns").Get(ctx, "agentapi-task-legacy", metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("Get() error = %v, want NotFound (got=%v)", err, got)
	}
	if len(store.records) != 0 {
		t.Fatal("legacy read must not migrate data")
	}
}

func TestAdapterListUsesStoreOnly(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "legacy", Namespace: "ns", Labels: map[string]string{"scope": "user"}}})
	store := newMemoryStore()
	client := NewKubernetesAdapter(kube, store)
	if _, err := client.CoreV1().Secrets("ns").Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-settings-new", Labels: map[string]string{"scope": "user"}}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	list, err := client.CoreV1().Secrets("ns").List(ctx, metav1.ListOptions{LabelSelector: "scope=user"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("listed %d records, want 1", len(list.Items))
	}
}
