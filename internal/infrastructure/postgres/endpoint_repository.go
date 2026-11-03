package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/webhook"
)

const (
	endpointColumns = "id, owner_id, url, secret, is_active, created_at"
	insertEndpoint  = `INSERT INTO webhook_endpoints (` + endpointColumns + `) VALUES ($1, $2, $3, $4, $5, $6)`
	selectEndpoint  = `SELECT ` + endpointColumns + ` FROM webhook_endpoints WHERE id = $1`
	listEndpoints   = `SELECT ` + endpointColumns + ` FROM webhook_endpoints WHERE owner_id = $1 ORDER BY id`
	countEndpoints  = `SELECT count(*) FROM webhook_endpoints WHERE owner_id = $1 AND is_active`
	updateEndpoint  = `UPDATE webhook_endpoints SET is_active = $2 WHERE id = $1`
	listActive      = `SELECT ` + endpointColumns + ` FROM webhook_endpoints
		WHERE owner_id = ANY($1::text[]::uuid[]) AND is_active ORDER BY id`
)

type EndpointRepository struct {
	db querier
}

func NewEndpointRepository(pool *pgxpool.Pool) *EndpointRepository {
	return &EndpointRepository{db: pool}
}

func (r *EndpointRepository) Save(ctx context.Context, endpoint webhook.Endpoint) error {
	_, err := r.db.Exec(ctx, insertEndpoint, endpoint.ID(), endpoint.OwnerID(), endpoint.URL(), endpoint.Secret(), endpoint.IsActive(), endpoint.CreatedAt())
	return err
}

func (r *EndpointRepository) FindByID(ctx context.Context, id webhook.EndpointID) (webhook.Endpoint, error) {
	var snapshot webhook.EndpointSnapshot
	err := r.db.QueryRow(ctx, selectEndpoint, id).Scan(endpointDestinations(&snapshot)...)
	return webhook.RestoreEndpoint(snapshot), notFound(err, webhook.ErrEndpointNotFound)
}

func (r *EndpointRepository) ListByOwner(ctx context.Context, owner apikey.OwnerID) ([]webhook.Endpoint, error) {
	rows, _ := r.db.Query(ctx, listEndpoints, owner)
	return pgx.CollectRows(rows, scanEndpointRow)
}

func (r *EndpointRepository) CountActiveByOwner(ctx context.Context, owner apikey.OwnerID) (int, error) {
	var active int
	err := r.db.QueryRow(ctx, countEndpoints, owner).Scan(&active)
	return active, err
}

func (r *EndpointRepository) Update(ctx context.Context, endpoint webhook.Endpoint) error {
	_, err := r.db.Exec(ctx, updateEndpoint, endpoint.ID(), endpoint.IsActive())
	return err
}

func (r *EndpointRepository) ListActiveByOwners(ctx context.Context, owners []apikey.OwnerID) ([]webhook.Endpoint, error) {
	rows, _ := r.db.Query(ctx, listActive, owners)
	return pgx.CollectRows(rows, scanEndpointRow)
}

func scanEndpointRow(row pgx.CollectableRow) (webhook.Endpoint, error) {
	var snapshot webhook.EndpointSnapshot
	err := row.Scan(endpointDestinations(&snapshot)...)
	return webhook.RestoreEndpoint(snapshot), err
}

func endpointDestinations(snapshot *webhook.EndpointSnapshot) []any {
	return []any{&snapshot.ID, &snapshot.OwnerID, &snapshot.URL, &snapshot.Secret, &snapshot.IsActive, &snapshot.CreatedAt}
}
