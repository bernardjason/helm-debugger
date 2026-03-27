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
	ui := NewDebuggerView(chartContent, "", valuesContent)
	if valuesPath != "" {
		ui.ValuesEditor.SetTitle(fmt.Sprintf("values.yaml (%s)", valuesPath))
	}

	app := tview.NewApplication().EnableMouse(true).EnablePaste(true)
	focusOrder := []tview.Primitive{ui.ChartView, ui.ValuesEditor}
	focusIndex := 0
	errorMessage := ""
	chartLines := strings.Split(chartContent, "\n")

	updateTrace := func() {
		row, _, _, _ := ui.ChartView.GetCursor()
		templateName := selectedTemplateName(chartLines, row)
		fieldPath := inferYAMLPath(chartLines, row)
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

	rerender := func() {
		clearHelmMessages()
		rendered, renderErr := view.RenderTemplatesWithValuesYAML(actionConfig, cliOptions, ui.ValuesYAML())
		if renderErr != nil {
			errorMessage = fmt.Sprintf("Rerender failed: %v", renderErr)
			updateErrors()
			return
		}
		errorMessage = ""
		chartLines = strings.Split(rendered, "\n")
		ui.SetChartContent(rendered)
		updateTrace()
		updateErrors()
	}

	ui.ChartView.SetMovedFunc(updateTrace)
	ui.ValuesEditor.SetChangedFunc(func() {
		rerender()
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
