package sessionrunner

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound     = errors.New("session runner resource not found")
	ErrConflict     = errors.New("session runner resource conflict")
	ErrUnauthorized = errors.New("session runner unauthorized")
)

type Store interface {
	CreateManager(context.Context, *Manager) error
	GetManager(context.Context, string) (*Manager, error)
	ListManagers(context.Context) ([]*Manager, error)
	UpdateManager(context.Context, *Manager) error
	DeleteManager(context.Context, string) error

	CreateLogicalPool(context.Context, *LogicalPool) error
	GetLogicalPool(context.Context, string) (*LogicalPool, error)
	ListLogicalPools(context.Context) ([]*LogicalPool, error)
	UpdateLogicalPool(context.Context, *LogicalPool) error
	DeleteLogicalPool(context.Context, string) error

	CreatePoolSupplier(context.Context, *PoolSupplier) error
	GetPoolSupplier(context.Context, string, string) (*PoolSupplier, error)
	ListPoolSuppliers(context.Context) ([]*PoolSupplier, error)
	UpdatePoolSupplier(context.Context, *PoolSupplier) error
	DeletePoolSupplier(context.Context, string, string) error

	CreateBinding(context.Context, *Binding) error
	ListBindings(context.Context, string) ([]*Binding, error)
	UpdateBinding(context.Context, *Binding) error
	DeleteBinding(context.Context, string) error

	CreateRunner(context.Context, *Runner) error
	GetRunner(context.Context, string) (*Runner, error)
	UpdateRunner(context.Context, *Runner) error
	ListRunners(context.Context, string) ([]*Runner, error)
	DeleteRunner(context.Context, string) error

	Enqueue(context.Context, *Allocation) error
	GetAllocation(context.Context, string) (*Allocation, error)
	ListAllocations(context.Context, string) ([]*Allocation, error)
	DeleteAllocation(context.Context, string) error
	ClaimNext(context.Context, string, string, time.Duration) (*Allocation, bool, error)
	Acknowledge(context.Context, string, string, string) (*Allocation, error)
	Fail(context.Context, string, string, string) (*Allocation, error)
}
