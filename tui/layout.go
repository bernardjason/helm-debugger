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
	SourceView      *tview.TextArea
	TraceValuesView *tview.TextView
	ValuesEditor    *tview.TextArea
	ErrorView       *tview.TextView
}

// NewDebuggerView builds a split layout with rendered charts and source on the
// left, plus trace-values, values.yaml editing, and errors on the right.
func NewDebuggerView(chartContent, sourceContent, traceValuesContent, valuesYAML string) *DebuggerView {
	chartView := tview.NewTextArea().
		SetText(chartContent, false).
		SetWrap(false)
	chartView.
		SetBorder(true).
		SetTitle("Expanded Helm Charts")
	chartView.SetInputCapture(readOnlyTextAreaCapture)

	sourceView := tview.NewTextArea().
		SetText(sourceContent, false).
		SetWrap(false)
	sourceView.
		SetBorder(true).
		SetTitle("Template Source")
	sourceView.SetInputCapture(readOnlyTextAreaCapture)

	traceValuesView := tview.NewTextView().
		SetText(traceValuesContent).
		SetScrollable(true).
		SetWrap(true)
	traceValuesView.
		SetBorder(true).
		SetTitle("Trace Values")

	valuesEditor := tview.NewTextArea().
		SetText(valuesYAML, false).
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

	leftPane := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(chartView, 0, 2, true).
		AddItem(sourceView, 0, 1, false)

	sidebar := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(traceValuesView, 0, 2, false).
		AddItem(valuesEditor, 0, 2, false).
		AddItem(errorView, 0, 1, false)

	root := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(leftPane, 0, 3, true).
		AddItem(sidebar, 0, 2, false)

	return &DebuggerView{
		Root:            root,
		ChartView:       chartView,
		SourceView:      sourceView,
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

func (v *DebuggerView) SetSourceContent(title, content string) {
	v.SourceView.SetTitle(title)
	v.SourceView.SetText(content, false)
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
