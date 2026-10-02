package tui

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"slop-generator/internal/form"
	"slop-generator/internal/task"
	"strings"
	"testing"
)

func TestStageWizardValidationPasteAndRetry(t *testing.T) {
	c := &stubController{fields: []form.Field{{ID: "generate.filename", Label: "Filename", Kind: form.Text, Required: true}, {ID: "generate.instruction", Label: "Instruction", Kind: form.Multiline, Required: true}}}
	m := ui{engine: c, width: 100, height: 20}
	m, _ = press(t, m, 'n')
	for i := 0; i < 3; i++ {
		m, _ = press(t, m, tea.KeyEnter)
	}
	m, cmd := press(t, m, tea.KeyEnter)
	if cmd != nil || m.fieldIndex != 0 || m.notice == "" {
		t.Fatal("empty filename accepted")
	}
	next, _ := m.Update(tea.PasteMsg{Content: "file.txt"})
	m = next.(ui)
	m, _ = press(t, m, tea.KeyEnter)
	next, _ = m.Update(tea.PasteMsg{Content: "Write a note"})
	m = next.(ui)
	m, _ = press(t, m, tea.KeyEnter)
	if m.value != "Write a note\n" {
		t.Fatal("multiline enter submitted")
	}
	c.enqueueError = errors.New("temporarily unavailable")
	next, cmd = m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m = next.(ui)
	if cmd == nil || m.creating {
		t.Fatal("form not submitted")
	}
	next, _ = m.Update(cmd())
	m = next.(ui)
	if !m.creating || m.value != "Write a note\n" {
		t.Fatal("failed submission lost answers")
	}
	c.enqueueError = nil
	next, cmd = m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m = next.(ui)
	if cmd == nil {
		t.Fatal("retry not submitted")
	}
	m.Update(cmd())
	if c.answers["generate.filename"] != "file.txt" || c.answers["generate.instruction"] != "Write a note\n" {
		t.Fatal("incorrect answers")
	}
}
func TestFormCancelAndEmptyProfiles(t *testing.T) {
	c := &stubController{}
	m := ui{engine: c, creating: true, choices: nil}
	m, cmd := press(t, m, tea.KeyEnter)
	if cmd != nil || m.notice == "" {
		t.Fatal("empty profiles accepted")
	}
	m, _ = press(t, m, tea.KeyEscape)
	if m.creating {
		t.Fatal("wizard not cancelled")
	}
}
func TestRerunUsesTaskIDAndLongFormFits(t *testing.T) {
	c := &stubController{}
	m := ui{engine: c, tasks: []task.Task{{ID: "saved"}}, width: 80, height: 15}
	_, cmd := press(t, m, 'r')
	cmd()
	if c.rerunID != "saved" {
		t.Fatal("rerun did not use snapshot task")
	}
	m.creating = true
	m.stage = 3
	m.fields = []form.Field{{ID: "instruction", Label: "Instruction", Kind: form.Multiline}}
	m.value = strings.Repeat("long instruction\n", 40)
	assertScreenBounds(t, m)
	if !strings.Contains(m.View().Content, "▌") {
		t.Fatal("long form hid cursor")
	}
}
