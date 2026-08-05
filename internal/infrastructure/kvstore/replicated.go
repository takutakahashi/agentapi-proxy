package kvstore

import (
	"context"
	"errors"
	"fmt"
	"log"
)

type ReplicationMode string

const (
	ReplicationModeBestEffort ReplicationMode = "best_effort"
	ReplicationModeRollback   ReplicationMode = "rollback"
)

// ReplicatedStore reads from primary and mirrors mutations to secondary.
type ReplicatedStore struct {
	primary   Store
	secondary Store
	mode      ReplicationMode
}

func NewReplicatedStore(primary, secondary Store, mode ReplicationMode) (*ReplicatedStore, error) {
	if primary == nil || secondary == nil {
		return nil, errors.New("primary and secondary stores are required")
	}
	if mode != ReplicationModeBestEffort && mode != ReplicationModeRollback {
		return nil, fmt.Errorf("unsupported replication mode %q", mode)
	}
	return &ReplicatedStore{primary: primary, secondary: secondary, mode: mode}, nil
}

func (s *ReplicatedStore) Get(ctx context.Context, kind Kind, namespace, key string) (Record, error) {
	return s.primary.Get(ctx, kind, namespace, key)
}

func (s *ReplicatedStore) List(ctx context.Context, query Query) ([]Record, error) {
	return s.primary.List(ctx, query)
}

func (s *ReplicatedStore) Create(ctx context.Context, record Record) (Record, error) {
	created, err := s.primary.Create(ctx, record)
	if err != nil {
		return Record{}, err
	}
	secondaryRecord := created
	secondaryRecord.Version = 0
	if _, secondaryErr := s.secondary.Create(ctx, secondaryRecord); secondaryErr != nil {
		return s.handleCreateFailure(ctx, created, secondaryErr)
	}
	return created, nil
}

func (s *ReplicatedStore) handleCreateFailure(ctx context.Context, created Record, secondaryErr error) (Record, error) {
	if s.mode == ReplicationModeBestEffort {
		log.Printf("[KV_REPLICATION] secondary create failed kind=%s namespace=%s key=%s: %v", created.Kind, created.Namespace, created.Key, secondaryErr)
		return created, nil
	}
	if rollbackErr := s.primary.Delete(ctx, created.Kind, created.Namespace, created.Key, created.Version); rollbackErr != nil {
		return Record{}, fmt.Errorf("%w: secondary create: %v; primary rollback: %v", ErrInDoubt, secondaryErr, rollbackErr)
	}
	return Record{}, fmt.Errorf("secondary create failed and primary was rolled back: %w", secondaryErr)
}

func (s *ReplicatedStore) Update(ctx context.Context, record Record) (Record, error) {
	previous, err := s.primary.Get(ctx, record.Kind, record.Namespace, record.Key)
	if err != nil {
		return Record{}, err
	}
	updated, err := s.primary.Update(ctx, record)
	if err != nil {
		return Record{}, err
	}
	secondaryRecord, secondaryErr := s.secondary.Get(ctx, record.Kind, record.Namespace, record.Key)
	switch {
	case secondaryErr == nil:
		secondaryRecord.Value = updated.Value
		_, secondaryErr = s.secondary.Update(ctx, secondaryRecord)
	case errors.Is(secondaryErr, ErrNotFound):
		secondaryRecord = updated
		secondaryRecord.Version = 0
		_, secondaryErr = s.secondary.Create(ctx, secondaryRecord)
	}
	if secondaryErr == nil {
		return updated, nil
	}
	if s.mode == ReplicationModeBestEffort {
		log.Printf("[KV_REPLICATION] secondary update failed kind=%s namespace=%s key=%s: %v", record.Kind, record.Namespace, record.Key, secondaryErr)
		return updated, nil
	}
	previous.Version = updated.Version
	if _, rollbackErr := s.primary.Update(ctx, previous); rollbackErr != nil {
		return Record{}, fmt.Errorf("%w: secondary update: %v; primary rollback: %v", ErrInDoubt, secondaryErr, rollbackErr)
	}
	return Record{}, fmt.Errorf("secondary update failed and primary was rolled back: %w", secondaryErr)
}

func (s *ReplicatedStore) Delete(ctx context.Context, kind Kind, namespace, key string, version int64) error {
	previous, err := s.primary.Get(ctx, kind, namespace, key)
	if err != nil {
		return err
	}
	secondaryRecord, secondaryErr := s.secondary.Get(ctx, kind, namespace, key)
	if errors.Is(secondaryErr, ErrNotFound) {
		secondaryErr = nil
		secondaryRecord = Record{}
	} else if secondaryErr != nil && s.mode == ReplicationModeRollback {
		return fmt.Errorf("secondary read before delete: %w", secondaryErr)
	}
	if err := s.primary.Delete(ctx, kind, namespace, key, version); err != nil {
		return err
	}
	if secondaryRecord.Key != "" {
		secondaryErr = s.secondary.Delete(ctx, kind, namespace, key, secondaryRecord.Version)
	}
	if secondaryErr == nil {
		return nil
	}
	if s.mode == ReplicationModeBestEffort {
		log.Printf("[KV_REPLICATION] secondary delete failed kind=%s namespace=%s key=%s: %v", kind, namespace, key, secondaryErr)
		return nil
	}
	previous.Version = 0
	if _, rollbackErr := s.primary.Create(ctx, previous); rollbackErr != nil {
		return fmt.Errorf("%w: secondary delete: %v; primary rollback: %v", ErrInDoubt, secondaryErr, rollbackErr)
	}
	return fmt.Errorf("secondary delete failed and primary was rolled back: %w", secondaryErr)
}

func (s *ReplicatedStore) Close() error {
	return errors.Join(s.primary.Close(), s.secondary.Close())
}
