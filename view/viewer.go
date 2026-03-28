package view

import (
	"fmt"
	"log"
	"strings"

	"github.com/bernardjason/helm-debugger/cmd"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
)

func ViewTemplates(actionConfig *action.Configuration, cliOptions cmd.Options) {
	manifest, err := RenderTemplates(actionConfig, cliOptions)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(manifest)
}

func RenderTemplates(actionConfig *action.Configuration, cliOptions cmd.Options) (string, error) {
	return renderTemplates(actionConfig, cliOptions, nil)
}

func RenderTemplatesWithValuesYAML(actionConfig *action.Configuration, cliOptions cmd.Options, valuesYAML string) (string, error) {
	vals, err := chartutil.ReadValues([]byte(valuesYAML))
	if err != nil {
		return "", err
	}
	return renderTemplates(actionConfig, cliOptions, vals)
}

func renderTemplates(actionConfig *action.Configuration, cliOptions cmd.Options, inlineVals map[string]interface{}) (string, error) {
	ch, err := loader.Load(cliOptions.Chart)
	if err != nil {
		return "", err
	}

	vals := map[string]interface{}{}
	if inlineVals != nil {
		vals = inlineVals
	} else if cliOptions.Values != "" {
		vals, err = chartutil.ReadValuesFile(cliOptions.Values)
		if err != nil {
			return "", err
		}
	}

	inst := action.NewInstall(actionConfig)
	inst.DryRun = true
	inst.ReleaseName = "debug"
	inst.Replace = true
	inst.ClientOnly = true
	inst.KubeVersion = &cliOptions.KubeVersion

	rel, err := inst.Run(ch, vals)
	if err != nil {
		return "", err
	}

	printLine := true
	specificTemplate := ""
	if cliOptions.Template != "" {
		specificTemplate = fmt.Sprintf("# Source: %s", cliOptions.Template)
	}

	var builder strings.Builder
	for _, line := range strings.Split(rel.Manifest, "\n") {
		if len(line) > 0 && line[0] == '#' && specificTemplate != "" {
			if line == specificTemplate {
				printLine = true
			} else {
				printLine = false
			}
		}
		if printLine {
			builder.WriteString(line)
			builder.WriteByte('\n')
		}
	}

	return builder.String(), nil
}
