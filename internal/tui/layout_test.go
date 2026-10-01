package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"slop-generator/internal/task"
)

func screenTasks(n int) []task.Task {
	tasks := make([]task.Task, n)
	for i := range tasks {
		tasks[i] = task.Task{ID: fmt.Sprintf("task-id-%06d", i), PipelineName: fmt.Sprintf("pipeline-%02d", i), RepoName: "playground", ProviderName: "provider", Status: "running", Step: "Generating source", Created: time.Now(), Events: []string{"first event", "latest event"}}
	}
	return tasks
}
func assertScreenBounds(t *testing.T, m ui) {
	t.Helper()
	lines := strings.Split(m.View().Content, "\n")
	if len(lines) != m.height {
		t.Fatalf("got %d rows, terminal has %d", len(lines), m.height)
	}
	for i, line := range lines {
		if ansi.StringWidth(line) != m.width {
			t.Fatalf("row %d width %d, expected %d: %q", i, ansi.StringWidth(line), m.width, line)
		}
	}
}
func TestTwoPaneLayoutUsesFullHeight(t *testing.T) {
	m := ui{engine: &stubController{}, tasks: screenTasks(40), width: 120, height: 30}
	m.keepSelectionVisible()
	assertScreenBounds(t, m)
	lines := strings.Split(m.View().Content, "\n")
	for row := 0; row < m.height-5; row++ {
		panes := strings.Split(lines[row+2], "│")
		if len(panes) != 4 || !strings.Contains(panes[1], fmt.Sprintf("pipeline-%02d", row)) {
			t.Fatalf("task row %d missing from sidebar: %s", row, lines[row+2])
		}
	}
	if !strings.Contains(m.View().Content, "Generating source") || !strings.Contains(m.View().Content, "latest event") {
		t.Fatal("right pane missing details or events")
	}
}
func TestTaskSelectionRemainsVisibleAfterResize(t *testing.T) {
	m := ui{engine: &stubController{}, tasks: screenTasks(50), width: 120, height: 30}
	m, _ = press(t, m, tea.KeyEnd)
	if m.selected != 49 || m.listOffset != 25 {
		t.Fatalf("selection did not scroll: selected=%d offset=%d", m.selected, m.listOffset)
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 15})
	m = next.(ui)
	assertScreenBounds(t, m)
	if m.listOffset != 40 {
		t.Fatalf("resize hid selected task: offset=%d", m.listOffset)
	}
	m, _ = press(t, m, tea.KeyHome)
	if m.selected != 0 || m.listOffset != 0 {
		t.Fatal("home did not restore first task")
	}
}
func TestUnicodeAndSmallScreensStayWithinBounds(t *testing.T) {
	for _, size := range [][2]int{{120, 32}, {80, 24}, {40, 12}, {12, 6}, {5, 4}} {
		m := ui{engine: &stubController{}, tasks: screenTasks(1), width: size[0], height: size[1]}
		m.tasks[0].PipelineName = "Текст 中文 🧠"
		m.tasks[0].Step = strings.Repeat("Генерация 中文 ", 30)
		m.tasks[0].Error = strings.Repeat("Ошибка проверки ", 10)
		m.tasks[0].Work = `C:\Users\example\AppData\Roaming\slop-generator\work\long-task-id`
		assertScreenBounds(t, m)
		m.creating = true
		m.choices = []string{"Текст 中文 🧠"}
		assertScreenBounds(t, m)
	}
}
func TestRightPaneEventScrolling(t *testing.T) {
	m := ui{engine: &stubController{}, tasks: screenTasks(1), width: 120, height: 30}
	m.tasks[0].Events = nil
	for i := 0; i < 50; i++ {
		m.tasks[0].Events = append(m.tasks[0].Events, fmt.Sprintf("event-%02d", i))
	}
	latest := strings.Join(m.details(70, 25), "\n")
	if !strings.Contains(latest, "event-49") {
		t.Fatal("events did not follow latest output")
	}
	m, _ = press(t, m, tea.KeyPgUp)
	earlier := strings.Join(m.details(70, 25), "\n")
	if strings.Contains(earlier, "event-49") || !strings.Contains(earlier, "event-39") {
		t.Fatal("page up did not reveal earlier events")
	}
	m, _ = press(t, m, tea.KeyPgDown)
	if !strings.Contains(strings.Join(m.details(70, 25), "\n"), "event-49") {
		t.Fatal("page down did not return to latest output")
	}
}
