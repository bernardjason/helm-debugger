# helm-debugger

`helm-debugger` is a small Go tool for exploring how a Helm chart renders.

It wraps the Helm Go SDK and provides:
- a TUI for rendered manifests, source templates, values editing, trace output, and errors
- trace mode for template and helper relationships
- live chart rerendering from an editable `values.yaml` buffer
- a small sample chart in [`./chart`](./chart)

## What It Does

The TUI is designed to answer three practical questions:
- What did Helm render?
- Which template file did this rendered line come from?
- Which values are likely involved in producing this output?

The app renders the chart with Helm, shows the expanded manifests, lets you move through the output, and keeps the source and trace panes in sync.

## Repository Layout

- `cmd/`: CLI flag parsing and Helm action setup
- `tui/`: terminal UI layout, views, and runtime behavior
- `tracer/`: helper/template tracing and summary generation
- `view/`: Helm rendering helpers
- `chart/`: sample chart used for local testing
- `spire/`: larger chart tree for debugging against a more realistic example

## Requirements

- Go
- A terminal that supports `tview` / `tcell`

## Run

By default the tool opens the TUI against `./chart`:

```bash
go run .
```

Use a different chart:

```bash
go run . --chart ./spire
```

Render a specific template path:

```bash
go run . --chart ./spire --show-only spire/charts/spire-agent/templates/daemonset.yaml
```

Load a values file on startup:

```bash
go run . --chart ./spire -f myvalues.yaml
```

Use trace mode instead of the TUI:

```bash
go run . trace --chart ./spire --helper spire-agent.someHelper
```

## CLI Flags

- `--chart`: chart directory to load. Defaults to `./chart`
- `--show-only`: restrict rendered output to one template path
- `--helper`: select a specific helper/template definition for trace mode
- `--trace`: show template/helper call graph instead of the TUI
- `--trace-values`: include inferred values override paths in trace output
- `-f`, `--values`: values file to load

## TUI Layout

The UI has a top menu and five working panes:
- expanded rendered manifests
- template source
- trace values
- editable `values.yaml`
- errors / Helm warnings

## TUI Controls

- `Tab`: cycle focus between rendered output, source, and values editor
- `Shift-Tab`: move focus backwards
- Arrow keys, `Page Up`, `Page Down`, `Home`, `End`: move in the focused pane
- `Enter` on the template source pane: sync the rendered pane to the selected source line
- `Ctrl-O`: open an existing values file into the editor
- `Ctrl-N`: create a new values file and switch the editor to it
- `Ctrl-L`: rerender using the current editor contents
- `Ctrl-S`: save the current values editor to disk
- `Ctrl-C`: quit

Menu buttons are also mouse-clickable.

## Sample Chart

A small sample chart is included under [`chart/`](./chart). It contains:
- a `Deployment`
- a `Service`
- a `ConfigMap`
- helper templates in `_helpers.tpl`

That chart is intended as a stable local target for trying the debugger and running tests.

## Tests

Run the test suite with:

```bash
go test ./...
```

A basic rendering test lives in [`view/viewer_test.go`](./view/viewer_test.go) and checks that inline values overrides appear in the rendered manifest.

## Notes

- `Ctrl-L` is the explicit sync point for edited values. Changing the editor does not rerender automatically.
- The trace pane is useful, but parts of the value/source mapping are still heuristic rather than full execution provenance.
- The TUI needs an interactive terminal; it will not run correctly in a non-interactive shell.
