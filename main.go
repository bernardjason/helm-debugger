package main

import (
	"log"

	"github.com/bernardjason/helm-debugger/cmd"
	"github.com/bernardjason/helm-debugger/tracer"
	"github.com/bernardjason/helm-debugger/tui"
)

func main() {
	actionConfig, cliOptions := cmd.Cmd()

	if cliOptions.Trace {
		tracer.TraceTemplates(cliOptions)
		return
	}

	if err := tui.Run(actionConfig, cliOptions); err != nil {
		log.Fatal(err)
	}
}
