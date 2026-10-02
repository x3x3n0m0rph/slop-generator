package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slop-generator/internal/stage"
	"strings"
	"testing"
)

type stageFunc[I, O, FI, FO any] func(context.Context, *stage.RunContext, stage.Input[I, FI]) (stage.Output[O, FO], error)

func (f stageFunc[I, O, FI, FO]) Run(c context.Context, r *stage.RunContext, i stage.Input[I, FI]) (stage.Output[O, FO], error) {
	return f(c, r, i)
}

type repair struct{ Delta int }

func TestTypedRetryPreservesInputAndEarlierStages(t *testing.T) {
	firstRuns, genRuns, checkRuns := 0, 0, 0
	var shared *stage.RunContext
	first := stageFunc[stage.Unit, int, stage.Unit, stage.Unit](func(_ context.Context, r *stage.RunContext, _ stage.Input[stage.Unit, stage.Unit]) (stage.Output[int, stage.Unit], error) {
		firstRuns++
		shared = r
		return stage.Output[int, stage.Unit]{Status: stage.Success, Result: 7}, nil
	})
	gen := stageFunc[int, string, repair, stage.Unit](func(_ context.Context, r *stage.RunContext, in stage.Input[int, repair]) (stage.Output[string, stage.Unit], error) {
		genRuns++
		if in.Result != 7 || r != shared {
			t.Fatal("input or shared context changed")
		}
		v := in.Result
		if in.Feedback != nil {
			v += in.Feedback.Delta
		}
		return stage.Output[string, stage.Unit]{Status: stage.Success, Result: fmt.Sprint(v)}, nil
	})
	check := stageFunc[string, bool, stage.Unit, repair](func(_ context.Context, _ *stage.RunContext, in stage.Input[string, stage.Unit]) (stage.Output[bool, repair], error) {
		checkRuns++
		if in.Feedback != nil {
			t.Fatal("happy path feedback populated")
		}
		if in.Result == "7" {
			return stage.Output[bool, repair]{Status: stage.Failure, Feedback: &repair{Delta: 2}}, nil
		}
		return stage.Output[bool, repair]{Status: stage.Success, Result: in.Result == "9"}, nil
	})
	chain := Then(Then(Start[stage.Unit, int, stage.Unit]("first", first), "generate", gen), "check", check)
	result, err := chain.Run(context.Background(), stage.Unit{}, 3, nil, nil)
	if err != nil || !result || firstRuns != 1 || genRuns != 2 || checkRuns != 2 {
		t.Fatalf("result=%v err=%v runs=%d/%d/%d", result, err, firstRuns, genRuns, checkRuns)
	}
}
func TestChainFailurePaths(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   stage.Status
		err      error
		feedback bool
		limit    int
		calls    int
	}{
		{"exhausted", stage.Failure, nil, true, 3, 4}, {"zero", stage.Failure, nil, true, 0, 1}, {"missing feedback", stage.Failure, nil, false, 3, 1}, {"technical error", stage.Success, errors.New("offline"), false, 3, 1}, {"invalid status", 0, nil, false, 3, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			gen := stageFunc[int, string, int, stage.Unit](func(_ context.Context, _ *stage.RunContext, in stage.Input[int, int]) (stage.Output[string, stage.Unit], error) {
				calls++
				return stage.Output[string, stage.Unit]{Status: stage.Success, Result: "value"}, nil
			})
			check := stageFunc[string, int, stage.Unit, int](func(context.Context, *stage.RunContext, stage.Input[string, stage.Unit]) (stage.Output[int, int], error) {
				out := stage.Output[int, int]{Status: tc.status}
				if tc.feedback {
					x := 1
					out.Feedback = &x
				}
				return out, tc.err
			})
			_, err := Then(Start[int, string, int]("gen", gen), "check", check).Run(context.Background(), 1, tc.limit, nil, nil)
			if err == nil || calls != tc.calls {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}
func TestFirstFailureAndCancellation(t *testing.T) {
	calls := 0
	s := stageFunc[int, int, stage.Unit, stage.Unit](func(context.Context, *stage.RunContext, stage.Input[int, stage.Unit]) (stage.Output[int, stage.Unit], error) {
		calls++
		return stage.Output[int, stage.Unit]{Status: stage.Failure, Feedback: &stage.Unit{}}, nil
	})
	chain := Start[int, int, stage.Unit]("first", s)
	if _, err := chain.Run(context.Background(), 1, 3, nil, nil); err == nil || calls != 1 {
		t.Fatal("first stage retried")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := chain.Run(ctx, 1, 3, nil, nil); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("cancelled stage executed")
	}
}
func TestIncompatibleConnectionsFailCompilation(t *testing.T) {
	for _, tc := range []struct{ name, input, feedback string }{{"result", "bool", "int"}, {"feedback", "string", "bool"}} {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := os.MkdirTemp(".", "compile-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			code := fmt.Sprintf(`package fixture
 import("context";"slop-generator/internal/pipeline";"slop-generator/internal/stage")
 type first struct{}
 func(first)Run(context.Context,*stage.RunContext,stage.Input[int,int])(stage.Output[string,stage.Unit],error){return stage.Output[string,stage.Unit]{},nil}
 type next struct{}
 func(next)Run(context.Context,*stage.RunContext,stage.Input[%s,stage.Unit])(stage.Output[int,%s],error){return stage.Output[int,%s]{},nil}
 var _=pipeline.Then(pipeline.Start[int,string,int]("first",first{}),"next",next{})
 `, tc.input, tc.feedback, tc.feedback)
			if err = os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(code), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("go", "test", "./"+filepath.Base(dir))
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), "does not match") {
				t.Fatalf("expected type mismatch: %v\n%s", err, out)
			}
		})
	}
}
