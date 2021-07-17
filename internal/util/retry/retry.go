package retry

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

// Op should return a nil error when succeeded. Or signal a retry with a non-nil error other than EndOfRetry.
// EndOfRetry error is meant to be used with context cancellation which is common in go.
type Op func() (interface{}, error)

type BackoffStrategy interface {
	// Backoff returns an time.Duration to wait before next retry starts or a non-nil error to stop retry.
	// lastError comes from Op and the BackoffStrategy may / may not consider it as part of the strategy.
	Backoff(lastError error) (time.Duration, error)
}

// AsBackoff wraps a simple function into a BackoffStrategy
type AsBackoff func(lastError error) (time.Duration, error)

func (backoffFn AsBackoff) Backoff(lastError error) (time.Duration, error) {
	return backoffFn(lastError)
}

// Randomize add a random jitter in between retries to avoid retry storm.
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

var EndOfRetry = errors.New("end of retry")

// Retry until Op returns nil / EndOfRetry error, or BackoffStrategy returns any non-nil error
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


// ExpBackoff is a exponential backoff strategy with optional upper bounds like MaxRetry / MaxInterval
type ExpBackoff struct {
	MaxRetry    int
	MinInterval time.Duration
	MaxInterval time.Duration
	retryCount  int
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

func (b *ExpBackoff) Backoff(_ error) (time.Duration, error) {
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
