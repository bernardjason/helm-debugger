package tui

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/bernardjason/helm-debugger/cmd"
	"github.com/bernardjason/helm-debugger/tracer"
	"github.com/bernardjason/helm-debugger/view"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"helm.sh/helm/v3/pkg/action"
)

type logCaptureWriter struct {
	append func(string)
}

func (w logCaptureWriter) Write(p []byte) (int, error) {
	message := strings.TrimSpace(string(p))
	if message != "" {
		w.append(message)
	}
	return len(p), nil
}

func Run(actionConfig *action.Configuration, cliOptions cmd.Options) error {
	helmMessages := []string{}
	appendHelmMessage := func(message string) {
		message = strings.TrimSpace(message)
		if message == "" {
			return
		}
		helmMessages = append(helmMessages, message)
	}

	oldLogWriter := log.Writer()
	oldLogFlags := log.Flags()
	oldLogPrefix := log.Prefix()
	log.SetOutput(logCaptureWriter{append: appendHelmMessage})
	log.SetFlags(0)
	log.SetPrefix("")
	defer func() {
		log.SetOutput(oldLogWriter)
		log.SetFlags(oldLogFlags)
		log.SetPrefix(oldLogPrefix)
	}()

	actionConfig.Log = func(format string, v ...interface{}) {
		appendHelmMessage(fmt.Sprintf(format, v...))
	}

	clearHelmMessages := func() {
		helmMessages = helmMessages[:0]
	}

	chartContent, err := func() (string, error) {
		clearHelmMessages()
		return view.RenderTemplates(actionConfig, cliOptions)
	}()
	if err != nil {
		return err
	}

	traceSession, err := tracer.NewSummarySession(cliOptions)
	if err != nil {
		return err
	}

	valuesPath, valuesContent := loadValuesFile(cliOptions)
	chartLines := strings.Split(chartContent, "\n")
	initialTemplate := selectedTemplateName(chartLines, 0)
	sourceTitle, sourceContent := loadTemplateSource(initialTemplate, cliOptions.Chart)
	ui := NewDebuggerView(chartContent, sourceContent, "", valuesContent)
	ui.SetSourceContent(sourceTitle, sourceContent)

	setValuesTitle := func() {
		title := "values.yaml"
		if valuesPath != "" {
			title = fmt.Sprintf("values.yaml (%s)", valuesPath)
		}
		ui.ValuesEditor.SetTitle(title)
	}
	setValuesTitle()

	app := tview.NewApplication().EnableMouse(true).EnablePaste(true)
	focusOrder := []tview.Primitive{ui.ChartView, ui.SourceView, ui.ValuesEditor}
	focusIndex := 0
	focusNext := func() {
		focusIndex = (focusIndex + 1) % len(focusOrder)
		app.SetFocus(focusOrder[focusIndex])
	}
	errorMessage := "Press Ctrl-L to sync edited values into the rendered output."
	syncEnabled := true
	currentTemplate := initialTemplate
	updatingFromChart := false
	updatingFromSource := false

	scrollSourceToTemplateLine := func(templateName, fieldPath string) {
		if line, ok := traceSession.SourceLineForTemplate(templateName, fieldPath); ok {
			offset := line - 3
			if offset < 0 {
				offset = 0
			}
			ui.SourceView.SetSelectedRow(offset)
		} else {
			ui.SourceView.SetSelectedRow(0)
		}
	}

	updateTrace := func() {
		row := ui.ChartView.SelectedRow()
		templateName := selectedTemplateName(chartLines, row)
		fieldPath := inferYAMLPath(chartLines, row)
		currentTemplate = templateName
		title, content := loadTemplateSource(templateName, cliOptions.Chart)
		ui.SetSourceContent(title, content)
		updatingFromChart = true
		scrollSourceToTemplateLine(templateName, fieldPath)
		updatingFromChart = false
		ui.SetTraceValues(traceSession.SummaryForTemplate(templateName, fieldPath))
	}

	buildErrorText := func() string {
		parts := make([]string, 0, len(helmMessages)+1)
		if errorMessage != "" {
			parts = append(parts, errorMessage)
		}
		parts = append(parts, helmMessages...)
		return strings.Join(parts, "\n")
	}

	updateErrors := func() {
		ui.SetErrors(buildErrorText())
	}

	syncChartToSource := func() {
		if !syncEnabled || updatingFromChart || currentTemplate == "" {
			return
		}
		sourceRow := ui.SourceView.SelectedRow()
		renderedRow, ok := renderedRowForSourceLine(chartLines, currentTemplate, sourceRow+1, traceSession)
		if !ok {
			return
		}
		updatingFromSource = true
		ui.ChartView.SetSelectedRow(renderedRow)
		updatingFromSource = false
	}

	rerender := func() {
		clearHelmMessages()
		rendered, renderErr := view.RenderTemplatesWithValuesYAML(actionConfig, cliOptions, ui.ValuesYAML())
		if renderErr != nil {
			syncEnabled = false
			errorMessage = fmt.Sprintf("Rerender failed: %v", renderErr)
			updateErrors()
			return
		}
		errorMessage = ""
		syncEnabled = true
		chartLines = strings.Split(rendered, "\n")
		ui.SetChartContent(rendered)
		updateTrace()
		updateErrors()
	}

	showPathPrompt := func(title, buttonLabel, initialValue string, onSubmit func(string) error) {
		input := tview.NewInputField().
			SetLabel("Path: ").
			SetText(initialValue)
		form := tview.NewForm().
			AddFormItem(input).
			AddButton(buttonLabel, func() {
				path := strings.TrimSpace(input.GetText())
				if path == "" {
					errorMessage = "A file path is required."
					updateErrors()
					return
				}
				if err := onSubmit(path); err != nil {
					errorMessage = err.Error()
				} else {
					errorMessage = ""
				}
				ui.Root.RemovePage("prompt")
				updateErrors()
				app.SetFocus(focusOrder[focusIndex])
			}).
			AddButton("Cancel", func() {
				ui.Root.RemovePage("prompt")
				app.SetFocus(focusOrder[focusIndex])
			})
		form.SetBorder(true).SetTitle(title)
		modal := centeredPrimitive(form, 80, 7)
		ui.Root.AddPage("prompt", modal, true, true)
		app.SetFocus(input)
	}

	showHelpModal := func() {
		helpText := strings.Join([]string{
			"Charts have to be local on disk for debugging",
			"Navigation",
			"",
			"Tab: cycle focus between rendered chart, source, and values editor.",
			"Shift-Tab: move focus backwards.",
			"Arrow keys, Page Up/Down, Home, End: move within the focused pane.",
			"Enter on Template Source: sync the rendered chart to the selected source line.",
			"",
			"Values",
			"",
			"Ctrl-O: open a values.yaml file into the editor.",
			"Ctrl-N: create a new values.yaml file and switch the editor to it.",
			"Ctrl-L: render the current values editor content into the chart.",
			"Ctrl-S: save the values editor to the active file path.",
			"",
			"Other",
			"",
			"Use the Trace Values pane to inspect the selected rendered field.",
			"Use the Errors pane to review Helm warnings, render failures, and save errors.",
			"Ctrl-C: quit the application.",
			"",
			"Helm commands",
			"helm list -A",
			"helm status <release> -n <namespace>",
			"helm repo add <name> <repo url>",
			"helm repo update",
			"helm search repo <name> --versions",
			"helm pull <name>/<chart> --version <version> --untar",
		}, "\n")

		content := tview.NewTextView().
			SetDynamicColors(true).
			SetScrollable(true).
			SetWrap(true).
			SetText(helpText)
		content.SetBorder(true).SetTitle("Help")

		closeButton := tview.NewButton("Close")
		closeButton.SetSelectedFunc(func() {
			ui.Root.RemovePage("help")
			app.SetFocus(focusOrder[focusIndex])
		})

		body := tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(content, 0, 1, true).
			AddItem(closeButton, 1, 0, false)
		body.SetBorder(true).SetTitle("Help")
		body.SetFullScreen(true)

		modal := centeredPrimitive(body, 90, 20)
		ui.Root.AddAndSwitchToPage("help", modal, true)
		app.SetFocus(closeButton)
	}

	openValuesFile := func(path string) error {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("Open failed: %w", err)
		}
		valuesPath = path
		ui.SetValuesYAML(string(content))
		setValuesTitle()
		return nil
	}

	createValuesFile := func(path string) error {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("Create failed: %s already exists", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("Create failed: %w", err)
		}
		if dir := filepath.Dir(path); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("Create failed: %w", err)
			}
		}
		if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
			return fmt.Errorf("Create failed: %w", err)
		}
		valuesPath = path
		ui.SetValuesYAML("")
		setValuesTitle()
		return nil
	}

	openAction := func() {
		showPathPrompt("Open values.yaml", "Open", valuesPath, openValuesFile)
	}
	newAction := func() {
		showPathPrompt("Create values.yaml", "Create", filepath.Join(filepath.Dir(valuesPath), "values.yaml"), createValuesFile)
	}
	helpAction := func() {
		showHelpModal()
	}
	syncAction := func() {
		rerender()
	}
	saveAction := func() {
		if valuesPath == "" {
			errorMessage = "No writable values.yaml file resolved for this chart."
			updateErrors()
			return
		}
		if err := os.WriteFile(valuesPath, []byte(ui.ValuesYAML()), 0o644); err != nil {
			errorMessage = fmt.Sprintf("Save failed: %v", err)
		} else {
			errorMessage = ""
		}
		updateErrors()
	}
	quitAction := func() {
		app.Stop()
	}

	ui.OpenButton.SetSelectedFunc(openAction)
	ui.NewButton.SetSelectedFunc(newAction)
	ui.HelpButton.SetSelectedFunc(helpAction)
	ui.SyncButton.SetSelectedFunc(syncAction)
	ui.SaveButton.SetSelectedFunc(saveAction)
	ui.FocusButton.SetSelectedFunc(focusNext)
	ui.QuitButton.SetSelectedFunc(quitAction)

	ui.ChartView.SetMovedFunc(func() {
		if !syncEnabled || updatingFromSource {
			return
		}
		updateTrace()
	})
	updateTrace()
	updateErrors()

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
			if focusIndex == 1 {
				syncChartToSource()
				return nil
			}
			return event
		case tcell.KeyCtrlC:
			quitAction()
			return nil
		case tcell.KeyTAB:
			focusNext()
			return nil
		case tcell.KeyBacktab:
			focusIndex = (focusIndex + len(focusOrder) - 1) % len(focusOrder)
			app.SetFocus(focusOrder[focusIndex])
			return nil
		case tcell.KeyCtrlL:
			syncAction()
			return nil
		case tcell.KeyCtrlO:
			openAction()
			return nil
		case tcell.KeyCtrlN:
			newAction()
			return nil
		case tcell.KeyCtrlS:
			saveAction()
			return nil
		}
		return event
	})

	return app.SetRoot(ui.Root, true).SetFocus(ui.ChartView).Run()
}

