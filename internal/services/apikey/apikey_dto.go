package apikey

import "time"

type IssueInput struct {
	OwnerID string
}

type IssuedKey struct {
	ID        string
	OwnerID   string
	Token     string
	CreatedAt time.Time
}

type Principal struct {
	OwnerID string
}

type RevokeInput struct {
	KeyID string
}
