package retry

import (
	"errors"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestExpoBackoff_StopAfterMaxRetry(t *testing.T) {
	expect := assert.New(t)
	backoff := &ExpBackoff{MaxRetry: 3, MinInterval: 10 * time.Millisecond}

	count := 0
	result, err := Retry(func() (interface{}, error) {
		count++
		return count, errors.New("any error")
	}, backoff)

	expect.True(errors.Is(err, EndOfRetry))
	expect.Nil(result)
	expect.Equal(4, count)
}

func TestExpoBackoff_StopAfterSuccess(t *testing.T) {
	expect := assert.New(t)
	backoff := &ExpBackoff{MaxRetry: 5, MinInterval: 100 * time.Millisecond}

	count := 0
	result, err := Retry(func() (interface{}, error) {
		count++
		return count, nil
	}, backoff)

	expect.Nil(err)
	expect.Equal(1, result)
}
