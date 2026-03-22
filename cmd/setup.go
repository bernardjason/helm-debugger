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
	Helper      string
	Trace       bool
	TraceValues bool
	Values      string
	KubeVersion chartutil.KubeVersion
	Field       string
}

func Cmd() (*action.Configuration, Options) {
	cliOptions := Options{}
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "trace" {
		cliOptions.Trace = true
		args = args[1:]
	}

	cliOptions.KubeVersion = chartutil.KubeVersion{
		Version: "v1.21.0",
		Major:   "1",
		Minor:   "21",
	}

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flag.StringVar(&cliOptions.Template, "show-only", "", "only show this chart, default is all")
	flag.StringVar(&cliOptions.Helper, "helper", "", "trace a specific helper/template definition")
	flag.BoolVar(&cliOptions.Trace, "trace", false, "show template/helper call graph instead of rendered manifests")
	flag.BoolVar(&cliOptions.TraceValues, "trace-values", false, "show inferred values override paths in trace output")
	flag.StringVar(&cliOptions.Values, "f", "", "yaml file to override anything set by chart")
	flag.StringVar(&cliOptions.Values, "values", "", "yaml file to override anything set by chart")
	flag.StringVar(&cliOptions.Chart, "chart", "./chart", "name of chart to use, defaults to all")
	flag.StringVar(&cliOptions.Field, "field", "", "yaml field we are interested in")
	if err := flag.CommandLine.Parse(args); err != nil {
		log.Fatal(err)
	}
	if remaining := flag.CommandLine.Args(); len(remaining) > 0 {
		cliOptions.Chart = remaining[0]
	}

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
