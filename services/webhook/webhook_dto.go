package webhook

import "time"

type RegisterInput struct {
	OwnerID string
	URL     string
}

type EndpointOutput struct {
	ID        string
	URL       string
	IsActive  bool
	CreatedAt time.Time
}

type RegisteredEndpoint struct {
	EndpointOutput
	Secret string
}

type EndpointQuery struct {
	OwnerID    string
	EndpointID string
}

type DeliveryListInput struct {
	OwnerID string
	Status  string
	Cursor  string
	Limit   string
}

type DeliveryOutput struct {
	ID            string
	EventID       string
	EndpointID    string
	Status        string
	Attempts      int
	NextAttemptAt time.Time
	LastError     string
}

type DeliveryPage struct {
	Items      []DeliveryOutput
	NextCursor string
}

type DeliveryQuery struct {
	OwnerID    string
	DeliveryID string
}
