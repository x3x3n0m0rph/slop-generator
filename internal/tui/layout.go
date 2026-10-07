package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"slop-generator/internal/form"
	"slop-generator/internal/task"
)

func (m ui) screenSize() (int, int) {
	width, height := m.width, m.height
	if width == 0 {
		width = 100
	}
	if height == 0 {
		height = 35
	}
	return max(1, width), max(1, height)
}
func (m *ui) keepSelectionVisible() {
	_, height := m.screenSize()
	rows := max(1, height-5)
	m.listOffset = max(0, min(m.listOffset, max(0, len(m.tasks)-rows)))
	if m.selected < m.listOffset {
		m.listOffset = m.selected
	}
	if m.selected >= m.listOffset+rows {
		m.listOffset = m.selected - rows + 1
	}
}

// fitLine measures terminal cells rather than bytes, including wide Unicode.
func fitLine(text string, width int) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\n", " "), "\r", "")
	text = ansi.Truncate(text, width, "…")
	return text + strings.Repeat(" ", max(0, width-ansi.StringWidth(text)))
}
func wrapLines(text string, width int) []string {
	return strings.Split(ansi.Hardwrap(strings.ReplaceAll(text, "\r", ""), width, true), "\n")
}
func heading(text string, width int) string {
	text = ansi.Truncate(" "+text+" ", width, "")
	return titleStyle.render(text) + borderStyle.render(strings.Repeat("─", max(0, width-ansi.StringWidth(text))))
}

func eventLabel(event string) string {
	if strings.HasPrefix(event, "[stage:") || strings.HasPrefix(event, "[pipeline]") {
		return event
	}
	fields := strings.Fields(event)
	if len(fields) >= 2 && (fields[0] == "Running" || fields[0] == "Completed") {
		return "[stage: " + fields[1] + "] " + strings.TrimPrefix(event, fields[0]+" ")
	}
	if len(fields) >= 4 && fields[0] == "Retrying" {
		return "[stage: " + fields[3] + "] " + event
	}
	return "[pipeline] " + event
}

func currentStage(step string) string {
	const prefix = "[stage: "
	if strings.HasPrefix(step, prefix) {
		if end := strings.Index(step, "] "); end > len(prefix) && strings.HasSuffix(step, "] Running") {
			return step[len(prefix):end]
		}
	}
	if strings.HasPrefix(step, "Running ") {
		return strings.TrimPrefix(step, "Running ")
	}
	return "—"
}

func (m ui) View() tea.View {
	width, height := m.screenSize()
	if width < 8 || height < 6 {
		lines := []string{fitLine("SLOP GENERATOR", width)}
		for len(lines) < height {
			lines = append(lines, strings.Repeat(" ", width))
		}
		v := tea.NewView(strings.Join(lines, "\n"))
		v.AltScreen = true
		return v
	}
	available := width - 3
	left := min(48, max(24, available/3))
	left = min(left, max(1, available/2))
	right := available - left
	rows := height - 5
	leftLines := m.taskList(left, rows)
	rightLines := m.details(right, rows)
	lines := []string{titleStyle.render(fitLine("SLOP GENERATOR", width))}
	listTitle := fmt.Sprintf("Tasks %d", len(m.tasks))
	if len(m.tasks) > 0 {
		listTitle = fmt.Sprintf("Tasks %d/%d", m.selected+1, len(m.tasks))
	}
	detailTitle := "Details / execution"
	if m.cleaning {
		detailTitle = "Clean old tasks"
	} else if m.creating {
		detailTitle = "New task"
	}
	lines = append(lines, borderStyle.render("┌")+heading(listTitle, left)+borderStyle.render("┬")+heading(detailTitle, right)+borderStyle.render("┐"))
	for row := 0; row < rows; row++ {
		l, r := "", ""
		if row < len(leftLines) {
			l = leftLines[row]
		}
		if row < len(rightLines) {
			r = rightLines[row]
		}
		lines = append(lines, borderStyle.render("│")+fitLine(l, left)+borderStyle.render("│")+fitLine(r, right)+borderStyle.render("│"))
	}
	lines = append(lines, borderStyle.render("└"+strings.Repeat("─", left)+"┴"+strings.Repeat("─", right)+"┘"))
	lines = append(lines, mutedStyle.render(fitLine("n new  x clean old tasks  ↑/↓ tasks  c cancel  r rerun  p retry  PgUp/PgDn logs  q quit", width)))
	notice := m.notice
	if m.closing {
		notice = "Stopping tasks and saving history…"
	}
	lines = append(lines, warningStyle.render(fitLine(notice, width)))
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}

func (m ui) taskList(width, rows int) []string {
	if len(m.tasks) == 0 {
		return []string{mutedStyle.render("No tasks yet.")}
	}
	start := max(0, min(m.listOffset, max(0, len(m.tasks)-rows)))
	if m.selected < start {
		start = m.selected
	}
	if m.selected >= start+rows {
		start = m.selected - rows + 1
	}
	lines := make([]string, 0, rows)
	for i := start; i < min(len(m.tasks), start+rows); i++ {
		t := m.tasks[i]
		mark := " "
		if i == m.selected {
			mark = ">"
		}
		id := t.ID
		if len(id) > 6 {
			id = id[len(id)-6:]
		}
		prefix := fmt.Sprintf("%s %-4s ", mark, shortStatus(t.Status))
		suffix := " " + id
		label := fitLine(t.PipelineName, max(0, width-ansi.StringWidth(prefix+suffix)))
		if i == m.selected {
			lines = append(lines, selectedStyle.render(fitLine(prefix+label+suffix, width)))
		} else {
			prefix = mark + " " + statusStyle(t.Status).render(fmt.Sprintf("%-4s", shortStatus(t.Status))) + " "
			lines = append(lines, prefix+label+mutedStyle.render(suffix))
		}
	}
	return lines
}
func shortStatus(status string) string {
	switch status {
	case "queued":
		return "WAIT"
	case "running":
		return "RUN"
	case "succeeded":
		return "DONE"
	case "failed":
		return "FAIL"
	case "cancelled":
		return "STOP"
	case "interrupted":
		return "INT"
	}
	return status
}

