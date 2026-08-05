package kvstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tursodatabase/libsql-client-go/libsql"
)

type LibSQLStore struct{ db *sql.DB }

func NewLibSQLStore(ctx context.Context, databaseURL, authToken string) (*LibSQLStore, error) {
	opts := []libsql.Option{}
	if authToken != "" {
		opts = append(opts, libsql.WithAuthToken(authToken))
	}
	connector, err := libsql.NewConnector(databaseURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("create libSQL connector: %w", err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(8)
	s := &LibSQLStore{db: db}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS agentapi_kv (
kind TEXT NOT NULL, namespace TEXT NOT NULL, key TEXT NOT NULL,
version INTEGER NOT NULL, value BLOB NOT NULL, updated_at TEXT NOT NULL,
PRIMARY KEY (kind, namespace, key))`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize libSQL schema: %w", err)
	}
	return s, nil
}

func (s *LibSQLStore) Close() error { return s.db.Close() }

func (s *LibSQLStore) Create(ctx context.Context, record Record) (Record, error) {
	record.Version = 1
	_, err := s.db.ExecContext(ctx, `INSERT INTO agentapi_kv
(kind, namespace, key, version, value, updated_at) VALUES (?, ?, ?, 1, ?, ?)`,
		record.Kind, record.Namespace, record.Key, record.Value, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		if _, getErr := s.Get(ctx, record.Kind, record.Namespace, record.Key); getErr == nil {
			return Record{}, ErrConflict
		}
		return Record{}, fmt.Errorf("create libSQL record: %w", err)
	}
	return record, nil
}

func (s *LibSQLStore) Update(ctx context.Context, record Record) (Record, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE agentapi_kv SET version = version + 1,
value = ?, updated_at = ? WHERE kind = ? AND namespace = ? AND key = ? AND version = ?`,
		record.Value, time.Now().UTC().Format(time.RFC3339Nano), record.Kind, record.Namespace, record.Key, record.Version)
	if err != nil {
		return Record{}, fmt.Errorf("update libSQL record: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Record{}, fmt.Errorf("read libSQL update result: %w", err)
	}
	if affected == 0 {
		return Record{}, ErrConflict
	}
	record.Version++
	return record, nil
}

func (s *LibSQLStore) Get(ctx context.Context, kind Kind, namespace, key string) (Record, error) {
	record := Record{Kind: kind, Namespace: namespace, Key: key}
	err := s.db.QueryRowContext(ctx, `SELECT version, value FROM agentapi_kv
WHERE kind = ? AND namespace = ? AND key = ?`, kind, namespace, key).Scan(&record.Version, &record.Value)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("get libSQL record: %w", err)
	}
	return record, nil
}

func (s *LibSQLStore) Delete(ctx context.Context, kind Kind, namespace, key string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM agentapi_kv WHERE kind = ? AND namespace = ? AND key = ?`, kind, namespace, key)
	if err != nil {
		return fmt.Errorf("delete libSQL record: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read libSQL delete result: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *LibSQLStore) List(ctx context.Context, query Query) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, version, value FROM agentapi_kv
WHERE kind = ? AND namespace = ? ORDER BY key`, query.Kind, query.Namespace)
	if err != nil {
		return nil, fmt.Errorf("list libSQL records: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var records []Record
	for rows.Next() {
		record := Record{Kind: query.Kind, Namespace: query.Namespace}
		if err := rows.Scan(&record.Key, &record.Version, &record.Value); err != nil {
			return nil, fmt.Errorf("scan libSQL record: %w", err)
		}
		records = append(records, record)
	}
	return records, rows.Err()
}
