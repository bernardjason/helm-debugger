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
	initialTemplate := selectedTemplateName(strings.Split(chartContent, "\n"), 0)
	sourceTitle, sourceContent := loadTemplateSource(initialTemplate, cliOptions.Chart)
	ui := NewDebuggerView(chartContent, sourceContent, "", valuesContent)
	ui.SetSourceContent(sourceTitle, sourceContent)
	if valuesPath != "" {
		ui.ValuesEditor.SetTitle(fmt.Sprintf("values.yaml (%s)", valuesPath))
	}

	app := tview.NewApplication().EnableMouse(true).EnablePaste(true)
	focusOrder := []tview.Primitive{ui.ChartView, ui.SourceView, ui.ValuesEditor}
	focusIndex := 0
	errorMessage := "Press Ctrl-L to sync edited values into the rendered output."
	syncEnabled := true
	chartLines := strings.Split(chartContent, "\n")
	currentTemplate := initialTemplate
	updatingFromChart := false
	updatingFromSource := false

	scrollSourceToTemplateLine := func(templateName, fieldPath string) {
		if line, ok := traceSession.SourceLineForTemplate(templateName, fieldPath); ok {
			offset := line - 3
			if offset < 0 {
				offset = 0
			}
			ui.SourceView.SetOffset(offset, 0)
		} else {
			ui.SourceView.SetOffset(0, 0)
		}
	}

	updateTrace := func() {
		row, _, _, _ := ui.ChartView.GetCursor()
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
		sourceRow, _, _, _ := ui.SourceView.GetCursor()
		renderedRow, ok := renderedRowForSourceLine(chartLines, currentTemplate, sourceRow+1, traceSession)
		if !ok {
			return
		}
		updatingFromSource = true
		offset := renderedRow - 2
		if offset < 0 {
			offset = 0
		}
		ui.ChartView.SetOffset(offset, 0)
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

	ui.ChartView.SetMovedFunc(func() {
		if !syncEnabled || updatingFromSource {
			return
		}
		updateTrace()
	})
	ui.SourceView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			syncChartToSource()
			return nil
		}
		return readOnlyTextAreaCapture(event)
	})
	updateTrace()
	updateErrors()

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlC:
			app.Stop()
			return nil
		case tcell.KeyTAB:
			focusIndex = (focusIndex + 1) % len(focusOrder)
			app.SetFocus(focusOrder[focusIndex])
			return nil
		case tcell.KeyBacktab:
			focusIndex = (focusIndex + len(focusOrder) - 1) % len(focusOrder)
			app.SetFocus(focusOrder[focusIndex])
			return nil
		case tcell.KeyCtrlL:
			rerender()
			return nil
		case tcell.KeyCtrlS:
			if valuesPath == "" {
				errorMessage = "No writable values.yaml file resolved for this chart."
				updateErrors()
				return nil
			}
			if err := os.WriteFile(valuesPath, []byte(ui.ValuesYAML()), 0o644); err != nil {
				errorMessage = fmt.Sprintf("Save failed: %v", err)
			} else {
				errorMessage = ""
			}
			updateErrors()
			return nil
		}
		return event
	})

	return app.SetRoot(ui.Root, true).SetFocus(ui.ChartView).Run()
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
