package retry

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

type Op func() (interface{}, error)

type BackoffStrategy interface {
	Backoff(err error) (time.Duration, error)
}

type AsBackoff func(err error) (time.Duration, error)

func Randomize(strategy BackoffStrategy, randomness time.Duration) BackoffStrategy {
	return AsBackoff(func(err error) (time.Duration, error) {
		backoff, err := strategy.Backoff(err)
		if err == nil {
			n := rand.Int63n(randomness.Nanoseconds())
			return time.Duration(n) + backoff, err
		}

		return backoff, err
	})
}

func (backoffFn AsBackoff) Backoff(err error) (time.Duration, error) {
	return backoffFn(err)
}

var EndOfRetry = errors.New("end of retry")

func Retry(doOp Op, strategy BackoffStrategy) (interface{}, error) {
	for {
		v, opErr := doOp()
		if opErr != nil {
			if errors.Is(opErr, EndOfRetry) {
				// ended
				return nil, EndOfRetry
			}
			// retry on any operation error
			duration, err := strategy.Backoff(opErr)
			if err != nil {
				// stop retry on any backoff error
				return nil, err
			}

			// retry after backoff
			time.Sleep(duration)
			continue
		}

		// operation success
		return v, nil
	}
}

type ExpBackoff struct {
	retryCount  int
	MaxRetry    int
	MinInterval time.Duration
	MaxInterval time.Duration
	init        sync.Once
}

func (b *ExpBackoff) setDefaults() {
	b.retryCount = 0
	if b.MaxInterval == 0 {
		b.MaxInterval = time.Duration(math.MaxInt64)
	}

	if b.MaxRetry == 0 {
		b.MaxRetry = math.MaxInt32
	}
}

func (b *ExpBackoff) Backoff(err error) (time.Duration, error) {
	b.init.Do(b.setDefaults)

	if b.retryCount >= b.MaxRetry {
		return 0, fmt.Errorf("maximum retries %d reached :%w", b.MaxRetry, EndOfRetry)
	}

	shift := 1 << b.retryCount
	interval := time.Duration(shift) * b.MinInterval
	b.retryCount++
	if interval > b.MaxInterval {
		return b.MaxInterval, nil
	}

	return interval, nil
}
