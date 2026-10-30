package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
)

const (
	apiKeyColumns      = "id, owner_id, prefix, hash, created_at, revoked_at"
	insertAPIKey       = `INSERT INTO api_keys (id, owner_id, prefix, hash, created_at) VALUES ($1, $2, $3, $4, $5)`
	selectAPIKey       = `SELECT ` + apiKeyColumns + ` FROM api_keys WHERE id = $1`
	selectAPIKeyPrefix = `SELECT ` + apiKeyColumns + ` FROM api_keys WHERE prefix = $1`
	updateAPIKey       = `UPDATE api_keys SET revoked_at = $2 WHERE id = $1`
)

type APIKeyRepository struct {
	database querier
}

func NewAPIKeyRepository(pool *pgxpool.Pool) *APIKeyRepository {
	return &APIKeyRepository{database: pool}
}

func (r *APIKeyRepository) Save(ctx context.Context, key apikey.APIKey) error {
	_, err := r.database.Exec(ctx, insertAPIKey, key.ID(), key.OwnerID(), key.Prefix(), key.Hash(), key.CreatedAt())
	return err
}

func (r *APIKeyRepository) FindByPrefix(ctx context.Context, prefix apikey.Prefix) (apikey.APIKey, error) {
	return scanAPIKey(r.database.QueryRow(ctx, selectAPIKeyPrefix, prefix))
}

func (r *APIKeyRepository) FindByID(ctx context.Context, id apikey.ID) (apikey.APIKey, error) {
	return scanAPIKey(r.database.QueryRow(ctx, selectAPIKey, id))
}

func (r *APIKeyRepository) Update(ctx context.Context, key apikey.APIKey) error {
	_, err := r.database.Exec(ctx, updateAPIKey, key.ID(), key.RevokedAt())
	return err
}

func scanAPIKey(row pgx.Row) (apikey.APIKey, error) {
	var id apikey.ID
	var owner apikey.OwnerID
	var prefix apikey.Prefix
	var hash string
	var createdAt time.Time
	var revokedAt *time.Time
	if err := row.Scan(&id, &owner, &prefix, &hash, &createdAt, &revokedAt); err != nil {
		return apikey.APIKey{}, notFound(err, apikey.ErrAPIKeyNotFound)
	}
	key, err := apikey.NewMoney(id, owner, prefix, hash, createdAt)
	if revokedAt != nil {
		key = apikey.Restore(key, *revokedAt)
	}
	return key, err
}
