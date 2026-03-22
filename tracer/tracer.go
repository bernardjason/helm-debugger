package tracer

import (
	"fmt"
	"log"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"text/template/parse"

	"github.com/Masterminds/sprig/v3"
	"github.com/bernardjason/helm-debugger/cmd"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
)

type canBeRendered struct {
	tpl      string
	vals     chartutil.Values
	basePath string
}

type callEdge struct {
	Kind     string
	Target   string
	Dynamic  bool
	Location string
	Context  string
}

type graphNode struct {
	Name   string
	Source string
	Calls  []callEdge
}

func TraceTemplates(cliOptions cmd.Options) {
	ch, err := loader.Load(cliOptions.Chart)
	if err != nil {
		log.Fatal(err)
	}

	graph, roots, sources, err := buildCallGraph(ch)
	if err != nil {
		log.Fatal(err)
	}

	targets := selectTraceTargets(cliOptions.Template, graph, roots)
	if len(targets) == 0 {
		log.Fatalf("no templates matched %q", cliOptions.Template)
	}

	for i, target := range targets {
		if i > 0 {
			fmt.Println()
		}

		node := graph[target]
		if cliOptions.Template != "" || cliOptions.Template == node.Source {
			fmt.Printf("%s (%s)\n", node.Name, node.Source)
			printCalls(target, "  ", graph, sources, map[string]bool{target: true})
		}
	}
}

func buildCallGraph(ch *chart.Chart) (map[string]graphNode, []string, map[string]string, error) {
	templates := allTemplates(ch, chartutil.Values{
		"Values":       chartutil.Values{},
		"Release":      chartutil.Values{},
		"Capabilities": chartutil.Values{},
	})

	t := template.New("gotpl")
	t.Option("missingkey=zero")
	t.Funcs(templateFuncMap())

	keys := sortTemplates(templates)
	for _, filename := range keys {
		if _, err := t.New(filename).Parse(templates[filename].tpl); err != nil {
			return nil, nil, nil, err
		}
	}

	graph := make(map[string]graphNode)
	sources := make(map[string]string, len(templates))
	for filename, renderable := range templates {
		sources[filename] = renderable.tpl
	}
	for _, template := range t.Templates() {
		if template.Tree == nil || template.Tree.Root == nil {
			continue
		}

		graph[template.Name()] = graphNode{
			Name:   template.Name(),
			Source: template.Tree.ParseName,
			Calls:  collectCalls(template.Tree, template.Tree.Root),
		}
	}

	var roots []string
	for _, filename := range keys {
		if strings.HasPrefix(path.Base(filename), "_") {
			continue
		}
		if _, ok := graph[filename]; ok {
			roots = append(roots, filename)
		}
	}

	return graph, roots, sources, nil
}

func selectTraceTargets(filter string, graph map[string]graphNode, roots []string) []string {
	if filter == "" {
		return roots
	}

	if _, ok := graph[filter]; ok {
		return []string{filter}
	}

	var matches []string
	for _, root := range roots {
		if strings.HasSuffix(root, filter) {
			matches = append(matches, root)
		}
	}
	return matches
}

func printCalls(name, indent string, graph map[string]graphNode, sources map[string]string, stack map[string]bool) {
	node, ok := graph[name]
	if !ok {
		return
	}

	for _, edge := range node.Calls {
		line := fmt.Sprintf("%s%s -> %s", indent, edge.Kind, edge.Target)
		if edge.Dynamic {
			line += " [dynamic]" // variables at render time
		}
		if child, ok := graph[edge.Target]; ok && child.Source != "" {
			line += fmt.Sprintf(" (%s)", child.Source)
		}
		fmt.Println(line)
		if edge.Location != "" {
			fmt.Printf("%s  at %s\n", indent, edge.Location)
		}
		if edge.Context != "" {
			fmt.Printf("%s  %s\n", indent, edge.Context)
		}
		if snippet := sourceSnippet(edge.Location, sources); snippet != "" {
			fmt.Printf("%s  source: %s\n", indent, snippet)
		}

		if stack[edge.Target] {
			fmt.Printf("%s  cycle\n", indent)
			continue
		}

		if _, ok := graph[edge.Target]; ok {
			stack[edge.Target] = true
			printCalls(edge.Target, indent+"  ", graph, sources, stack)
			delete(stack, edge.Target)
		}
	}
}

