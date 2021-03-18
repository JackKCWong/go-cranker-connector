package util

import (
	"context"
	"time"
)

func WithGrace(parent context.Context, period time.Duration) context.Context {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		<-parent.Done()
		<-time.After(period)
		cancel()
	}()

	return ctx
}
