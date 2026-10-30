package rest

import apikeyservice "github.com/UrielJaloto/paygo/internal/services/apikey"

type issuedKeyResponse struct {
	ID        string `json:"id"`
	OwnerID   string `json:"owner_id"`
	Token     string `json:"token"`
	CreatedAt string `json:"created_at"`
}

type AdminPresenter struct{}

func (AdminPresenter) toIssuedKeyResponse(key apikeyservice.IssuedKey) issuedKeyResponse {
	return issuedKeyResponse{
		ID:        key.ID,
		OwnerID:   key.OwnerID,
		Token:     key.Token,
		CreatedAt: formatTimestamp(key.CreatedAt),
	}
}
