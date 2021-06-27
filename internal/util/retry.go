package util

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

type Op func() (interface{}, error)

type BackoffStrategy interface {
	Backoff() (time.Duration, error)
}

type SimpleBackoff func() (time.Duration, error)

func (backoffFn SimpleBackoff) Backoff() (time.Duration, error) {
	return backoffFn()
}

var EOR = errors.New("end of retry")
var OpSuccess = fmt.Errorf("operation successful. %w", EOR)

func Retry(doOp Op, strategy BackoffStrategy) (interface{}, error) {
	for {
		v, err := doOp()
		if err != nil {
			if errors.Is(err, EOR) {
				return v, nil
			} else {
				return v, err
			}
		}

		duration, err := strategy.Backoff()
		if err != nil {
			if errors.Is(err, EOR) {
				return v, nil
			} else {
				return v, err
			}
		}

		time.Sleep(duration)
	}
}

type ExpoBackoff struct {
	retryCount  int
	MaxRetry    int
	MinInterval time.Duration
	MaxInterval time.Duration
	init        sync.Once
}

func (b *ExpoBackoff) setDefaults() {
	b.retryCount = 0
	if b.MaxInterval == 0 {
		b.MaxInterval = time.Duration(math.MaxInt64)
	}

	if b.MaxRetry == 0 {
		b.MaxRetry = math.MaxInt32
	}
}

func (b *ExpoBackoff) Backoff() (time.Duration, error) {
	b.init.Do(b.setDefaults)

	if b.retryCount >= b.MaxRetry {
		return 0, fmt.Errorf("maximum retries %d reached :%w", b.MaxRetry, EOR)
	}

	shift := 1 << b.retryCount
	interval := time.Duration(shift) * b.MinInterval
	b.retryCount++
	if interval > b.MaxInterval {
		return b.MaxInterval, nil
	}

	return interval, nil
}
