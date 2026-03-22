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
	DataExpr string
	Bindings map[string]string
}

type graphNode struct {
	Name      string
	Source    string
	Calls     []callEdge
	ValueRefs []string
}

type traceContext struct {
	ChartPrefix string
	DotExpr     string
	Bindings    map[string]string
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

	targets := selectTraceTargets(cliOptions.Template, cliOptions.Helper, graph, roots)
	if len(targets) == 0 {
		if cliOptions.Helper != "" {
			log.Fatalf("no helpers matched %q", cliOptions.Helper)
		}
		log.Fatalf("no templates matched %q", cliOptions.Template)
	}

	for i, target := range targets {
		if i > 0 {
			fmt.Println()
		}

		node := graph[target]
		fmt.Printf("%s (%s)\n", node.Name, node.Source)
		ctx := traceContext{
			ChartPrefix: chartValuePrefix(target),
			DotExpr:     ".",
		}
		printValueHints("  ", node.ValueRefs, ctx, cliOptions.TraceValues)
		printCalls(target, "  ", graph, sources, map[string]bool{target: true}, ctx, cliOptions.TraceValues)
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
			Name:      template.Name(),
			Source:    template.Tree.ParseName,
			Calls:     collectCalls(template.Tree, template.Tree.Root),
			ValueRefs: collectValueRefs(template.Tree.Root),
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

func selectTraceTargets(templateFilter, helperFilter string, graph map[string]graphNode, roots []string) []string {
	if helperFilter != "" {
		return matchGraphTargets(helperFilter, graph)
	}

	filter := templateFilter
	if filter == "" {
		return roots
	}

	var matches []string
	for _, root := range roots {
		if strings.HasSuffix(root, filter) {
			matches = append(matches, root)
		}
	}
	if len(matches) == 0 {
		return matchGraphTargets(filter, graph)
	}
	return matches
}

func matchGraphTargets(filter string, graph map[string]graphNode) []string {
	if _, ok := graph[filter]; ok {
		return []string{filter}
	}

	var matches []string
	for name := range graph {
		if strings.HasSuffix(name, filter) {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	return matches
}

func printCalls(name, indent string, graph map[string]graphNode, sources map[string]string, stack map[string]bool, ctx traceContext, showValues bool) {
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
		nextCtx := deriveTraceContext(edge, ctx)
		if child, ok := graph[edge.Target]; ok {
			printValueHints(indent+"  ", child.ValueRefs, nextCtx, showValues)
		}

		if stack[edge.Target] {
			fmt.Printf("%s  cycle\n", indent)
			continue
		}

		if _, ok := graph[edge.Target]; ok {
			stack[edge.Target] = true
			printCalls(edge.Target, indent+"  ", graph, sources, stack, nextCtx, showValues)
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
		dataExpr, bindings := analyzePipeData(n.Pipe)
		emit(callEdge{Kind: "template", Target: n.Name, Location: location, Context: context, DataExpr: dataExpr, Bindings: bindings})
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
			dataExpr, bindings := analyzeCommandData(cmd, 2)
			emit(callEdge{Kind: "include", Target: target, Dynamic: dynamic, Location: location, Context: context, DataExpr: dataExpr, Bindings: bindings})
		case "tpl":
			target, dynamic := stringArg(cmd, 1)
			if target == "" {
				target = "<tpl>"
			}
			dataExpr, bindings := analyzeCommandData(cmd, 2)
			emit(callEdge{Kind: "tpl", Target: target, Dynamic: dynamic, Location: location, Context: context, DataExpr: dataExpr, Bindings: bindings})
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

func collectValueRefs(root parse.Node) []string {
	refs := make([]string, 0)
	seen := make(map[string]bool)
	var walk func(parse.Node)
	walk = func(node parse.Node) {
		if node == nil {
			return
		}
		switch n := node.(type) {
		case *parse.ListNode:
			if n == nil {
				return
			}
			for _, child := range n.Nodes {
				walk(child)
			}
		case *parse.ActionNode:
			if n == nil {
				return
			}
			walkPipeRefs(n.Pipe, &refs, seen)
		case *parse.TemplateNode:
			if n == nil {
				return
			}
			walkPipeRefs(n.Pipe, &refs, seen)
		case *parse.IfNode:
			if n == nil {
				return
			}
			walkPipeRefs(n.Pipe, &refs, seen)
			walk(n.List)
			walk(n.ElseList)
		case *parse.RangeNode:
			if n == nil {
				return
			}
			walkPipeRefs(n.Pipe, &refs, seen)
			walk(n.List)
			walk(n.ElseList)
		case *parse.WithNode:
			if n == nil {
				return
			}
			walkPipeRefs(n.Pipe, &refs, seen)
			walk(n.List)
			walk(n.ElseList)
		}
	}
	walk(root)
	sort.Strings(refs)
	return refs
}

func walkPipeRefs(pipe *parse.PipeNode, refs *[]string, seen map[string]bool) {
	if pipe == nil {
		return
	}
	for _, cmd := range pipe.Cmds {
		for _, arg := range cmd.Args {
			walkArgRefs(arg, refs, seen)
		}
	}
}

func walkArgRefs(node parse.Node, refs *[]string, seen map[string]bool) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *parse.FieldNode:
		if n == nil {
			return
		}
		addValueRef(n.String(), refs, seen)
	case *parse.VariableNode:
		if n == nil {
			return
		}
		addValueRef(n.String(), refs, seen)
	case *parse.ChainNode:
		if n == nil {
			return
		}
		addValueRef(n.String(), refs, seen)
		if n.Node != nil {
			walkArgRefs(n.Node, refs, seen)
		}
	case *parse.PipeNode:
		if n == nil {
			return
		}
		walkPipeRefs(n, refs, seen)
	case *parse.CommandNode:
		if n == nil {
			return
		}
		for _, arg := range n.Args {
			walkArgRefs(arg, refs, seen)
		}
	}
}

func addValueRef(ref string, refs *[]string, seen map[string]bool) {
	if ref == "" {
		return
	}
	if strings.HasPrefix(ref, ".Values.") || strings.HasPrefix(ref, "$.Values.") {
		if !seen[ref] {
			seen[ref] = true
			*refs = append(*refs, ref)
		}
		return
	}
	if !strings.HasPrefix(ref, ".") {
		return
	}
	top := strings.TrimPrefix(ref, ".")
	if top == "" {
		return
	}
	if strings.Contains(top, ".") {
		top = top[:strings.Index(top, ".")]
	}
	switch top {
	case "Chart", "Release", "Capabilities", "Template", "Files", "Subcharts":
		return
	}
	if !seen[ref] {
		seen[ref] = true
		*refs = append(*refs, ref)
	}
}

func analyzeCommandData(cmd *parse.CommandNode, index int) (string, map[string]string) {
	if cmd == nil || len(cmd.Args) <= index {
		return ".", nil
	}
	return analyzeDataNode(cmd.Args[index])
}

func analyzePipeData(pipe *parse.PipeNode) (string, map[string]string) {
	if pipe == nil || len(pipe.Cmds) == 0 {
		return ".", nil
	}
	if len(pipe.Cmds) == 1 {
		return analyzeCommandLike(pipe.Cmds[0])
	}
	return pipe.String(), nil
}

func analyzeDataNode(node parse.Node) (string, map[string]string) {
	switch n := node.(type) {
	case *parse.PipeNode:
		return analyzePipeData(n)
	case *parse.CommandNode:
		return analyzeCommandLike(n)
	default:
		if node == nil {
			return ".", nil
		}
		return node.String(), nil
	}
}

func analyzeCommandLike(cmd *parse.CommandNode) (string, map[string]string) {
	if cmd == nil {
		return ".", nil
	}
	if firstIdentifier(cmd) == "dict" {
		return ".", parseDictBindings(cmd)
	}
	if len(cmd.Args) == 1 {
		return cmd.Args[0].String(), nil
	}
	return cmd.String(), nil
}

func parseDictBindings(cmd *parse.CommandNode) map[string]string {
	bindings := make(map[string]string)
	for i := 1; i+1 < len(cmd.Args); i += 2 {
		keyNode, ok := cmd.Args[i].(*parse.StringNode)
		if !ok {
			continue
		}
		bindings[keyNode.Text] = cmd.Args[i+1].String()
	}
	if len(bindings) == 0 {
		return nil
	}
	return bindings
}

func deriveTraceContext(edge callEdge, parent traceContext) traceContext {
	child := traceContext{
		ChartPrefix: parent.ChartPrefix,
		DotExpr:     ".",
	}
	if edge.DataExpr != "" {
		if resolved, ok := resolveExpr(edge.DataExpr, parent); ok {
			child.DotExpr = resolved
		} else {
			child.DotExpr = edge.DataExpr
		}
	}
	if len(edge.Bindings) > 0 {
		child.DotExpr = "."
		child.Bindings = make(map[string]string, len(edge.Bindings))
		for key, value := range edge.Bindings {
			if resolved, ok := resolveExpr(value, parent); ok {
				child.Bindings[key] = resolved
			} else {
				child.Bindings[key] = value
			}
		}
	}
	return child
}

func printValueHints(indent string, refs []string, ctx traceContext, enabled bool) {
	if !enabled {
		return
	}
	paths := inferOverridePaths(refs, ctx)
	if len(paths) == 0 {
		return
	}
	fmt.Printf("%sinferred values:\n", indent)
	for _, overridePath := range paths {
		fmt.Printf("%s  path: %s\n", indent, overridePath)
		for _, line := range yamlSnippetLines(overridePath) {
			fmt.Printf("%s  %s\n", indent, line)
		}
	}
}

func inferOverridePaths(refs []string, ctx traceContext) []string {
	seen := make(map[string]bool)
	paths := make([]string, 0)
	for _, ref := range refs {
		resolved, ok := resolveExpr(ref, ctx)
		if !ok {
			continue
		}
		overridePath, ok := valuesExprToOverridePath(resolved, ctx.ChartPrefix)
		if !ok || seen[overridePath] {
			continue
		}
		seen[overridePath] = true
		paths = append(paths, overridePath)
	}
	sort.Strings(paths)
	return paths
}

func resolveExpr(expr string, ctx traceContext) (string, bool) {
	for range 12 {
		expr = strings.TrimSpace(expr)
		if expr == "" {
			return "", false
		}
		if strings.HasPrefix(expr, "$.") {
			expr = "." + strings.TrimPrefix(expr, "$.")
		}
		if expr == "." {
			if ctx.DotExpr == "" {
				return ".", true
			}
			if ctx.DotExpr == expr {
				return expr, true
			}
			expr = ctx.DotExpr
			continue
		}
		if strings.HasPrefix(expr, ".Values") {
			return expr, true
		}
		if !strings.HasPrefix(expr, ".") {
			return expr, true
		}
		path := strings.TrimPrefix(expr, ".")
		first, rest := splitPath(path)
		if base, ok := ctx.Bindings[first]; ok {
			expr = joinExpr(base, rest)
			continue
		}
		if ctx.DotExpr != "" && ctx.DotExpr != "." {
			expr = joinExpr(ctx.DotExpr, path)
			continue
		}
		return expr, true
	}
	return expr, false
}

func splitPath(path string) (string, string) {
	if path == "" {
		return "", ""
	}
	index := strings.Index(path, ".")
	if index == -1 {
		return path, ""
	}
	return path[:index], path[index+1:]
}

func joinExpr(base, rest string) string {
	base = strings.TrimSpace(base)
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return base
	}
	if base == "." {
		return "." + rest
	}
	return strings.TrimRight(base, ".") + "." + rest
}

func valuesExprToOverridePath(expr, chartPrefix string) (string, bool) {
	path := ""
	switch {
	case expr == ".Values":
		path = ""
	case strings.HasPrefix(expr, ".Values."):
		path = strings.TrimPrefix(expr, ".Values.")
	default:
		return "", false
	}
	if chartPrefix == "" {
		return path, path != ""
	}
	if path == "" {
		return chartPrefix, true
	}
	return chartPrefix + "." + path, true
}

func chartValuePrefix(templateName string) string {
	parts := strings.Split(templateName, "/")
	prefix := make([]string, 0)
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "charts" && i+1 < len(parts) {
			prefix = append(prefix, parts[i+1])
		}
	}
	return strings.Join(prefix, ".")
}

func yamlSnippetLines(path string) []string {
	if path == "" {
		return nil
	}
	parts := strings.Split(path, ".")
	lines := make([]string, 0, len(parts))
	for i, part := range parts {
		indent := strings.Repeat("  ", i)
		value := ":"
		if i == len(parts)-1 {
			value = ": <override>"
		}
		lines = append(lines, indent+part+value)
	}
	return lines
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
