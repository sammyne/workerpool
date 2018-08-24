package workerpool

import (
	"context"
	"errors"
	"runtime"
)

type poolAction struct {
	ctx      context.Context
	action   Action
	response chan<- error
}

type pool struct {
	done <-chan struct{}
	in   chan poolAction
}

// Execute enqueues all Actions on the worker pool, failing closed on the
// first error or if ctx is cancelled. This method blocks until all enqueued
// Actions have returned. In the event of an error, not all Actions may be
// executed.
func (p pool) Execute(ctx context.Context, actions []Action) error {
	qty := len(actions)
	if qty == 0 {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	res := make(chan error, qty)

	var err error
	var queued uint64

enqueue:
	for _, action := range actions {
		pa := poolAction{ctx: ctx, action: action, response: res}
		select {
		case <-p.done: // pool is closed
			cancel()
			return errors.New("pool is closed")
		case <-ctx.Done(): // ctx is closed by caller
			err = ctx.Err()
			break enqueue
		case p.in <- pa: // enqueue action
			queued++
		}
	}

	for ; queued > 0; queued-- {
		if r := <-res; r != nil {
			if err == nil {
				err = r
				cancel()
			}
		}
	}

	return err
}

func (p pool) work(in <-chan poolAction, done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case a := <-in:
			a.response <- a.action.Execute(a.ctx)
		}
	}
}

// Pool creates an Executor backed by a concurrent worker pool. Up to n Actions
// can be in-flight simultaneously; if n is less than or equal to zero,
// runtime.NumCPU is used. The done channel should be closed to release
// resources held by the Executor.
func Pool(n int, done <-chan struct{}) Executor {
	if n <= 0 {
		n = runtime.NumCPU()
	}

	p := pool{done: done, in: make(chan poolAction, n)}

	for i := 0; i < n; i++ {
		go p.work(p.in, p.done)
	}

	return p
}
