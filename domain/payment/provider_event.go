package payment

const (
	EventChargePaid    EventKind = "charge_paid"
	EventChargeExpired EventKind = "charge_expired"
	EventPayoutDone    EventKind = "payout_done"
	EventPayoutFailed  EventKind = "payout_failed"
	EventIgnored       EventKind = "ignored"
)

type EventKind string

type ProviderEvent struct {
	Provider   string
	EventID    string
	ProviderID string
	Kind       EventKind
}
