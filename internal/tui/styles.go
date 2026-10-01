package tui

// Styles use ANSI SGR sequences; layout.go measures visible terminal cells.
// Bubble Tea renders these styles using the terminal's supported color profile.
type textStyle string

const (
	borderStyle   textStyle = "38;5;67"
	titleStyle    textStyle = "1;38;5;81"
	mutedStyle    textStyle = "38;5;245"
	selectedStyle textStyle = "1;38;5;231;48;5;24"
	activeStyle   textStyle = "38;5;81"
	successStyle  textStyle = "38;5;114"
	warningStyle  textStyle = "38;5;221"
	errorStyle    textStyle = "1;38;5;203"
)

func (s textStyle) render(text string) string {
	if text == "" {
		return text
	}
	return "\x1b[" + string(s) + "m" + text + "\x1b[0m"
}

func statusStyle(status string) textStyle {
	switch status {
	case "running":
		return activeStyle
	case "succeeded":
		return successStyle
	case "failed":
		return errorStyle
	case "queued", "interrupted":
		return warningStyle
	default:
		return mutedStyle
	}
}
