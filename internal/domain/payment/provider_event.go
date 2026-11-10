package payment

const (
	EventChargePaid    = "charge_paid"
	EventChargeExpired = "charge_expired"
	EventPayoutDone    = "payout_done"
	EventPayoutFailed  = "payout_failed"
	EventIgnored       = "ignored"
)

type ProviderEvent struct {
	Provider   string
	EventID    string
	ProviderID string
	Kind       string
}
