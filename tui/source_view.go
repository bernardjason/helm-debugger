package tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	templateStringPattern = regexp.MustCompile(`"[^"]*"|'[^']*'`)
	// templateKeywordPattern = regexp.MustCompile(`(^|[^[:alnum:]_])(include|tpl|define|range|with|if|end|else|)([^[:alnum:]_]|$)`)
	templateKeywordPattern = regexp.MustCompile(`(^|[^[:alnum:]_])(include|template|required|default|fail|lookup|if|else|end|and|or|not|eq|ne|lt|le|gt|ge|quote|upper|lower|title|trim|trimSuffix|trimPrefix|replace|contains|hasPrefix|hasSuffix|printf|print|println|list|dict|index|toYaml|nindent|indent|toJson|toPrettyJson|fromJson|coalesce|ternary|int|int64|float64|toString|b64enc|b64dec|now|date|dateInZone|range|with|tpl|semverCompare|randAlpha|randNumeric|sha256sum|)([^[:alnum:]_]|$)`)
	templateValuesPattern  = regexp.MustCompile(`(^|[^[:alnum:]_])(\.Values)([^[:alnum:]_]|$)`)
)

type SourceCodeView struct {
	*tview.TextView
	lines     []string
	selected  int
	movedFunc func()
}

func NewSourceCodeView(content string) *SourceCodeView {
	view := &SourceCodeView{
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

func (v *SourceCodeView) SetMovedFunc(handler func()) {
	v.movedFunc = handler
}

func (v *SourceCodeView) SelectedRow() int {
	return v.selected
}

func (v *SourceCodeView) SetSelectedRow(row int) {
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

func (v *SourceCodeView) SetContent(content string) {
	v.lines = strings.Split(content, "\n")
	var builder strings.Builder
	for i, line := range v.lines {
		fmt.Fprintf(&builder, `["%s"]%s[""]`, v.regionID(i), renderSourceLine(line))
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

func (v *SourceCodeView) handleInput(event *tcell.EventKey) *tcell.EventKey {
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
			return event
		}
	default:
		return event
	}
	v.SetSelectedRow(next)
	return nil
}

func (v *SourceCodeView) regionID(row int) string {
	return fmt.Sprintf("src-%d", row)
}

func renderSourceLine(line string) string {
	trimmed := strings.TrimSpace(line)
	// if strings.HasPrefix(trimmed, "{{") && strings.HasSuffix(trimmed, "}}") {
	// 	return `[red]` + escapeTView(line) + `[-]`
	// }
	if strings.HasPrefix(trimmed, "#") {
		return `[gray]` + escapeTView(line) + `[-]`
	}
	if idx := strings.Index(line, "#"); idx >= 0 {
		left := highlightTemplateSyntax(escapeTView(line[:idx]))
		right := `[gray]` + escapeTView(line[idx:]) + `[-]`
		return left + right
	}
	return highlightTemplateSyntax(escapeTView(line))
}

// // TO DO TIGHTEN UP COLOUR HIGHLIGHTING
// func highlightTemplateSyntax(s string) string {
// 	s = templateStringPattern.ReplaceAllStringFunc(s, func(match string) string {
// 		return `[gold]` + match + `[-]`
// 	})
// 	s = templateValuesPattern.ReplaceAllString(s, `${1}[pink]${2}[-]${3}`)
// 	s = templateKeywordPattern.ReplaceAllString(s, `${1}[teal]${2}[-]${3}`)
// 	s = strings.ReplaceAll(s, "{{", `[orange]{{[-]`)
// 	s = strings.ReplaceAll(s, "}}", `[orange]}}[-]`)

// 	return s
// }

func highlightTemplateSyntax(s string) string {
	var word string = ""
	var response string = ""
	var doubleQuotes = false
	var singleQuotes = false
	var colourStack = make(stack, 0)

	handleKey := func() {
		if len(word) > 0 && word[len(word)-1] == ':' {
			response = response + fmt.Sprintf("[blue]%s[-]%s ", word, colourStack.Peek())
		} else if strings.HasPrefix(word, ".Values") {
			response = response + fmt.Sprintf("[white]%s[-]%s ", word, colourStack.Peek())
		} else {
			response = response + word + " "
		}
	}

	for _, c := range s {
		if c == '"' {
			doubleQuotes = !doubleQuotes
		}
		if c == '\'' {
			singleQuotes = !singleQuotes
		}
		switch word {
		case "{{":
			colourStack = colourStack.Push("[orange]")
			response = response + fmt.Sprintf("[orange]%s", word)
			word = ""
		case "}}":
			response = response + fmt.Sprintf("%s[-]", word)
			colourStack, _ = colourStack.Pop()
			word = ""
		}
		if c != ' ' {
			word = word + string(c)
		} else {
			if !doubleQuotes && !singleQuotes {
				switch word {
				case "include", "template", "required", "default", "fail", "lookup", "if", "else", "end", "and", "or", "not", "eq", "ne",
					"lt", "le", "gt", "ge", "quote", "upper", "lower", "title", "trim", "trimSuffix", "trimPrefix", "replace", "contains",
					"hasPrefix", "hasSuffix", "printf", "print", "println", "list", "dict", "index", "toYaml", "nindent", "indent", "toJson",
					"toPrettyJson", "fromJson", "coalesce", "ternary", "int", "int64", "float64", "toString", "b64enc", "b64dec", "now", "date",
					"dateInZone", "range", "with", "tpl", "semverCompare", "randAlpha", "randNumeric", "sha256sum":
					response = response + fmt.Sprintf("[teal]%s[-]%s ", word, colourStack.Peek())
				default:
					handleKey()
				}
			} else {
				handleKey()
			}
			word = ""
		}
	}
	switch word {
	case "{{":
		colourStack = colourStack.Push("[orange]")
		response = response + fmt.Sprintf("[orange]%s%s", word, colourStack.Peek())
	case "}}":
		response = response + fmt.Sprintf("%s[-]", word)
		colourStack, _ = colourStack.Pop()
	default:
		response = response + word
	}

	return response
}

type stack []string

func (s stack) Push(v string) stack {
	return append(s, v)
}

func (s stack) Pop() (stack, string) {
	l := len(s)
	if l == 0 {
		return s, ""
	}
	return s[:l-1], s[l-1]
}
func (s stack) Peek() string {
	l := len(s)
	if l == 0 {
		return ""
	}
	return s[l-1]
}
