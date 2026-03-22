# MVP features

Render manifests through Helm SDK, then add:

Merged values view

Template/helper call graph

Searchable mapping of rendered field → source template

Trace mode for one helper

“Why this value?” explanation for common patterns

# ## A good architecture

### `cmd/helm-debug`

CLI and flags

### `internal/runner`

Loads chart, merges values, invokes Helm SDK actions

### `internal/analyzer`

Static analysis of:

* `define`
* `include`
* `template`
* `tpl`
* `.Values.*`
* `default`
* `coalesce`

### `internal/tracer`

Tracks:

* helper calls
* scope changes
* value lineage where inferable

### `internal/output`

Formats:

* plain text
* JSON
* maybe HTML/TUI later


------------------------------------------------------------------------------------------------------------------------

Yes — you’d typically build this as a **separate CLI that uses Helm’s Go SDK**, not as something Helm calls automatically. Helm’s own docs say the Go SDK lets custom software leverage Helm functionality, and that **the Helm CLI is effectively just one such tool**. ([Helm][1])

So the two realistic models are:

**Model 1: run instead of `helm`**
You build something like:

```bash
helm-debug template ./chart -f values.yaml
```

Your tool loads the chart, merges values, and invokes Helm’s rendering/install logic through the SDK. This is the cleanest option for a debugger. The `action` package is specifically meant for top-level Helm actions like install, upgrade, and list, mirroring the CLI. ([Go Packages][2])

**Model 2: wrap the `helm` binary externally**
You shell out to `helm template --debug`, parse output, and add your own tracing. This is easier to start, but much weaker if you want helper call stacks or scope inspection, because you only see the final output and Helm’s printed diagnostics.

For a real debugger, **Model 1 is better**.

## How to wrap Helm’s rendering engine

At a high level, your Go program would:

1. Initialize Helm action configuration.
2. Load the chart from disk.
3. Merge values.
4. Run a render-oriented Helm action, usually an install action in dry-run/client-only mode, or use Helm’s render path directly.
5. Intercept/augment template execution where possible.

Helm’s SDK examples and `action` package are the canonical places to start. Helm documents SDK examples, and the install action exposes `Run` / `RunWithContext`. The install path also has a dry-run mode used for template-style rendering without talking to the cluster. ([Helm][3])

## Smallest useful skeleton

A minimal wrapper looks roughly like this:

```go
package main

import (
	"fmt"
	"log"
	"os"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
)

func main() {
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

	ch, err := loader.Load("./chart")
	if err != nil {
		log.Fatal(err)
	}

	vals := map[string]interface{}{
		"image": map[string]interface{}{
			"tag": "debug",
		},
	}

	inst := action.NewInstall(actionConfig)
	inst.DryRun = true
	inst.ReleaseName = "debug-release"
	inst.Replace = true
	inst.ClientOnly = true

	rel, err := inst.Run(ch, vals)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(rel.Manifest)
}
```

That general shape matches Helm’s SDK model: `loader.Load(...)` for charts and `action.NewInstall(...).Run(...)` for execution. Helm’s docs call `loader.Load` the preferred way to load a chart, and the install action supports dry-run behavior used for rendering. ([Go Packages][4])

## Where the hard part starts

If you only want “Helm but embedded,” the above is enough.
If you want a **debugger**, you need to go deeper.

The challenge is that Helm templates are rendered through Go’s template engine, and Helm’s public SDK is designed around **actions**, not around a debugger API. The official docs cover using the SDK for Helm functionality, but they do not provide a step debugger or variable watch interface. ([Helm][1])

So a debugger project usually goes one of three ways:

### Option A: build a smarter renderer around public SDK APIs

Good for:

* showing merged values
* showing rendered manifests
* helper usage graphs from static analysis
* tracing likely value origins

Less good for:

* true step-through execution

### Option B: instrument Helm internals

You vendor or import deeper Helm packages and wrap template-related functions such as `include`, `tpl`, and helper resolution. This is the most powerful route, but Helm internals are less stable than the top-level `action` APIs.

### Option C: static analysis + partial execution

Parse chart templates, `define` blocks, `include` calls, and values references, then combine that with one real render. This can produce excellent “explain why this value happened” output without requiring a true debugger.

For a personal project, **Option C is probably the best MVP**.

## What I would build first

I would not start with a full interactive debugger. I’d build a CLI that runs **instead of Helm** for debug sessions, like:

```bash
helm-debug render ./chart -f values.yaml
helm-debug trace ./chart --template charts/spire-agent/templates/daemonset.yaml --field image
helm-debug graph ./chart
```

### MVP features

Render manifests through Helm SDK, then add:

* **Merged values view**
* **Template/helper call graph**
* **Searchable mapping of rendered field → source template**
* **Trace mode for one helper**
* **“Why this value?” explanation for common patterns**
* detection of `default`, `coalesce`, `tpl`, `include`

That would already be useful.

## Would Helm call your tool?

Not by default. Helm has plugins and post-renderers, but those are different mechanisms. Helm’s post-rendering feature is for modifying or validating already-rendered manifests before install, not for stepping through template execution. ([v3-1-0.helm.sh][5])

So if you want real chart-debug behavior, users would normally run:

```bash
helm-debug template ...
```

rather than:

```bash
helm template ...
```

You could later add:

* a Helm plugin front-end
* a VS Code extension
* a TUI

But the engine should be its own binary.

## A good architecture

### `cmd/helm-debug`

CLI and flags

### `internal/runner`

Loads chart, merges values, invokes Helm SDK actions

### `internal/analyzer`

Static analysis of:

* `define`
* `include`
* `template`
* `tpl`
* `.Values.*`
* `default`
* `coalesce`

### `internal/tracer`

Tracks:

* helper calls
* scope changes
* value lineage where inferable

### `internal/output`

Formats:

* plain text
* JSON
* maybe HTML/TUI later

## One caution

When rendering charts through Helm or the Helm SDK, template functions like `getHostByName` can perform DNS lookups during rendering. That’s a known Helm vulnerability class and worth being careful about if your debugger renders untrusted charts. ([Go Packages][6])

## My recommendation

Build it as a **separate CLI that runs instead of Helm for debugging**, powered by Helm’s Go SDK under the hood. That gives you full control, avoids fighting the Helm CLI UX, and makes the project much more interesting than a thin plugin wrapper. ([Helm][1])

A very strong first milestone would be:

1. `helm-debug template` using Helm SDK
2. print merged values and rendered manifests
3. add static helper graph
4. add `trace <helper-name>`
5. add `explain <rendered-field>`

That’s already a serious and useful project.

[1]: https://helm.sh/docs/sdk/gosdk/?utm_source=chatgpt.com "Introduction"
[2]: https://pkg.go.dev/helm.sh/helm/v3/pkg/action?utm_source=chatgpt.com "action package - helm.sh/helm/v3/pkg/action"
[3]: https://helm.sh/docs/sdk/examples/?utm_source=chatgpt.com "Examples"
[4]: https://pkg.go.dev/helm.sh/helm/v3/pkg/chartutil?utm_source=chatgpt.com "chartutil package - helm.sh/helm/v3/pkg/chartutil"
[5]: https://v3-1-0.helm.sh/docs/topics/advanced/?utm_source=chatgpt.com "Advanced Helm Techniques"
[6]: https://pkg.go.dev/vuln/GO-2023-1547?utm_source=chatgpt.com "Vulnerability Report: GO-2023-1547"
