package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

const insertProviderEvent = `INSERT INTO provider_events (provider, event_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`

type ProviderEventLog struct {
	database querier
}

func NewProviderEventLog(pool *pgxpool.Pool) *ProviderEventLog {
	return &ProviderEventLog{database: pool}
}

func (l *ProviderEventLog) Record(ctx context.Context, provider, eventID string) (bool, error) {
	tag, err := l.database.Exec(ctx, insertProviderEvent, provider, eventID)
	return tag.RowsAffected() == 1, err
}
