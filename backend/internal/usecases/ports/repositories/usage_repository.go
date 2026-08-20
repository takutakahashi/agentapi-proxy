package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

type UsageRepository interface {
	InsertEvents(context.Context, []entities.UsageEvent) (entities.UsageInsertResult, error)
	Aggregate(context.Context, entities.UsageQuery) (entities.UsageSummary, error)
	ListEvents(context.Context, entities.UsageQuery) ([]entities.UsageEvent, error)
	Close() error
}
