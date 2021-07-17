package util

import (
	"context"
	"fmt"
	"time"
)

type GraceContext struct {
	c    context.Context
	done chan struct{}
}

func (c GraceContext) Done() <-chan struct{} {
	return c.done
}

func (c GraceContext) Err() error {
	return fmt.Errorf("grace timeout exceeded: %w", context.DeadlineExceeded)
}

func (c GraceContext) Deadline() (deadline time.Time, ok bool) {
	return time.Time{}, false
}

func (c GraceContext) Value(key interface{}) interface{} {
	return nil
}

func WithGrace(parent context.Context, period time.Duration) context.Context {
	done := make(chan struct{})
	go func() {
		<-parent.Done()
		<-time.After(period)
		close(done)
	}()

	return GraceContext{
		done: done,
	}
}
