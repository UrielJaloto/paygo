package clock

import "time"

type SystemClock struct{}

func NewMoney() SystemClock {
	return SystemClock{}
}

func (SystemClock) Now() time.Time {
	return time.Now().UTC().Truncate(time.Millisecond)
}
