package main

import (
	"errors"
	"math/rand/v2"
)

const (
	scenarioUniform    = "uniform"
	scenarioContention = "contention"
	hotPercent         = 90
	hotDivisor         = 100
)

var errUnknownScenario = errors.New("unknown scenario")

type picker struct {
	random   *rand.Rand
	accounts int
	hot      int
}

func newPicker(scenario string, accounts int, seed uint64) (*picker, error) {
	chosen := &picker{random: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), accounts: accounts}
	switch scenario {
	case scenarioUniform:
		return chosen, nil
	case scenarioContention:
		chosen.hot = max(1, accounts/hotDivisor)
		return chosen, nil
	default:
		return nil, errUnknownScenario
	}
}

func (p *picker) next() (from, to int) {
	from = p.source()
	to = p.random.IntN(p.accounts - 1)
	if to >= from {
		to++
	}
	return from, to
}

func (p *picker) source() int {
	if p.hot > 0 && p.random.IntN(100) < hotPercent {
		return p.random.IntN(p.hot)
	}
	return p.random.IntN(p.accounts)
}
