// Package tui presents tasks through a terminal user interface.
package tui

import (
	"fmt"
	"strings"
	"time"

	"slop-generator/internal/form"

	tea "charm.land/bubbletea/v2"

	"slop-generator/internal/task"
)

type tickMsg struct{}
type actionMsg struct{ err error }
type enqueueMsg struct{ err error }
type cleanupMsg struct {
	removed int
	err     error
}
type closedMsg struct{}

var cleanupAges = []time.Duration{24 * time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour, 90 * 24 * time.Hour}

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
	cleaning                     bool
	cleanupChoice                int
	fields                       []form.Field
	answers                      form.Answers
	fieldIndex                   int
	value                        string
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
	case tea.PasteMsg:
		if m.creating && m.stage == 3 {
			value := strings.ReplaceAll(msg.Content, "\r\n", "\n")
			if m.fields[m.fieldIndex].Kind != form.Multiline {
				value = strings.ReplaceAll(value, "\n", " ")
			}
			m.value += value
		}
	case enqueueMsg:
		if msg.err != nil {
			m.notice = msg.err.Error()
			if len(m.fields) > 0 {
				m.creating = true
				m.stage = 3
				m.fieldIndex = len(m.fields) - 1
				m.value = m.answers[m.fields[m.fieldIndex].ID]
			}
		} else {
			m.notice = "Task queued"
		}
		m.refresh()
	case cleanupMsg:
		m.cleaning = false
		if msg.err != nil {
			m.notice = msg.err.Error()
		} else {
			m.notice = fmt.Sprintf("Removed %d old task(s)", msg.removed)
		}
		m.refresh()
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
			if m.stage == 3 {
				switch key {
				case "shift+tab":
					if m.fieldIndex > 0 {
						m.answers[m.fields[m.fieldIndex].ID] = m.value
						m.fieldIndex--
						m.value = m.answers[m.fields[m.fieldIndex].ID]
					}
				case "up", "down", "left", "right", "space":
					field := m.fields[m.fieldIndex]
					if field.Kind == form.Boolean {
						if m.value == "true" {
							m.value = "false"
						} else {
							m.value = "true"
						}
					} else if field.Kind == form.Choice && len(field.Options) > 0 {
						index := 0
						for i, v := range field.Options {
							if v == m.value {
								index = i
								break
							}
						}
						delta := 1
						if key == "up" || key == "left" {
							delta = -1
						}
						m.value = field.Options[(index+delta+len(field.Options))%len(field.Options)]
					} else if key == "space" {
						m.value += " "
					}
				case "esc":
					m.creating = false
				case "backspace":
					r := []rune(m.value)
					if len(r) > 0 {
						m.value = string(r[:len(r)-1])
					}
				case "enter", "ctrl+enter", "ctrl+s":
					field := m.fields[m.fieldIndex]
					if field.Kind == form.Multiline && key == "enter" {
						m.value += "\n"
						return m, nil
					}
					if err := field.Validate(m.value); err != nil {
						m.notice = err.Error()
						return m, nil
					}
					m.answers[field.ID] = m.value
					m.fieldIndex++
					if m.fieldIndex == len(m.fields) {
						m.creating = false
						picked := m.picked
						answers := m.answers
						return m, func() tea.Msg {
							return enqueueMsg{m.engine.EnqueueConfigured(picked[0], picked[1], picked[2], answers)}
						}
					}
					m.value = m.fields[m.fieldIndex].Value
					if v, ok := m.answers[m.fields[m.fieldIndex].ID]; ok {
						m.value = v
					}
				default:
					if msg.Text != "" {
						m.value += msg.Text
					} else if key == "space" {
						m.value += " "
					}
				}
				return m, nil
			}
			switch key {
			case "esc":
				m.creating = false
			case "up", "k":
				m.choice = max(0, m.choice-1)
			case "down", "j":
				m.choice = min(len(m.choices)-1, m.choice+1)
			case "enter":
				if len(m.choices) == 0 {
					m.notice = "No profiles available"
					return m, nil
				}
				m.picked[m.stage] = m.choices[m.choice]
				m.stage++
				if m.stage == 3 {
					fields, err := m.engine.Fields(m.picked[0])
					if err != nil {
						m.notice = err.Error()
						m.creating = false
						return m, nil
					}
					m.fields = fields
					m.answers = form.Answers{}
					m.fieldIndex = 0
					if len(fields) != 0 {
						m.value = fields[0].Value
						return m, nil
					}
					m.creating = false
					picked := m.picked
					return m, func() tea.Msg { return enqueueMsg{m.engine.EnqueueConfigured(picked[0], picked[1], picked[2], nil)} }
				}
				m.pickChoices()
			}
			return m, nil
		}
		if m.cleaning {
			switch key {
			case "esc":
				m.cleaning = false
			case "up", "k":
				m.cleanupChoice = max(0, m.cleanupChoice-1)
			case "down", "j":
				m.cleanupChoice = min(len(cleanupAges)-1, m.cleanupChoice+1)
			case "enter":
				cutoff := time.Now().Add(-cleanupAges[m.cleanupChoice])
				return m, func() tea.Msg { n, err := m.engine.DeleteOlderThan(cutoff); return cleanupMsg{removed: n, err: err} }
			}
			return m, nil
		}
		switch key {
		case "n":
			m.creating = true
			m.stage = 0
			m.pickChoices()
			m.notice = ""
		case "x":
			m.cleaning = true
			m.cleanupChoice = 0
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
				return m, func() tea.Msg { return actionMsg{m.engine.Rerun(t.ID)} }
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
	EnqueueConfigured(pipeline, repository, provider string, answers form.Answers) error
	Fields(string) ([]form.Field, error)
	Rerun(string) error
	Cancel(id string)
	RetryPublish(id string) error
	DeleteOlderThan(cutoff time.Time) (int, error)
	Close()
}

func Run(controller Controller) error {
	m := ui{engine: controller, width: 100, height: 35}
	m.refresh()
	_, err := tea.NewProgram(m).Run()
	return err
}
