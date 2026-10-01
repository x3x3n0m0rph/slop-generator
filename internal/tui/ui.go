// Package tui presents tasks through a terminal user interface.
package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"slop-generator/internal/task"
)

type tickMsg struct{}
type actionMsg struct{ err error }
type closedMsg struct{}
type ui struct {
	engine                       Controller
	tasks                        []task.Task
	selected, scroll, listOffset int
	width, height                int
	creating                     bool
	stage, choice                int
	choices                      []string
	picked                       [3]string
	notice                       string
	closing                      bool
}

func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}
func (m ui) Init() tea.Cmd { return tick() }
func (m *ui) refresh() {
	var err error
	m.tasks, err = m.engine.Snapshot()
	if err != nil {
		m.notice = err.Error()
	}
	if m.selected >= len(m.tasks) {
		m.selected = max(0, len(m.tasks)-1)
	}
	m.keepSelectionVisible()
}
func (m *ui) pickChoices() {
	m.choice = 0
	switch m.stage {
	case 0:
		m.choices = m.engine.Profiles().Pipelines
	case 1:
		m.choices = m.engine.Profiles().Repositories
	case 2:
		m.choices = m.engine.Profiles().Providers
	}
}
func (m ui) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.keepSelectionVisible()
	case tickMsg:
		m.refresh()
		return m, tick()
	case actionMsg:
		if msg.err != nil {
			m.notice = msg.err.Error()
		} else {
			m.notice = "Task queued"
		}
		m.refresh()
	case closedMsg:
		return m, tea.Quit
	case tea.KeyPressMsg:
		key := msg.String()
		if m.closing {
			return m, nil
		}
		if key == "ctrl+c" || (key == "q" && !m.creating) {
			m.closing = true
			return m, func() tea.Msg { m.engine.Close(); return closedMsg{} }
		}
		if m.creating {
			switch key {
			case "esc":
				m.creating = false
			case "up", "k":
				m.choice = max(0, m.choice-1)
			case "down", "j":
				m.choice = min(len(m.choices)-1, m.choice+1)
			case "enter":
				m.picked[m.stage] = m.choices[m.choice]
				m.stage++
				if m.stage == 3 {
					m.creating = false
					picked := m.picked
					return m, func() tea.Msg { return actionMsg{m.engine.Enqueue(picked[0], picked[1], picked[2])} }
				}
				m.pickChoices()
			}
			return m, nil
		}
		switch key {
		case "n":
			m.creating = true
			m.stage = 0
			m.pickChoices()
			m.notice = ""
		case "up", "k":
			m.selected = max(0, m.selected-1)
			m.scroll = 0
		case "down", "j":
			m.selected = min(max(0, len(m.tasks)-1), m.selected+1)
			m.scroll = 0
		case "home":
			m.selected = 0
			m.scroll = 0
		case "end":
			m.selected = max(0, len(m.tasks)-1)
			m.scroll = 0
		case "pgup":
			m.scroll += 10
		case "pgdown":
			m.scroll = max(0, m.scroll-10)
		case "c":
			if len(m.tasks) > 0 {
				m.engine.Cancel(m.tasks[m.selected].ID)
			}
		case "r":
			if len(m.tasks) > 0 {
				t := m.tasks[m.selected]
				return m, func() tea.Msg { return actionMsg{m.engine.Enqueue(t.PipelineName, t.RepoName, t.ProviderName)} }
			}
		case "p":
			if len(m.tasks) > 0 {
				id := m.tasks[m.selected].ID
				return m, func() tea.Msg { return actionMsg{m.engine.RetryPublish(id)} }
			}
		}
	}
	m.keepSelectionVisible()
	return m, nil
}

// Controller is the application capability consumed by the terminal UI.
type Controller interface {
	Snapshot() ([]task.Task, error)
	Profiles() task.Profiles
	Enqueue(pipeline, repository, provider string) error
	Cancel(id string)
	RetryPublish(id string) error
	Close()
}

func Run(controller Controller) error {
	m := ui{engine: controller, width: 100, height: 35}
	m.refresh()
	_, err := tea.NewProgram(m).Run()
	return err
}
