package main

import (
	"github.com/bernardjason/helm-debugger/cmd"
	"github.com/bernardjason/helm-debugger/tracer"
	"github.com/bernardjason/helm-debugger/view"
)

func main() {

	actionConfig, cliOptions := cmd.Cmd()

	if cliOptions.Trace {
		tracer.TraceTemplates(cliOptions)
		return
	}

	view.ViewTemplates(actionConfig, cliOptions)

}
