package clock

import "time"

type SystemClock struct{}

func New() SystemClock {
	return SystemClock{}
}

func (SystemClock) Now() time.Time {
	return time.Now().UTC().Truncate(time.Millisecond)
}
