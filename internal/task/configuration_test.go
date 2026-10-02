package task

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slop-generator/internal/config"
	"slop-generator/internal/form"
	"slop-generator/internal/inference"
	"slop-generator/internal/pipeline"
	"slop-generator/internal/stage"
	"strings"
	"testing"
)

type failAfterCommit struct{ testDefinition }

func (f failAfterCommit) StageCount(c config.Pipeline) int { return f.testDefinition.StageCount(c) + 1 }
func (f failAfterCommit) Build(p config.Pipeline, r pipeline.Resources) (pipeline.Chain[stage.Artifacts, stage.Artifacts, stage.Unit], error) {
	c, err := f.testDefinition.Build(p, r)
	if err != nil {
		return c, err
	}
	return pipeline.Then(c, "after-commit", fatalStage{}), nil
}

type fatalStage struct{}

func (fatalStage) Run(context.Context, *stage.RunContext, stage.Input[stage.Artifacts, stage.Unit]) (stage.Output[stage.Artifacts, stage.Unit], error) {
	return stage.Output[stage.Artifacts, stage.Unit]{}, errors.New("stage after commit failed")
}
func TestIntermediateCommitDoesNotEnablePublication(t *testing.T) {
	r, remote := gitFixture(t)
	initial := mustGit(t, remote, "rev-parse", "main")
	p := testPipeline("text", "one", "write", ".txt")
	p.Config.CommitMode = "stages"
	e := setupEngine(t, r, p, func(w http.ResponseWriter, _ *http.Request) { answer(w, "text") })
	e.deps.Pipeline.(*pipeline.Engine).Definitions[p.Definition] = failAfterCommit{testDefinition{ids: []string{"one"}}}
	result := waitTask(t, e, enqueue(t, e))
	if result.Status != "failed" || result.SHA != "" || len(result.Commits) != 1 || result.Published || mustGit(t, remote, "rev-parse", "main") != initial {
		t.Fatalf("%+v", result)
	}
	if err := e.RetryPublish(result.ID); err == nil {
		t.Fatal("partial chain enabled publication")
	}
}

func TestMissingConfigurationAndRerunSnapshot(t *testing.T) {
	r, _ := gitFixture(t)
	p := testPipeline("text", "one", "write original", ".txt")
	delete(p.Stages["one.generate"], "instruction")
	e := setupEngine(t, r, p, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string
			Messages []inference.Message
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "test" || body.Messages[1].Content != "write original" {
			t.Errorf("saved settings changed: %+v", body)
		}
		answer(w, "text")
	})
	fields, err := e.Fields("test")
	if err != nil || len(fields) != 1 {
		t.Fatalf("fields=%v err=%v", fields, err)
	}
	if err = e.Enqueue("test", "test", "test"); err == nil {
		t.Fatal("incomplete task queued")
	}
	if err = e.EnqueueConfigured("test", "test", "test", form.Answers{"one.generate.filename": "overridden.txt"}); err == nil {
		t.Fatal("YAML setting overridden")
	}
	answers := form.Answers{"one.generate.instruction": "write original"}
	if err = e.EnqueueConfigured("test", "test", "test", answers); err != nil {
		t.Fatal(err)
	}
	answers["one.generate.instruction"] = "mutated"
	tasks, _ := e.Snapshot()
	original := waitTask(t, e, tasks[0].ID)
	if original.Status != "succeeded" {
		t.Fatalf("%+v", original)
	}
	e.cfg.Pipelines["test"] = testPipeline("text", "one", "changed", ".txt")
	provider := e.cfg.Providers["test"]
	provider.Model = "changed"
	e.cfg.Providers["test"] = provider
	if err = e.Rerun(original.ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ = e.Snapshot()
	rerun := waitTask(t, e, tasks[1].ID)
	if rerun.Status != "succeeded" || rerun.Provider.Model != "test" || rerun.Pipeline.Stages["one.generate"]["instruction"] != "write original" {
		t.Fatalf("%+v", rerun)
	}
}

func TestStageCommitMode(t *testing.T) {
	for _, commitStage := range []bool{true, false} {
		t.Run(map[bool]string{true: "commit stage", false: "missing commit"}[commitStage], func(t *testing.T) {
			r, remote := gitFixture(t)
			initial := mustGit(t, remote, "rev-parse", "main")
			p := testPipeline("text", "one", "write", ".txt")
			p.Config.CommitMode = "stages"
			e := setupEngine(t, r, p, func(w http.ResponseWriter, _ *http.Request) { answer(w, "text") })
			definition := testDefinition{ids: []string{"one"}}
			if !commitStage {
				e.deps.Pipeline.(*pipeline.Engine).Definitions[p.Definition] = failAfterCommit{definition}
			} else {
				e.deps.Pipeline.(*pipeline.Engine).Definitions[p.Definition] = definition
			}
			result := waitTask(t, e, enqueue(t, e))
			if commitStage {
				if result.Status != "succeeded" || len(result.Commits) != 1 || result.SHA != result.Commits[0] || mustGit(t, remote, "rev-parse", "main") != result.SHA {
					t.Fatalf("%+v", result)
				}
			} else {
				if result.Status != "failed" || result.SHA != "" || len(result.Commits) != 1 || !strings.Contains(result.Error, "stage after commit failed") || mustGit(t, remote, "rev-parse", "main") != initial {
					t.Fatalf("%+v", result)
				}
			}
		})
	}
}