func centeredPrimitive(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 1, true).
			AddItem(nil, 0, 1, false), width, 1, true).
		AddItem(nil, 0, 1, false)
}

func loadValuesFile(cliOptions cmd.Options) (string, string) {
	candidates := []string{}
	if cliOptions.Values != "" {
		candidates = append(candidates, cliOptions.Values)
	}
	if info, err := os.Stat(cliOptions.Chart); err == nil && info.IsDir() {
		candidates = append(candidates, filepath.Join(cliOptions.Chart, "values.yaml"))
	}
	for _, candidate := range candidates {
		content, err := os.ReadFile(candidate)
		if err == nil {
			return candidate, string(content)
		}
	}
	if len(candidates) > 0 {
		return candidates[0], ""
	}
	return "", ""
}

func loadTemplateSource(templateName, chartPath string) (string, string) {
	if templateName == "" {
		return "Template Source", "Move the cursor onto a rendered manifest line under a '# Source:' header."
	}

	resolvedPath := resolveTemplatePath(templateName, chartPath)
	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		return fmt.Sprintf("Template Source (%s)", resolvedPath), fmt.Sprintf("unable to load template source: %v", err)
	}
	return fmt.Sprintf("Template Source (%s)", resolvedPath), string(content)
}

func resolveTemplatePath(templateName, chartPath string) string {
	candidates := []string{templateName}

	cleanChartPath := filepath.Clean(chartPath)
	if filepath.IsAbs(templateName) {
		return templateName
	}

	if cleanChartPath != "" {
		parts := strings.Split(filepath.ToSlash(templateName), "/")
		if len(parts) > 1 {
			candidates = append(candidates, filepath.Join(cleanChartPath, filepath.FromSlash(strings.Join(parts[1:], "/"))))
		}
		if strings.Contains(filepath.ToSlash(templateName), "/charts/") {
			idx := strings.Index(filepath.ToSlash(templateName), "/charts/")
			candidates = append(candidates, filepath.Join(cleanChartPath, filepath.FromSlash(templateName[idx+1:])))
		}
		candidates = append(candidates, filepath.Join(cleanChartPath, filepath.FromSlash(templateName)))
	}

	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return filepath.Clean(candidates[len(candidates)-1])
}

