package memory

import "github.com/UrielJaloto/paygo/domain/account"

func (s *Store) CorruptBalance(id account.ID, balance int64) {
	s.data.Lock()
	defer s.data.Unlock()
	s.accounts[id].balance = balance
}
