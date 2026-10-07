package tui

import (
	"slop-generator/internal/form"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"slop-generator/internal/task"
)

type stubController struct {
	selection    [3]string
	closed       bool
	fields       []form.Field
	answers      form.Answers
	enqueueError error
	rerunID      string
}

func (*stubController) Snapshot() ([]task.Task, error) { return nil, nil }
func (*stubController) Profiles() task.Profiles {
	return task.Profiles{Pipelines: []string{"python"}, Repositories: []string{"repo"}, Providers: []string{"provider"}}
}
func (c *stubController) EnqueueConfigured(p, r, v string, answers form.Answers) error {
	c.selection = [3]string{p, r, v}
	c.answers = answers
	return c.enqueueError
}
func (*stubController) Cancel(string)                          {}
func (*stubController) RetryPublish(string) error              { return nil }
func (*stubController) DeleteOlderThan(time.Time) (int, error) { return 0, nil }
func (c *stubController) Close()                               { c.closed = true }

func press(t *testing.T, m ui, code rune) (ui, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(tea.KeyPressMsg{Code: code})
	return next.(ui), cmd
}
func TestWizardUsesControllerInterface(t *testing.T) {
	controller := &stubController{}
	m := ui{engine: controller, height: 30}
	m.refresh()
	m, _ = press(t, m, 'n')
	if !m.creating || m.choices[0] != "python" {
		t.Fatal("wizard did not read profile names")
	}
	m, _ = press(t, m, tea.KeyEnter)
	m, _ = press(t, m, tea.KeyEnter)
	m, cmd := press(t, m, tea.KeyEnter)
	if cmd == nil {
		t.Fatal("wizard did not enqueue a task")
	}
	m.Update(cmd())
	if controller.selection != [3]string{"python", "repo", "provider"} {
		t.Fatal("incorrect selection")
	}
	m, cmd = press(t, m, 'q')
	if !m.closing || cmd == nil {
		t.Fatal("quit did not close controller")
	}
	cmd()
	if !controller.closed {
		t.Fatal("controller not closed")
	}
}

func (c *stubController) Fields(string) ([]form.Field, error) { return c.fields, nil }
func (c *stubController) Rerun(id string) error               { c.rerunID = id; return nil }
