package tui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// DebuggerView groups the tview primitives used by the debugger layout.
type DebuggerView struct {
	Root            *tview.Flex
	ChartView       *tview.TextArea
	TraceValuesView *tview.TextView
	ValuesEditor    *tview.TextArea
	ErrorView       *tview.TextView
}

// NewDebuggerView builds a split layout with rendered charts on the left and
// trace-values, an editable values.yaml editor, and errors on the right.
func NewDebuggerView(chartContent, traceValuesContent, valuesYAML string) *DebuggerView {
	chartView := tview.NewTextArea().
		SetText(chartContent, false).
		SetWrap(false)
	chartView.
		SetBorder(true).
		SetTitle("Expanded Helm Charts")
	chartView.SetInputCapture(readOnlyTextAreaCapture)

	traceValuesView := tview.NewTextView().
		SetText(traceValuesContent).
		SetScrollable(true).
		SetWrap(true)
	traceValuesView.
		SetBorder(true).
		SetTitle("Trace Values")

	valuesEditor := tview.NewTextArea().
		SetText(valuesYAML, true).
		SetWrap(false)
	valuesEditor.
		SetBorder(true).
		SetTitle("values.yaml")

	errorView := tview.NewTextView().
		SetScrollable(true).
		SetWrap(true)
	errorView.
		SetBorder(true).
		SetTitle("Errors")

	sidebar := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(traceValuesView, 0, 2, false).
		AddItem(valuesEditor, 0, 2, false).
		AddItem(errorView, 0, 1, false)

	root := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(chartView, 0, 3, true).
		AddItem(sidebar, 0, 2, false)

	return &DebuggerView{
		Root:            root,
		ChartView:       chartView,
		TraceValuesView: traceValuesView,
		ValuesEditor:    valuesEditor,
		ErrorView:       errorView,
	}
}

func readOnlyTextAreaCapture(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyUp,
		tcell.KeyDown,
		tcell.KeyLeft,
		tcell.KeyRight,
		tcell.KeyPgUp,
		tcell.KeyPgDn,
		tcell.KeyHome,
		tcell.KeyEnd,
		tcell.KeyCtrlA,
		tcell.KeyCtrlB,
		tcell.KeyCtrlE,
		tcell.KeyCtrlF,
		tcell.KeyCtrlLeftSq,
		tcell.KeyCtrlRightSq:
		return event
	case tcell.KeyRune:
		switch event.Rune() {
		case 'h', 'j', 'k', 'l':
			return event
		}
	}
	return nil
}

func (v *DebuggerView) SetChartContent(content string) {
	cursorAtEnd := false
	if strings.HasSuffix(content, "\n") {
		cursorAtEnd = true
	}
	v.ChartView.SetText(content, cursorAtEnd)
}

func (v *DebuggerView) SetTraceValues(content string) {
	v.TraceValuesView.SetText(content)
}

func (v *DebuggerView) SetValuesYAML(content string) {
	v.ValuesEditor.SetText(content, true)
}

func (v *DebuggerView) SetErrors(content string) {
	v.ErrorView.SetText(content)
}

func (v *DebuggerView) ValuesYAML() string {
	return v.ValuesEditor.GetText()
}
