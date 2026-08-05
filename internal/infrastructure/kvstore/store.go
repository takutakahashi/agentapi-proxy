package kvstore

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("kv record not found")
var ErrConflict = errors.New("kv record version conflict")

type Kind string

const (
	KindSecret    Kind = "secret"
	KindConfigMap Kind = "configmap"
)

// Record is the storage-neutral representation of a Kubernetes object used as
// a KV document. Value contains the complete JSON object to preserve metadata.
type Record struct {
	Kind      Kind
	Namespace string
	Key       string
	Value     []byte
	Version   int64
}

type Query struct {
	Kind      Kind
	Namespace string
}

// Store is the single persistence boundary for Kubernetes-backed KV data.
type Store interface {
	Create(context.Context, Record) (Record, error)
	Update(context.Context, Record) (Record, error)
	Get(context.Context, Kind, string, string) (Record, error)
	Delete(context.Context, Kind, string, string) error
	List(context.Context, Query) ([]Record, error)
	Close() error
}
