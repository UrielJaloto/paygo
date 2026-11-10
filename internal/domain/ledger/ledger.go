package ledger

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/money"
)

const (
	Debit  = "debit"
	Credit = "credit"

	minimumEntries = 2
)

var (
	ErrTooFewEntries         = errors.New("too few entries")
	ErrUnbalancedTransaction = errors.New("unbalanced transaction")
)

type Entry struct {
	id        string
	journalID string
	accountID account.ID
	direction string
	amount    money.Money
	createdAt time.Time
}

func NewEntry(id string, accountID account.ID, direction string, amount money.Money, createdAt time.Time) (Entry, error) {
	if amount.Minor() < 1 {
		return Entry{}, fmt.Errorf("%w: entry of %s", money.ErrInvalidAmount, amount)
	}
	return Entry{id: id, accountID: accountID, direction: direction, amount: amount, createdAt: createdAt}, nil
}

func (e Entry) ID() string {
	return e.id
}

func (e Entry) JournalID() string {
	return e.journalID
}

func (e Entry) AssignJournal(journalID string) Entry {
	e.journalID = journalID
	return e
}

func (e Entry) AccountID() account.ID {
	return e.accountID
}

func (e Entry) Direction() string {
	return e.direction
}

func (e Entry) Amount() money.Money {
	return e.amount
}

func (e Entry) CreatedAt() time.Time {
	return e.createdAt
}

func (e Entry) adjustBalance(balance money.Money) (money.Money, error) {
	if e.direction == Credit {
		return balance.Add(e.amount)
	}
	return balance.Subtract(e.amount)
}

type JournalTransaction struct {
	id      string
	entries []Entry
}

func NewJournalTransaction(id string, entries []Entry) (JournalTransaction, error) {
	if len(entries) < minimumEntries {
		return JournalTransaction{}, fmt.Errorf("%w: %d", ErrTooFewEntries, len(entries))
	}
	net, err := CalculateBalance(entries[0].amount.Currency(), entries)
	if err != nil {
		return JournalTransaction{}, err
	}
	if !net.IsZero() {
		return JournalTransaction{}, fmt.Errorf("%w: net %s", ErrUnbalancedTransaction, net)
	}
	stamped := slices.Clone(entries)
	for index := range stamped {
		stamped[index].journalID = id
	}
	return JournalTransaction{id: id, entries: stamped}, nil
}

func (j JournalTransaction) ID() string {
	return j.id
}

func (j JournalTransaction) Entries() []Entry {
	return slices.Clone(j.entries)
}

type Posting struct {
	ID       string
	EntryIDs [2]string
	From     account.ID
	To       account.ID
	Amount   money.Money
	At       time.Time
}

func (p Posting) BuildJournal() (JournalTransaction, error) {
	debit, err := NewEntry(p.EntryIDs[0], p.From, Debit, p.Amount, p.At)
	if err != nil {
		return JournalTransaction{}, err
	}
	credit := Entry{id: p.EntryIDs[1], accountID: p.To, direction: Credit, amount: p.Amount, createdAt: p.At}
	return NewJournalTransaction(p.ID, []Entry{debit, credit})
}

func CalculateBalance(currency money.Currency, entries []Entry) (money.Money, error) {
	balance := money.NewZero(currency)
	for _, entry := range entries {
		next, err := entry.adjustBalance(balance)
		if err != nil {
			return money.Money{}, err
		}
		balance = next
	}
	return balance, nil
}
