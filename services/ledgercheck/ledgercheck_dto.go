package ledgercheck

type CheckOutput struct {
	OK                   bool
	Accounts             int
	Mismatches           []string
	UnbalancedCurrencies []string
}
