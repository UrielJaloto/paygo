package randomness

import (
	"math/rand/v2"
	"sync"
)

const (
	jitterLimit = 0.2
	streamSeed  = 0x9E3779B97F4A7C15
)

type Jitter struct {
	mutex  sync.Mutex
	source *rand.Rand
}

func NewJitter(seed uint64) *Jitter {
	return &Jitter{source: rand.New(rand.NewPCG(seed, streamSeed))}
}

func (j *Jitter) Next() float64 {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	return j.source.Float64()*2*jitterLimit - jitterLimit
}