func collectCalls(tree *parse.Tree, root parse.Node) []callEdge {
	edges := make([]callEdge, 0)
	seen := make(map[string]bool)
	walkNode(tree, root, func(edge callEdge) {
		key := edge.Kind + "\x00" + edge.Target + "\x00" + edge.Location
		if seen[key] {
			return
		}
		seen[key] = true
		edges = append(edges, edge)
	})
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Kind == edges[j].Kind {
			return edges[i].Target < edges[j].Target
		}
		return edges[i].Kind < edges[j].Kind
	})
	return edges
}

func walkNode(tree *parse.Tree, node parse.Node, emit func(callEdge)) {
	switch n := node.(type) {
	case *parse.ListNode:
		for _, child := range n.Nodes {
			walkNode(tree, child, emit)
		}
	case *parse.ActionNode:
		walkPipe(tree, n.Pipe, emit)
	case *parse.TemplateNode:
		location, context := nodeErrorContext(tree, n)
		emit(callEdge{Kind: "template", Target: n.Name, Location: location, Context: context})
		if n.Pipe != nil {
			walkPipe(tree, n.Pipe, emit)
		}
	case *parse.IfNode:
		walkPipe(tree, n.Pipe, emit)
		walkNode(tree, n.List, emit)
		if n.ElseList != nil {
			walkNode(tree, n.ElseList, emit)
		}
	case *parse.RangeNode:
		walkPipe(tree, n.Pipe, emit)
		walkNode(tree, n.List, emit)
		if n.ElseList != nil {
			walkNode(tree, n.ElseList, emit)
		}
	case *parse.WithNode:
		walkPipe(tree, n.Pipe, emit)
		walkNode(tree, n.List, emit)
		if n.ElseList != nil {
			walkNode(tree, n.ElseList, emit)
		}
	}
}

func walkPipe(tree *parse.Tree, pipe *parse.PipeNode, emit func(callEdge)) {
	if pipe == nil {
		return
	}
	for _, cmd := range pipe.Cmds {
		walkCommand(tree, cmd, emit)
	}
}

func walkCommand(tree *parse.Tree, cmd *parse.CommandNode, emit func(callEdge)) {
	if cmd == nil {
		return
	}

	if fn := firstIdentifier(cmd); fn != "" {
		location, context := nodeErrorContext(tree, cmd)
		switch fn {
		case "include":
			target, dynamic := stringArg(cmd, 1)
			emit(callEdge{Kind: "include", Target: target, Dynamic: dynamic, Location: location, Context: context})
		case "tpl":
			target, dynamic := stringArg(cmd, 1)
			if target == "" {
				target = "<tpl>"
			}
			emit(callEdge{Kind: "tpl", Target: target, Dynamic: dynamic, Location: location, Context: context})
		}
	}

	for _, arg := range cmd.Args {
		switch n := arg.(type) {
		case *parse.PipeNode:
			walkPipe(tree, n, emit)
		case *parse.ChainNode:
			if n.Node != nil {
				walkNode(tree, n.Node, emit)
			}
		}
	}
}

func firstIdentifier(cmd *parse.CommandNode) string {
	if len(cmd.Args) == 0 {
		return ""
	}
	if ident, ok := cmd.Args[0].(*parse.IdentifierNode); ok {
		return ident.Ident
	}
	return ""
}

func stringArg(cmd *parse.CommandNode, index int) (string, bool) {
	if len(cmd.Args) <= index {
		return "<dynamic>", true
	}
	if arg, ok := cmd.Args[index].(*parse.StringNode); ok {
		return arg.Text, false
	}
	return "<dynamic>", true
}

func nodeErrorContext(tree *parse.Tree, node parse.Node) (string, string) {
	if tree == nil || node == nil {
		return "", ""
	}
	return tree.ErrorContext(node)
}

func sourceSnippet(location string, sources map[string]string) string {
	filename, line, ok := parseLocation(location)
	if !ok {
		return ""
	}

	content, ok := sources[filename]
	if !ok {
		return ""
	}

	lines := strings.Split(content, "\n")
	if line <= 0 || line > len(lines) {
		return ""
	}

	return strings.TrimSpace(lines[line-1])
}

