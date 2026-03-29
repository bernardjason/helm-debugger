package view

import (
	"log"
	"os"
	"strings"
	"testing"

	"github.com/bernardjason/helm-debugger/cmd"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
)

func newTestActionConfig(t *testing.T) *action.Configuration {
	t.Helper()

	settings := cli.New()
	cfg := new(action.Configuration)
	if err := cfg.Init(
		settings.RESTClientGetter(),
		"default",
		os.Getenv("HELM_DRIVER"),
		func(format string, v ...interface{}) {
			log.Printf(format, v...)
		},
	); err != nil {
		t.Fatalf("init action config: %v", err)
	}

	return cfg
}

func TestRenderTemplatesWithValuesYAMLOverridesManifest(t *testing.T) {
	actionConfig := newTestActionConfig(t)
	cliOptions := cmd.Options{
		Chart: "../chart",
		KubeVersion: chartutil.KubeVersion{
			Version: "v1.21.0",
			Major:   "1",
			Minor:   "21",
		},
	}

	valuesYAML := `replicaCount: 3
image:
  repository: busybox
  tag: "1.36"
config:
  logLevel: debug
service:
  port: 8080
`

	manifest, err := RenderTemplatesWithValuesYAML(actionConfig, cliOptions, valuesYAML)
	if err != nil {
		t.Fatalf("RenderTemplatesWithValuesYAML returned error: %v", err)
	}

	checks := []string{
		"replicas: 3",
		"image: \"busybox:1.36\"",
		"value: \"debug\"",
		"port: 8080",
		"title: \"Hello World From Helm\"",
		"nindent-demo: |",
	}

	for _, want := range checks {
		if !strings.Contains(manifest, want) {
			t.Fatalf("manifest missing %q\nmanifest:\n%s", want, manifest)
		}
	}
}
