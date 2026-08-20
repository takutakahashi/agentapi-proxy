package kvstore

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

var secretResource = schema.GroupResource{Resource: "secrets"}
var configMapResource = schema.GroupResource{Resource: "configmaps"}

// KubernetesAdapter routes Secret and ConfigMap KV operations through Store.
type KubernetesAdapter struct {
	kubernetes.Interface
	store Store
}

func NewKubernetesAdapter(base kubernetes.Interface, store Store) kubernetes.Interface {
	return &KubernetesAdapter{Interface: base, store: store}
}

func (c *KubernetesAdapter) CoreV1() typedcorev1.CoreV1Interface {
	return &coreAdapter{CoreV1Interface: c.Interface.CoreV1(), store: c.store}
}

type coreAdapter struct {
	typedcorev1.CoreV1Interface
	store Store
}

func (c *coreAdapter) Secrets(namespace string) typedcorev1.SecretInterface {
	return &secretAdapter{SecretInterface: c.CoreV1Interface.Secrets(namespace), store: c.store, namespace: namespace}
}

func (c *coreAdapter) ConfigMaps(namespace string) typedcorev1.ConfigMapInterface {
	return &configMapAdapter{ConfigMapInterface: c.CoreV1Interface.ConfigMaps(namespace), store: c.store, namespace: namespace}
}

type secretAdapter struct {
	typedcorev1.SecretInterface
	store     Store
	namespace string
}

func (a *secretAdapter) Create(ctx context.Context, object *corev1.Secret, opts metav1.CreateOptions) (*corev1.Secret, error) {
	object = materializeSecretStringData(object)
	value, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	record, err := a.store.Create(ctx, Record{Kind: KindSecret, Namespace: a.namespace, Key: object.Name, Value: value})
	if err != nil {
		return nil, createStorageError(secretResource, object.Name, err)
	}
	result := object.DeepCopy()
	result.Namespace = a.namespace
	result.ResourceVersion = strconv.FormatInt(record.Version, 10)
	return result, nil
}

func (a *secretAdapter) Update(ctx context.Context, object *corev1.Secret, opts metav1.UpdateOptions) (*corev1.Secret, error) {
	object = materializeSecretStringData(object)
	version, _ := strconv.ParseInt(object.ResourceVersion, 10, 64)
	value, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	record, err := a.store.Update(ctx, Record{Kind: KindSecret, Namespace: a.namespace, Key: object.Name, Value: value, Version: version})
	if err != nil {
		return nil, storageError(secretResource, object.Name, err)
	}
	result := object.DeepCopy()
	result.ResourceVersion = strconv.FormatInt(record.Version, 10)
	return result, nil
}

func (a *secretAdapter) Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Secret, error) {
	record, err := a.store.Get(ctx, KindSecret, a.namespace, name)
	if err != nil {
		return nil, storageError(secretResource, name, err)
	}
	var object corev1.Secret
	if err := json.Unmarshal(record.Value, &object); err != nil {
		return nil, err
	}
	object = *materializeSecretStringData(&object)
	object.Namespace = a.namespace
	object.ResourceVersion = strconv.FormatInt(record.Version, 10)
	return &object, nil
}

func (a *secretAdapter) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	record, err := a.store.Get(ctx, KindSecret, a.namespace, name)
	if err == nil {
		err = a.store.Delete(ctx, KindSecret, a.namespace, name, record.Version)
	}
	if err != nil {
		return storageError(secretResource, name, err)
	}
	return nil
}

func (a *secretAdapter) List(ctx context.Context, opts metav1.ListOptions) (*corev1.SecretList, error) {
	records, err := a.store.List(ctx, Query{Kind: KindSecret, Namespace: a.namespace})
	if err != nil {
		return nil, err
	}
	selector, err := labels.Parse(opts.LabelSelector)
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	result := &corev1.SecretList{}
	for _, record := range records {
		var object corev1.Secret
		if err := json.Unmarshal(record.Value, &object); err != nil {
			return nil, err
		}
		object = *materializeSecretStringData(&object)
		object.ResourceVersion = strconv.FormatInt(record.Version, 10)
		if selector.Matches(labels.Set(object.Labels)) {
			result.Items = append(result.Items, object)
		}
	}
	return result, nil
}

