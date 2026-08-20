package kvstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestKubernetesStoreSecretLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewKubernetesStore(fake.NewSimpleClientset())
	value, _ := json.Marshal(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "settings"}, Data: map[string][]byte{"key": []byte("old")}})
	created, err := store.Create(ctx, Record{Kind: KindSecret, Namespace: "ns", Key: "settings", Value: value})
	if err != nil {
		t.Fatal(err)
	}
	var updatedObject corev1.Secret
	if err := json.Unmarshal(created.Value, &updatedObject); err != nil {
		t.Fatal(err)
	}
	updatedObject.Data["key"] = []byte("new")
	created.Value, _ = json.Marshal(&updatedObject)
	updated, err := store.Update(ctx, created)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, KindSecret, "ns", "settings")
	if err != nil {
		t.Fatal(err)
	}
	var gotObject corev1.Secret
	if err := json.Unmarshal(got.Value, &gotObject); err != nil {
		t.Fatal(err)
	}
	if string(gotObject.Data["key"]) != "new" {
		t.Fatalf("secret data = %q", gotObject.Data["key"])
	}
	if err := store.Delete(ctx, KindSecret, "ns", "settings", updated.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, KindSecret, "ns", "settings"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}