func (m ui) details(width, rows int) []string {
	if m.closing {
		return wrapLines("Stopping tasks and saving history…", width)
	}
	if m.cleaning {
		ageNames := []string{"1 day", "7 days", "30 days", "90 days"}
		lines := wrapLines("Choose the age limit. Enter confirms deletion of completed tasks created before that limit. Running and queued tasks are kept.", width)
		lines = append(lines, "")
		for i, age := range cleanupAges {
			count := 0
			cutoff := time.Now().Add(-age)
			for _, t := range m.tasks {
				if t.Created.Before(cutoff) && t.Status != "queued" && t.Status != "running" {
					count++
				}
			}
			mark := "  "
			if i == m.cleanupChoice {
				mark = "> "
			}
			line := fmt.Sprintf("%s%s · %d task(s)", mark, ageNames[i], count)
			if i == m.cleanupChoice {
				line = selectedStyle.render(line)
			}
			lines = append(lines, line)
		}
		lines = append(lines, "", "Enter delete · Esc cancel")
		return lines
	}
	if m.creating {
		if m.stage == 3 {
			field := m.fields[m.fieldIndex]
			help := "Enter accepts · Esc cancels"
			if field.Kind == form.Multiline {
				help = "Enter newline · Ctrl+S accepts · Esc cancels"
			}
			header := wrapLines(fmt.Sprintf("Field %d/%d · %s\n%s · Shift+Tab back", m.fieldIndex+1, len(m.fields), field.Label, help), width)
			input := wrapLines(m.value+"▌", width)
			capacity := max(1, rows-len(header)-1)
			if len(input) > capacity {
				input = input[len(input)-capacity:]
			}
			return append(append(header, ""), input...)
		}
		labels := []string{"pipeline", "repository", "provider"}
		lines := wrapLines("Choose "+labels[m.stage]+"\nEnter selects · Esc cancels", width)
		lines = append(lines, "")
		capacity := max(1, rows-len(lines))
		start := max(0, m.choice-capacity+1)
		for i := start; i < min(len(m.choices), start+capacity); i++ {
			mark := "  "
			if i == m.choice {
				mark = "> "
			}
			line := fitLine(mark+m.choices[i], width)
			if i == m.choice {
				line = selectedStyle.render(line)
			}
			lines = append(lines, line)
		}
		return lines
	}
	if len(m.tasks) == 0 {
		return wrapLines("Select a task to see its details.\n\nPress n to create a task.", width)
	}
	t := m.tasks[m.selected]
	stage := "—"
	if t.Status == "running" {
		stage = currentStage(t.Step)
	}
	metadata := []string{
		"Task: " + t.ID,
		"Pipeline: " + t.PipelineName,
		fmt.Sprintf("Repository: %s [%s]", t.RepoName, t.Repo.Branch),
		fmt.Sprintf("Status: %s · Stages: %d/%d", statusStyle(t.Status).render(t.Status), t.Completed, t.StageTotal),
		"Provider: " + t.ProviderName,
		"Model: " + t.Provider.Model,
		"Current stage: " + stage,
		"Latest activity: " + t.Step,
		fmt.Sprintf("Started: %s · Elapsed: %s", t.Created.Format(time.DateTime), taskElapsed(t).Round(time.Second)),
	}
	if t.UsageKnown {
		suffix := ""
		if t.UsageMissing {
			suffix = " (partial)"
		}
		metadata = append(metadata, fmt.Sprintf("Tokens: in %d / out %d / total %d%s", t.Usage.Prompt, t.Usage.Completion, t.Usage.Total, suffix))
	} else {
		metadata = append(metadata, "Tokens: unavailable")
	}
	if t.SHA != "" {
		metadata = append(metadata, "Commit: "+t.SHA)
	}
	if t.Published {
		metadata = append(metadata, "Publication: "+successStyle.render("succeeded"))
	}
	if t.Error != "" {
		metadata = append(metadata, errorStyle.render("Error: "+t.Error), "Working copy: "+t.Work)
	}
	lines := wrapLines(strings.Join(metadata, "\n"), width)
	limit := max(0, rows-3)
	if len(lines) > limit {
		lines = lines[:limit]
	}
	lines = append(lines, borderStyle.render(strings.Repeat("─", width)), titleStyle.render("Execution events:"))
	capacity := max(0, rows-len(lines))
	events := []string{}
	for _, event := range t.Events {
		events = append(events, wrapLines(eventLabel(event), width)...)
	}
	offset := min(m.scroll, max(0, len(events)-capacity))
	last := len(events) - offset
	first := max(0, last-capacity)
	return append(lines, events[first:last]...)
}

func taskElapsed(t task.Task) time.Duration {
	if t.Status == "running" || t.Status == "queued" {
		return time.Since(t.Created)
	}
	return t.Updated.Sub(t.Created)
}