func renderedRowForSourceLine(chartLines []string, templateName string, sourceLine int, session *tracer.SummarySession) (int, bool) {
	bestRow := -1
	bestDistance := int(^uint(0) >> 1)
	for row := range chartLines {
		if selectedTemplateName(chartLines, row) != templateName {
			continue
		}
		fieldPath := inferYAMLPath(chartLines, row)
		if fieldPath == "" {
			continue
		}
		candidateLine, ok := session.SourceLineForTemplate(templateName, fieldPath)
		if !ok {
			continue
		}
		distance := candidateLine - sourceLine
		if distance < 0 {
			distance = -distance
		}
		if distance < bestDistance {
			bestDistance = distance
			bestRow = row
		}
		if distance == 0 {
			break
		}
	}
	if bestRow == -1 {
		return 0, false
	}
	return bestRow, true
}

func selectedTemplateName(lines []string, row int) string {
	if row < 0 {
		row = 0
	}
	if row >= len(lines) {
		row = len(lines) - 1
	}
	for i := row; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "# Source: ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# Source: "))
		}
		if trimmed == "---" {
			break
		}
	}
	return ""
}

func inferYAMLPath(lines []string, row int) string {
	type yamlNode struct {
		indent int
		key    string
	}

	var stack []yamlNode
	for i := 0; i <= row && i < len(lines); i++ {
		raw := lines[i]
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "---" {
			stack = stack[:0]
			continue
		}

		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		for len(stack) > 0 && indent <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}

		if strings.HasPrefix(trimmed, "- ") {
			item := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			if key := keyForLine(item); key != "" {
				stack = append(stack, yamlNode{indent: indent, key: key + "[]"})
			} else {
				stack = append(stack, yamlNode{indent: indent, key: "[]"})
			}
			continue
		}

		if key := keyForLine(trimmed); key != "" {
			stack = append(stack, yamlNode{indent: indent, key: key})
		}
	}

	parts := make([]string, 0, len(stack))
	for _, node := range stack {
		if node.key != "" {
			parts = append(parts, node.key)
		}
	}
	return strings.Join(parts, ".")
}

func keyForLine(line string) string {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return ""
	}
	return strings.TrimSpace(line[:idx])
}
