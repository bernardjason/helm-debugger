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

	ch, err := loader.Load(cliOptions.Chart)
	if err != nil {
		log.Fatal(err)
	}

	vals := map[string]interface{}{}

	if cliOptions.Values != "" {
		vals, err = chartutil.ReadValuesFile(cliOptions.Values)
		if err != nil {
			log.Fatal(err)
		}
	}

	fmt.Println("VALS is ", vals)

	inst := action.NewInstall(actionConfig)
	inst.DryRun = true
	inst.ReleaseName = "debug"
	inst.Replace = true
	inst.ClientOnly = true
	inst.KubeVersion = &cliOptions.KubeVersion

	rel, err := inst.Run(ch, vals)
	if err != nil {
		log.Fatal(err)
	}

	print := true
	var specificTemplate string = ""
	if cliOptions.Template != "" {
		specificTemplate = fmt.Sprintf("# Source: %s", cliOptions.Template)
	}
	for _, line := range strings.Split(rel.Manifest, "\n") {

		if len(line) > 0 && line[0] == '#' && specificTemplate != "" {
			if line == specificTemplate {
				print = true
			} else {
				print = false
			}
		}

		if print {
			fmt.Println(line)
		}
	}
}
