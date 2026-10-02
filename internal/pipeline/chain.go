package pipeline

import (
	"context"
	"fmt"
	"slop-generator/internal/stage"
)

type step struct {
	id    string
	run   func(context.Context, *stage.RunContext) (stage.Status, error)
	clear func()
}

// Chain retains typed connections while the executor sees only control flow.
type Chain[I, O, F any] struct {
	steps    []step
	result   func() O
	feedback func(*F)
	seed     func(I)
}

func Start[I, O, FI any](id string, s stage.Stage[I, O, FI, stage.Unit]) Chain[I, O, FI] {
	var input I
	var result O
	var feedback *FI
	run := func(ctx context.Context, shared *stage.RunContext) (stage.Status, error) {
		out, err := s.Run(ctx, shared, stage.Input[I, FI]{Result: input, Feedback: feedback})
		if err == nil && out.Status == stage.Failure && out.Feedback == nil {
			return out.Status, fmt.Errorf("stage %s failed without feedback", id)
		}
		if err == nil && out.Status == stage.Success {
			result = out.Result
		}
		return out.Status, err
	}
	return Chain[I, O, FI]{steps: []step{{id: id, run: run, clear: func() { var zero O; result = zero; feedback = nil }}}, result: func() O { return result }, feedback: func(f *FI) { feedback = f }, seed: func(i I) { input = i }}
}

func Then[I, O, N, FI, FN any](c Chain[I, O, FI], id string, s stage.Stage[O, N, FN, FI]) Chain[I, N, FN] {
	var input O
	var saved bool
	var result N
	var feedback *FN
	run := func(ctx context.Context, shared *stage.RunContext) (stage.Status, error) {
		if !saved {
			input = c.result()
			saved = true
		}
		out, err := s.Run(ctx, shared, stage.Input[O, FN]{Result: input, Feedback: feedback})
		if err != nil {
			return out.Status, err
		}
		switch out.Status {
		case stage.Success:
			result = out.Result
		case stage.Failure:
			if out.Feedback == nil {
				return out.Status, fmt.Errorf("stage %s failed without feedback", id)
			}
			c.feedback(out.Feedback)
		}
		return out.Status, nil
	}
	steps := append(append([]step(nil), c.steps...), step{id: id, run: run, clear: func() { saved = false; feedback = nil; var zero N; result = zero }})
	return Chain[I, N, FN]{steps: steps, result: func() N { return result }, feedback: func(f *FN) { feedback = f }, seed: c.seed}
}

func (c Chain[I, O, F]) Run(ctx context.Context, input I, maxRetries int, event func(string), complete func(int)) (O, error) {
	var zero O
	if maxRetries < 0 {
		return zero, fmt.Errorf("max_retries must not be negative")
	}
	if len(c.steps) == 0 {
		return zero, fmt.Errorf("pipeline has no stages")
	}
	for _, step := range c.steps {
		step.clear()
	}
	c.seed(input)
	shared := &stage.RunContext{}
	retries := make([]int, len(c.steps))
	for i := 0; i < len(c.steps); {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		if event != nil {
			event("Running " + c.steps[i].id)
		}
		status, err := c.steps[i].run(ctx, shared)
		if err != nil {
			return zero, fmt.Errorf("stage %s: %w", c.steps[i].id, err)
		}
		if status == stage.Success {
			i++
			if complete != nil {
				complete(i)
			}
			continue
		}
		if status != stage.Failure {
			return zero, fmt.Errorf("stage %s returned invalid status", c.steps[i].id)
		}
		if i == 0 {
			return zero, fmt.Errorf("first stage %s failed", c.steps[i].id)
		}
		if retries[i] >= maxRetries {
			return zero, fmt.Errorf("stage %s exhausted %d retries", c.steps[i].id, maxRetries)
		}
		retries[i]++
		if event != nil {
			event(fmt.Sprintf("Retrying %s after %s (%d/%d)", c.steps[i-1].id, c.steps[i].id, retries[i], maxRetries))
		}
		// Preserve the predecessor input and its freshly delivered feedback.
		for j := i; j < len(c.steps); j++ {
			c.steps[j].clear()
		}
		i--
		if complete != nil {
			complete(i)
		}
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return c.result(), nil
}
