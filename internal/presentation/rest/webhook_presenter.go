package rest

import webhookservice "github.com/UrielJaloto/paygo/internal/services/webhook"

type endpointResponse struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	IsActive  bool   `json:"is_active"`
	CreatedAt string `json:"created_at"`
}

type registeredEndpointResponse struct {
	endpointResponse
	Secret string `json:"secret"`
}

type endpointListResponse struct {
	Items []endpointResponse `json:"items"`
}

type deliveryResponse struct {
	ID            string `json:"id"`
	EventID       string `json:"event_id"`
	EndpointID    string `json:"endpoint_id"`
	Status        string `json:"status"`
	Attempts      int    `json:"attempts"`
	NextAttemptAt string `json:"next_attempt_at"`
	LastError     string `json:"last_error"`
}

type deliveryPageResponse struct {
	Items      []deliveryResponse `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

type WebhookPresenter struct{}

func (WebhookPresenter) endpoint(output webhookservice.EndpointOutput) endpointResponse {
	return endpointResponse{
		ID:        output.ID,
		URL:       output.URL,
		IsActive:  output.IsActive,
		CreatedAt: formatTimestamp(output.CreatedAt),
	}
}

func (p WebhookPresenter) registered(output webhookservice.RegisteredEndpoint) registeredEndpointResponse {
	return registeredEndpointResponse{endpointResponse: p.endpoint(output.EndpointOutput), Secret: output.Secret}
}

func (p WebhookPresenter) endpointList(outputs []webhookservice.EndpointOutput) endpointListResponse {
	items := make([]endpointResponse, 0, len(outputs))
	for _, output := range outputs {
		items = append(items, p.endpoint(output))
	}
	return endpointListResponse{Items: items}
}

func (WebhookPresenter) delivery(output webhookservice.DeliveryOutput) deliveryResponse {
	return deliveryResponse{
		ID:            output.ID,
		EventID:       output.EventID,
		EndpointID:    output.EndpointID,
		Status:        output.Status,
		Attempts:      output.Attempts,
		NextAttemptAt: formatTimestamp(output.NextAttemptAt),
		LastError:     output.LastError,
	}
}

func (p WebhookPresenter) deliveryPage(page webhookservice.DeliveryPage) deliveryPageResponse {
	items := make([]deliveryResponse, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, p.delivery(item))
	}
	return deliveryPageResponse{Items: items, NextCursor: page.NextCursor}
}
