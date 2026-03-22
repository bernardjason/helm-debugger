package cmd

import (
	"flag"
	"log"
	"os"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
)

type Options struct {
	Template    string
	Chart       string
	Trace       bool
	Values      string
	KubeVersion chartutil.KubeVersion
	Field       string
}

func Cmd() (*action.Configuration, Options) {

	cliOptions := Options{}

	cliOptions.KubeVersion = chartutil.KubeVersion{
		Version: "v1.21.0",
		Major:   "1",
		Minor:   "21",
	}

	flag.StringVar(&cliOptions.Template, "show-only", "", "only show this chart, default is all")
	flag.BoolVar(&cliOptions.Trace, "trace", false, "show template/helper call graph instead of rendered manifests")
	flag.StringVar(&cliOptions.Values, "values", "", "yaml file to override anything set by chart")
	flag.StringVar(&cliOptions.Chart, "chart", "./chart", "name of chart to use, defaults to all")
	flag.StringVar(&cliOptions.Field, "field", "", "yaml field we are interested in")
	flag.Parse()

	settings := cli.New()

	actionConfig := new(action.Configuration)

	if err := actionConfig.Init(
		settings.RESTClientGetter(),
		"default",
		os.Getenv("HELM_DRIVER"),
		func(format string, v ...interface{}) {
			log.Printf(format, v...)
		},
	); err != nil {
		log.Fatal(err)
	}

	return actionConfig, cliOptions
}
