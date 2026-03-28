package tui

import "strings"

func formatTraceText(content string) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
		case strings.HasSuffix(trimmed, ":"):
			lines[i] = `[deepskyblue::b]` + escapeTView(line) + `[-::-]`
		case strings.Contains(trimmed, " -> "):
			lines[i] = `[teal]` + escapeTView(line) + `[-]`
		case strings.Contains(trimmed, "path: "):
			parts := strings.SplitN(line, "path: ", 2)
			lines[i] = escapeTView(parts[0]) + `[gold]path: ` + escapeTView(parts[1]) + `[-]`
		case strings.HasPrefix(trimmed, "Selected field:"):
			lines[i] = `[orchid]` + escapeTView(line) + `[-]`
		case strings.HasPrefix(trimmed, "Source line:"):
			lines[i] = `[green]` + escapeTView(line) + `[-]`
		default:
			lines[i] = escapeTView(line)
		}
	}
	return strings.Join(lines, "\n")
}

func formatErrorText(content string) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
		case strings.Contains(strings.ToLower(trimmed), "failed") || strings.Contains(strings.ToLower(trimmed), "error"):
			lines[i] = `[red::b]` + escapeTView(line) + `[-::-]`
		case strings.Contains(strings.ToLower(trimmed), "warning") || strings.Contains(strings.ToLower(trimmed), "cannot overwrite"):
			lines[i] = `[yellow]` + escapeTView(line) + `[-]`
		case strings.Contains(trimmed, "Ctrl-L") || strings.Contains(trimmed, "Ctrl-S"):
			lines[i] = `[teal]` + escapeTView(line) + `[-]`
		default:
			lines[i] = escapeTView(line)
		}
	}
	return strings.Join(lines, "\n")
}
