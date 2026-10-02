// Package stage defines typed, UI-independent pipeline steps.
package stage

import "context"

type Unit struct{}
type Status uint8

const (
	Success Status = iota + 1
	Failure
)

type Input[R, F any] struct {
	Result   R
	Feedback *F
}
type Output[R, F any] struct {
	Status   Status
	Result   R
	Feedback *F
}

// RunContext is shared sequentially within one run, never between tasks.
type RunContext struct{ reserved byte }

// Stage inputs are immutable. Side effects must tolerate repeated execution.
type Stage[I, O, FIn, FOut any] interface {
	Run(context.Context, *RunContext, Input[I, FIn]) (Output[O, FOut], error)
}
