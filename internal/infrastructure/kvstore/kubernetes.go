package kvstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// KubernetesStore exposes Kubernetes Secrets and ConfigMaps through Store.
type KubernetesStore struct{ client kubernetes.Interface }

func NewKubernetesStore(client kubernetes.Interface) *KubernetesStore {
	return &KubernetesStore{client: client}
}

func (s *KubernetesStore) Close() error { return nil }

func (s *KubernetesStore) Create(ctx context.Context, record Record) (Record, error) {
	switch record.Kind {
	case KindSecret:
		var object corev1.Secret
		if err := json.Unmarshal(record.Value, &object); err != nil {
			return Record{}, err
		}
		object.Namespace, object.ResourceVersion = record.Namespace, ""
		created, err := s.client.CoreV1().Secrets(record.Namespace).Create(ctx, &object, metav1.CreateOptions{})
		return secretRecord(created, err)
	case KindConfigMap:
		var object corev1.ConfigMap
		if err := json.Unmarshal(record.Value, &object); err != nil {
			return Record{}, err
		}
		object.Namespace, object.ResourceVersion = record.Namespace, ""
		created, err := s.client.CoreV1().ConfigMaps(record.Namespace).Create(ctx, &object, metav1.CreateOptions{})
		return configMapRecord(created, err)
	default:
		return Record{}, fmt.Errorf("unsupported kv kind %q", record.Kind)
	}
}

func (s *KubernetesStore) Update(ctx context.Context, record Record) (Record, error) {
	resourceVersion := strconv.FormatInt(record.Version, 10)
	switch record.Kind {
	case KindSecret:
		var object corev1.Secret
		if err := json.Unmarshal(record.Value, &object); err != nil {
			return Record{}, err
		}
		object.Namespace, object.ResourceVersion = record.Namespace, resourceVersion
		updated, err := s.client.CoreV1().Secrets(record.Namespace).Update(ctx, &object, metav1.UpdateOptions{})
		return secretRecord(updated, err)
	case KindConfigMap:
		var object corev1.ConfigMap
		if err := json.Unmarshal(record.Value, &object); err != nil {
			return Record{}, err
		}
		object.Namespace, object.ResourceVersion = record.Namespace, resourceVersion
		updated, err := s.client.CoreV1().ConfigMaps(record.Namespace).Update(ctx, &object, metav1.UpdateOptions{})
		return configMapRecord(updated, err)
	default:
		return Record{}, fmt.Errorf("unsupported kv kind %q", record.Kind)
	}
}

func (s *KubernetesStore) Get(ctx context.Context, kind Kind, namespace, key string) (Record, error) {
	switch kind {
	case KindSecret:
		object, err := s.client.CoreV1().Secrets(namespace).Get(ctx, key, metav1.GetOptions{})
		return secretRecord(object, err)
	case KindConfigMap:
		object, err := s.client.CoreV1().ConfigMaps(namespace).Get(ctx, key, metav1.GetOptions{})
		return configMapRecord(object, err)
	default:
		return Record{}, fmt.Errorf("unsupported kv kind %q", kind)
	}
}

func (s *KubernetesStore) Delete(ctx context.Context, kind Kind, namespace, key string, version int64) error {
	resourceVersion := strconv.FormatInt(version, 10)
	opts := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{ResourceVersion: &resourceVersion}}
	var err error
	switch kind {
	case KindSecret:
		err = s.client.CoreV1().Secrets(namespace).Delete(ctx, key, opts)
	case KindConfigMap:
		err = s.client.CoreV1().ConfigMaps(namespace).Delete(ctx, key, opts)
	default:
		return fmt.Errorf("unsupported kv kind %q", kind)
	}
	return translateKubernetesError(err)
}

func (s *KubernetesStore) List(ctx context.Context, query Query) ([]Record, error) {
	var records []Record
	switch query.Kind {
	case KindSecret:
		list, err := s.client.CoreV1().Secrets(query.Namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.Everything().String()})
		if err != nil {
			return nil, translateKubernetesError(err)
		}
		for i := range list.Items {
			record, err := secretRecord(&list.Items[i], nil)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		}
	case KindConfigMap:
		list, err := s.client.CoreV1().ConfigMaps(query.Namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.Everything().String()})
		if err != nil {
			return nil, translateKubernetesError(err)
		}
		for i := range list.Items {
			record, err := configMapRecord(&list.Items[i], nil)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		}
	default:
		return nil, fmt.Errorf("unsupported kv kind %q", query.Kind)
	}
	return records, nil
}

func secretRecord(object *corev1.Secret, err error) (Record, error) {
	if err != nil {
		return Record{}, translateKubernetesError(err)
	}
	value, err := json.Marshal(object)
	if err != nil {
		return Record{}, err
	}
	version, err := parseResourceVersion(object.ResourceVersion)
	if err != nil {
		return Record{}, err
	}
	return Record{Kind: KindSecret, Namespace: object.Namespace, Key: object.Name, Value: value, Version: version}, nil
}

func configMapRecord(object *corev1.ConfigMap, err error) (Record, error) {
	if err != nil {
		return Record{}, translateKubernetesError(err)
	}
	value, err := json.Marshal(object)
	if err != nil {
		return Record{}, err
	}
	version, err := parseResourceVersion(object.ResourceVersion)
	if err != nil {
		return Record{}, err
	}
	return Record{Kind: KindConfigMap, Namespace: object.Namespace, Key: object.Name, Value: value, Version: version}, nil
}

func parseResourceVersion(value string) (int64, error) {
	// client-go's fake client leaves resourceVersion empty. Treat it as the
	// initial version so replicated persistence also works in local fallback mode.
	if value == "" {
		return 1, nil
	}
	version, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse Kubernetes resourceVersion %q: %w", value, err)
	}
	return version, nil
}

func translateKubernetesError(err error) error {
	switch {
	case err == nil:
		return nil
	case apierrors.IsNotFound(err):
		return ErrNotFound
	case apierrors.IsAlreadyExists(err), apierrors.IsConflict(err):
		return ErrConflict
	default:
		return err
	}
}
