package util

import (
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestExpoBackoff_MaxRetry(t *testing.T) {
	expect := assert.New(t)
	backoff := &ExpoBackoff{MaxRetry: 3, MinInterval: 10 * time.Millisecond}

	count := 0
	result, err := Retry(func() (interface{}, error) {
		count++
		return count, nil
	}, backoff)

	expect.Nil(err)
	expect.Equal(4, result)
}

func TestExpoBackoff_StopAfterSuccess(t *testing.T) {
	expect := assert.New(t)
	backoff := &ExpoBackoff{MaxRetry: 5, MinInterval: 100 * time.Millisecond}

	count := 0
	result, err := Retry(func() (interface{}, error) {
		count++
		return count, OpSuccess
	}, backoff)

	expect.Nil(err)
	expect.Equal(1, result)
}