func parseLocation(location string) (string, int, bool) {
	lastColon := strings.LastIndex(location, ":")
	if lastColon == -1 {
		return "", 0, false
	}

	secondLastColon := strings.LastIndex(location[:lastColon], ":")
	if secondLastColon == -1 {
		return "", 0, false
	}

	filename := location[:secondLastColon]
	lineText := location[secondLastColon+1 : lastColon]
	line := 0
	for _, ch := range lineText {
		if ch < '0' || ch > '9' {
			return "", 0, false
		}
		line = line*10 + int(ch-'0')
	}
	if line == 0 {
		return "", 0, false
	}

	return filename, line, true
}

func templateFuncMap() template.FuncMap {
	f := sprig.TxtFuncMap()
	delete(f, "env")
	delete(f, "expandenv")

	extra := template.FuncMap{
		"include":  func(string, interface{}) string { return "" },
		"tpl":      func(string, interface{}) interface{} { return "" },
		"required": func(string, interface{}) (interface{}, error) { return "", nil },
		"lookup": func(string, string, string, string) (map[string]interface{}, error) {
			return map[string]interface{}{}, nil
		},
		"toYaml":        func(interface{}) string { return "" },
		"toYamlPretty":  func(interface{}) string { return "" },
		"fromYaml":      func(string) map[string]interface{} { return map[string]interface{}{} },
		"fromYamlArray": func(string) []interface{} { return []interface{}{} },
		"toJson":        func(interface{}) string { return "" },
		"fromJson":      func(string) map[string]interface{} { return map[string]interface{}{} },
		"fromJsonArray": func(string) []interface{} { return []interface{}{} },
		"toToml":        func(interface{}) string { return "" },
		"fromToml":      func(string) map[string]interface{} { return map[string]interface{}{} },
	}

	for name, fn := range extra {
		f[name] = fn
	}

	return f
}

func sortTemplates(templates map[string]canBeRendered) []string {
	keys := make([]string, 0, len(templates))
	for key := range templates {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		ca, cb := strings.Count(a, "/"), strings.Count(b, "/")
		if ca == cb {
			return a > b
		}
		return ca > cb
	})
	return keys
}

func allTemplates(c *chart.Chart, vals chartutil.Values) map[string]canBeRendered {
	templates := make(map[string]canBeRendered)
	recurseOverTemplates(c, templates, vals)
	return templates
}

func recurseOverTemplates(c *chart.Chart, templates map[string]canBeRendered, vals chartutil.Values) map[string]interface{} {
	subCharts := make(map[string]interface{})
	chartMetaData := struct {
		chart.Metadata
		IsRoot bool
	}{*c.Metadata, c.IsRoot()}

	next := map[string]interface{}{
		"Chart":        chartMetaData,
		"Files":        newFiles(c.Files),
		"Release":      vals["Release"],
		"Capabilities": vals["Capabilities"],
		"Values":       make(chartutil.Values),
		"Subcharts":    subCharts,
	}

	if c.IsRoot() {
		next["Values"] = vals["Values"]
	} else if vs, err := vals.Table("Values." + c.Name()); err == nil {
		next["Values"] = vs
	}

	for _, child := range c.Dependencies() {
		subCharts[child.Name()] = recurseOverTemplates(child, templates, next)
	}

	newParentID := c.ChartFullPath()
	for _, tpl := range c.Templates {
		if tpl == nil || !isTemplateValid(c, tpl.Name) {
			continue
		}

		templates[path.Join(newParentID, tpl.Name)] = canBeRendered{
			tpl:      string(tpl.Data),
			vals:     next,
			basePath: path.Join(newParentID, "templates"),
		}
	}

	return next
}

func isTemplateValid(ch *chart.Chart, templateName string) bool {
	if strings.EqualFold(ch.Metadata.Type, "library") {
		return strings.HasPrefix(filepath.Base(templateName), "_")
	}
	return true
}

type files map[string][]byte

func newFiles(from []*chart.File) files {
	files := make(map[string][]byte)
	for _, f := range from {
		files[f.Name] = f.Data
	}
	return files
}
