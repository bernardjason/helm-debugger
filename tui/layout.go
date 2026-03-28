package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// DebuggerView groups the tview primitives used by the debugger layout.
type DebuggerView struct {
	Root            *tview.Pages
	MenuBar         *tview.Flex
	OpenButton      *tview.Button
	NewButton       *tview.Button
	HelpButton      *tview.Button
	SyncButton      *tview.Button
	SaveButton      *tview.Button
	FocusButton     *tview.Button
	QuitButton      *tview.Button
	ChartView       *ManifestView
	SourceView      *SourceCodeView
	TraceValuesView *tview.TextView
	ValuesEditor    *tview.TextArea
	ErrorView       *tview.TextView
}

// NewDebuggerView builds a split layout with rendered charts and source on the
// left, plus trace-values, values.yaml editing, and errors on the right.
func NewDebuggerView(chartContent, sourceContent, traceValuesContent, valuesYAML string) *DebuggerView {
	makeMenuButton := func(label string) *tview.Button {
		button := tview.NewButton(label)
		button.SetLabelColor(tcell.ColorBlack)
		button.SetLabelColorActivated(tcell.ColorBlack)
		button.SetBackgroundColor(tcell.ColorLightSkyBlue)
		button.SetBackgroundColorActivated(tcell.ColorLightCyan)
		return button
	}

	openButton := makeMenuButton("Ctrl-O Open")
	newButton := makeMenuButton("Ctrl-N New")
	helpButton := makeMenuButton("Help")
	syncButton := makeMenuButton("Ctrl-L Sync")
	saveButton := makeMenuButton("Ctrl-S Save")
	focusButton := makeMenuButton("Tab Focus")
	quitButton := makeMenuButton("Ctrl-C Quit")

	makeSpacer := func() *tview.Box { return tview.NewBox() }
	menuBar := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(openButton, 14, 0, false).
		AddItem(makeSpacer(), 1, 0, false).
		AddItem(newButton, 13, 0, false).
		AddItem(makeSpacer(), 1, 0, false).
		AddItem(syncButton, 14, 0, false).
		AddItem(makeSpacer(), 1, 0, false).
		AddItem(saveButton, 14, 0, false).
		AddItem(makeSpacer(), 1, 0, false).
		AddItem(quitButton, 14, 0, false).
		AddItem(makeSpacer(), 0, 1, false).
		AddItem(helpButton, 12, 0, false).
		AddItem(makeSpacer(), 1, 0, false).
		AddItem(makeSpacer(), 1, 0, false)
	menuBar.SetBorder(true).SetTitle("Menu")

	chartView := NewManifestView(chartContent)
	chartView.SetBorder(true).SetTitle("Expanded Helm Charts")

	sourceView := NewSourceCodeView(sourceContent)
	sourceView.SetBorder(true).SetTitle("Template Source")

	traceValuesView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(formatTraceText(traceValuesContent)).
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
		SetDynamicColors(true).
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

	body := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(leftPane, 0, 3, true).
		AddItem(sidebar, 0, 2, false)

	main := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(menuBar, 3, 0, false).
		AddItem(body, 0, 1, true)

	pages := tview.NewPages()
	pages.AddPage("main", main, true, true)

	return &DebuggerView{
		Root:            pages,
		MenuBar:         menuBar,
		OpenButton:      openButton,
		NewButton:       newButton,
		HelpButton:      helpButton,
		SyncButton:      syncButton,
		SaveButton:      saveButton,
		FocusButton:     focusButton,
		QuitButton:      quitButton,
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
	v.ChartView.SetContent(content)
}

func (v *DebuggerView) SetSourceContent(title, content string) {
	v.SourceView.SetTitle(title)
	v.SourceView.SetContent(content)
}

func (v *DebuggerView) SetTraceValues(content string) {
	v.TraceValuesView.SetText(formatTraceText(content))
}

func (v *DebuggerView) SetValuesYAML(content string) {
	v.ValuesEditor.SetText(content, true)
}

func (v *DebuggerView) SetErrors(content string) {
	v.ErrorView.SetText(formatErrorText(content))
}

func (v *DebuggerView) ValuesYAML() string {
	return v.ValuesEditor.GetText()
}
