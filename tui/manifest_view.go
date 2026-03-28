package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var yamlNumberPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$`)

type ManifestView struct {
	*tview.TextView
	lines     []string
	selected  int
	movedFunc func()
}

func NewManifestView(content string) *ManifestView {
	view := &ManifestView{
		TextView: tview.NewTextView().
			SetDynamicColors(true).
			SetRegions(true).
			SetScrollable(true).
			SetWrap(false),
	}
	view.SetInputCapture(view.handleInput)
	view.SetContent(content)
	return view
}

func (v *ManifestView) SetMovedFunc(handler func()) {
	v.movedFunc = handler
}

func (v *ManifestView) SelectedRow() int {
	return v.selected
}

func (v *ManifestView) SetSelectedRow(row int) {
	if len(v.lines) == 0 {
		v.selected = 0
		v.TextView.Highlight()
		return
	}
	if row < 0 {
		row = 0
	}
	if row >= len(v.lines) {
		row = len(v.lines) - 1
	}
	v.selected = row
	v.TextView.Highlight(v.regionID(row))
	v.TextView.ScrollTo(max(row-2, 0), 0)
	if v.movedFunc != nil {
		v.movedFunc()
	}
}

func (v *ManifestView) SetContent(content string) {
	v.lines = strings.Split(content, "\n")
	var builder strings.Builder
	for i, line := range v.lines {
		fmt.Fprintf(&builder, `["%s"]%s[""]`, v.regionID(i), renderManifestLine(line))
		if i < len(v.lines)-1 {
			builder.WriteByte('\n')
		}
	}
	v.TextView.SetText(builder.String())
	if len(v.lines) == 0 {
		v.selected = 0
		v.TextView.Highlight()
		return
	}
	if v.selected >= len(v.lines) {
		v.selected = len(v.lines) - 1
	}
	v.TextView.Highlight(v.regionID(v.selected))
	v.TextView.ScrollTo(max(v.selected-2, 0), 0)
}

func (v *ManifestView) handleInput(event *tcell.EventKey) *tcell.EventKey {
	if len(v.lines) == 0 {
		return nil
	}
	next := v.selected
	switch event.Key() {
	case tcell.KeyUp:
		next--
	case tcell.KeyDown:
		next++
	case tcell.KeyPgUp:
		_, _, _, height := v.GetInnerRect()
		if height <= 1 {
			height = 10
		}
		next -= height - 1
	case tcell.KeyPgDn:
		_, _, _, height := v.GetInnerRect()
		if height <= 1 {
			height = 10
		}
		next += height - 1
	case tcell.KeyHome, tcell.KeyCtrlA:
		next = 0
	case tcell.KeyEnd, tcell.KeyCtrlE:
		next = len(v.lines) - 1
	case tcell.KeyRune:
		switch event.Rune() {
		case 'j':
			next++
		case 'k':
			next--
		case 'g':
			next = 0
		case 'G':
			next = len(v.lines) - 1
		default:
			return nil
		}
	default:
		return nil
	}
	v.SetSelectedRow(next)
	return nil
}

func (v *ManifestView) regionID(row int) string {
	return fmt.Sprintf("line-%d", row)
}

func renderManifestLine(line string) string {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "# Source: "):
		return `[teal::b]` + escapeTView(line) + `[-::-]`
	case strings.HasPrefix(trimmed, "#"):
		return `[gray]` + escapeTView(line) + `[-]`
	case trimmed == "---":
		return `[blue::b]` + escapeTView(line) + `[-::-]`
	}

	prefix, key, value, ok := splitYAMLKeyValue(line)
	if !ok {
		return highlightTemplateDelimiters(escapeTView(line))
	}

	return escapeTView(prefix) + `[deepskyblue]` + escapeTView(key) + `[-]: ` + renderYAMLValue(value)
}

func splitYAMLKeyValue(line string) (string, string, string, bool) {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", "", "", false
	}
	keyPart := line[:idx]
	trimmedKey := strings.TrimSpace(keyPart)
	if trimmedKey == "" || strings.HasPrefix(trimmedKey, "#") {
		return "", "", "", false
	}
	prefixLen := len(keyPart) - len(strings.TrimLeft(keyPart, " -"))
	prefix := keyPart[:prefixLen]
	key := strings.TrimSpace(keyPart[prefixLen:])
	return prefix, key, line[idx+1:], true
}

func renderYAMLValue(value string) string {
	leading := value[:len(value)-len(strings.TrimLeft(value, " "))]
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return escapeTView(value)
	}

	comment := ""
	if idx := strings.Index(trimmed, " #"); idx >= 0 {
		comment = trimmed[idx+1:]
		trimmed = strings.TrimRight(trimmed[:idx], " ")
	}

	color := "white"
	switch {
	case strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`):
		color = "gold"
	case strings.HasPrefix(trimmed, `'`) && strings.HasSuffix(trimmed, `'`):
		color = "gold"
	case trimmed == "true" || trimmed == "false":
		color = "orchid"
	case trimmed == "null" || trimmed == "~":
		color = "orchid"
	case yamlNumberPattern.MatchString(trimmed):
		color = "green"
	case trimmed != "" && isLikelyCollectionStart(trimmed):
		color = "lightskyblue"
	}

	var builder strings.Builder
	builder.WriteString(escapeTView(leading))
	builder.WriteString("[")
	builder.WriteString(color)
	builder.WriteString("]")
	builder.WriteString(highlightTemplateDelimiters(escapeTView(trimmed)))
	builder.WriteString("[-]")
	if comment != "" {
		builder.WriteString(" [gray]#")
		builder.WriteString(escapeTView(strings.TrimSpace(comment)))
		builder.WriteString("[-]")
	}
	return builder.String()
}

func isLikelyCollectionStart(value string) bool {
	if value == "[]" || value == "{}" {
		return true
	}
	if strings.HasPrefix(value, "[") || strings.HasPrefix(value, "{") {
		return true
	}
	if _, err := strconv.Unquote(value); err == nil {
		return false
	}
	return false
}

func highlightTemplateDelimiters(s string) string {
	s = strings.ReplaceAll(s, "{{", `[orange]{{[-]`)
	s = strings.ReplaceAll(s, "}}", `[orange]}}[-]`)
	return s
}

func escapeTView(s string) string {
	s = strings.ReplaceAll(s, `[`, `[[`)
	s = strings.ReplaceAll(s, `]`, `]]`)
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