// The Kubernetes apiserver converts write-only stringData into Data before a
// Secret is persisted. The KV adapter must provide the same contract, both for
// new writes and for records written by older adapter versions.
func materializeSecretStringData(object *corev1.Secret) *corev1.Secret {
	if object == nil || len(object.StringData) == 0 {
		return object
	}
	copy := object.DeepCopy()
	if copy.Data == nil {
		copy.Data = make(map[string][]byte, len(copy.StringData))
	}
	for key, value := range copy.StringData {
		copy.Data[key] = []byte(value)
	}
	copy.StringData = nil
	return copy
}

type configMapAdapter struct {
	typedcorev1.ConfigMapInterface
	store     Store
	namespace string
}

func (a *configMapAdapter) Create(ctx context.Context, object *corev1.ConfigMap, opts metav1.CreateOptions) (*corev1.ConfigMap, error) {
	value, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	record, err := a.store.Create(ctx, Record{Kind: KindConfigMap, Namespace: a.namespace, Key: object.Name, Value: value})
	if err != nil {
		return nil, createStorageError(configMapResource, object.Name, err)
	}
	result := object.DeepCopy()
	result.Namespace = a.namespace
	result.ResourceVersion = strconv.FormatInt(record.Version, 10)
	return result, nil
}

func (a *configMapAdapter) Update(ctx context.Context, object *corev1.ConfigMap, opts metav1.UpdateOptions) (*corev1.ConfigMap, error) {
	version, _ := strconv.ParseInt(object.ResourceVersion, 10, 64)
	value, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	record, err := a.store.Update(ctx, Record{Kind: KindConfigMap, Namespace: a.namespace, Key: object.Name, Value: value, Version: version})
	if err != nil {
		return nil, storageError(configMapResource, object.Name, err)
	}
	result := object.DeepCopy()
	result.ResourceVersion = strconv.FormatInt(record.Version, 10)
	return result, nil
}

func (a *configMapAdapter) Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.ConfigMap, error) {
	record, err := a.store.Get(ctx, KindConfigMap, a.namespace, name)
	if err != nil {
		return nil, storageError(configMapResource, name, err)
	}
	var object corev1.ConfigMap
	if err := json.Unmarshal(record.Value, &object); err != nil {
		return nil, err
	}
	object.Namespace = a.namespace
	object.ResourceVersion = strconv.FormatInt(record.Version, 10)
	return &object, nil
}

func (a *configMapAdapter) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	record, err := a.store.Get(ctx, KindConfigMap, a.namespace, name)
	if err == nil {
		err = a.store.Delete(ctx, KindConfigMap, a.namespace, name, record.Version)
	}
	if err != nil {
		return storageError(configMapResource, name, err)
	}
	return nil
}

func (a *configMapAdapter) List(ctx context.Context, opts metav1.ListOptions) (*corev1.ConfigMapList, error) {
	records, err := a.store.List(ctx, Query{Kind: KindConfigMap, Namespace: a.namespace})
	if err != nil {
		return nil, err
	}
	selector, err := labels.Parse(opts.LabelSelector)
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	result := &corev1.ConfigMapList{}
	for _, record := range records {
		var object corev1.ConfigMap
		if err := json.Unmarshal(record.Value, &object); err != nil {
			return nil, err
		}
		object.ResourceVersion = strconv.FormatInt(record.Version, 10)
		if selector.Matches(labels.Set(object.Labels)) {
			result.Items = append(result.Items, object)
		}
	}
	return result, nil
}

func storageError(resource schema.GroupResource, name string, err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return apierrors.NewNotFound(resource, name)
	case errors.Is(err, ErrConflict):
		return apierrors.NewConflict(resource, name, err)
	default:
		return err
	}
}

func createStorageError(resource schema.GroupResource, name string, err error) error {
	if errors.Is(err, ErrConflict) {
		return apierrors.NewAlreadyExists(resource, name)
	}
	return storageError(resource, name, err)
}
