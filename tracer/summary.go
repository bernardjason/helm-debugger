package tracer

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/bernardjason/helm-debugger/cmd"
	"helm.sh/helm/v3/pkg/chart/loader"
)

type SummarySession struct {
	graph   map[string]graphNode
	roots   []string
	sources map[string]string
}

func NewSummarySession(cliOptions cmd.Options) (*SummarySession, error) {
	ch, err := loader.Load(cliOptions.Chart)
	if err != nil {
		return nil, err
	}

	graph, roots, sources, err := buildCallGraph(ch)
	if err != nil {
		return nil, err
	}

	return &SummarySession{
		graph:   graph,
		roots:   roots,
		sources: sources,
	}, nil
}

func (s *SummarySession) SummaryForTemplate(templateName, fieldPath string) string {
	if templateName == "" {
		return "Move the cursor onto a rendered manifest line under a '# Source:' header."
	}

	targets := selectTraceTargets(templateName, "", s.graph, s.roots)
	if len(targets) == 0 {
		return fmt.Sprintf("No trace target matched %q.", templateName)
	}

	fieldName := selectedFieldName(fieldPath)
	var out strings.Builder
	for i, target := range targets {
		if i > 0 {
			out.WriteString("\n\n")
		}
		node := s.graph[target]
		fmt.Fprintf(&out, "%s (%s)\n", node.Name, node.Source)
		if fieldPath != "" {
			fmt.Fprintf(&out, "Selected field: %s\n", fieldPath)
		}
		if sourceLine, ok := s.SourceLineForTemplate(target, fieldPath); ok {
			fmt.Fprintf(&out, "Source line: %s:%d\n", target, sourceLine)
			// fmt.Fprintf(&out, "Source text: %f\n", sourceLine.text)
		}

		ctx := traceContext{ChartPrefix: chartValuePrefix(target), DotExpr: "."}
		matching := filterPathsByField(inferOverridePaths(node.ValueRefs, ctx), fieldName)
		if len(matching) > 0 {
			out.WriteString("matching values:\n")
			writePaths(&out, "  ", matching)
		}

		allValues := inferOverridePaths(node.ValueRefs, ctx)
		if len(allValues) > 0 {
			out.WriteString("template values:\n")
			writePaths(&out, "  ", allValues)
		}

		writeCallsSummary(&out, target, "  ", s.graph, s.sources, map[string]bool{target: true}, ctx, fieldName)
	}

	if out.Len() == 0 {
		return fmt.Sprintf("No trace details found for %q.", templateName)
	}
	return strings.TrimSpace(out.String())
}

type sourceLineMatch struct {
	line int
	text string
}

func (s *SummarySession) SourceLineForTemplate(templateName, fieldPath string) (int, bool) {
	match, ok := s.exactSourceLine(templateName, fieldPath)
	if !ok {
		return 0, false
	}
	return match.line, true
}

func (s *SummarySession) exactSourceLine(templateName, fieldPath string) (sourceLineMatch, bool) {
	source, ok := s.sources[templateName]
	if !ok {
		return sourceLineMatch{}, false
	}
	lines := strings.Split(source, "\n")
	keys := candidateFieldKeys(fieldPath)
	if len(keys) == 0 {
		return firstNonEmptySourceLine(lines)
	}

	bestIndex := -1
	bestScore := -1
	for idx, line := range lines {
		score := scoreSourceLine(line, idx, lines, keys)
		if score > bestScore {
			bestScore = score
			bestIndex = idx
		}
	}
	if bestIndex == -1 || bestScore <= 0 {
		return sourceLineMatch{}, false
	}
	return sourceLineMatch{line: bestIndex + 1, text: strings.TrimSpace(lines[bestIndex])}, true
}

func firstNonEmptySourceLine(lines []string) (sourceLineMatch, bool) {
	for idx, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		return sourceLineMatch{line: idx + 1, text: trimmed}, true
	}
	return sourceLineMatch{}, false
}

func candidateFieldKeys(fieldPath string) []string {
	if fieldPath == "" {
		return nil
	}
	parts := strings.Split(fieldPath, ".")
	keys := make([]string, 0, len(parts))
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.TrimSuffix(parts[i], "[]")
		if part != "" {
			keys = append(keys, part)
		}
	}
	return keys
}

func scoreSourceLine(line string, lineIndex int, lines []string, keys []string) int {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return 0
	}

	leafPattern := regexp.MustCompile(`(^|[[:space:]-])` + regexp.QuoteMeta(keys[0]) + `\s*:`)
	if !leafPattern.MatchString(line) {
		return 0
	}

	score := 100
	for depth := 1; depth < len(keys); depth++ {
		ancestorPattern := regexp.MustCompile(`(^|[[:space:]-])` + regexp.QuoteMeta(keys[depth]) + `\s*:`)
		for back := lineIndex - 1; back >= 0 && back >= lineIndex-40; back-- {
			if ancestorPattern.MatchString(lines[back]) {
				score += 20 - depth
				break
			}
		}
	}
	return score
}

func writeCallsSummary(out *strings.Builder, name, indent string, graph map[string]graphNode, sources map[string]string, stack map[string]bool, ctx traceContext, fieldName string) {
	node, ok := graph[name]
	if !ok {
		return
	}

	for _, edge := range node.Calls {
		line := fmt.Sprintf("%s%s -> %s", indent, edge.Kind, edge.Target)
		if edge.Dynamic {
			line += " [dynamic]"
		}
		if child, ok := graph[edge.Target]; ok && child.Source != "" {
			line += fmt.Sprintf(" (%s)", child.Source)
		}
		fmt.Fprintln(out, line)
		if edge.Location != "" {
			fmt.Fprintf(out, "%s  at %s\n", indent, edge.Location)
		}
		if edge.Context != "" {
			fmt.Fprintf(out, "%s  %s\n", indent, edge.Context)
		}
		if snippet := sourceSnippet(edge.Location, sources); snippet != "" {
			fmt.Fprintf(out, "%s  source: %s\n", indent, snippet)
		}

		nextCtx := deriveTraceContext(edge, ctx)
		writeEdgeValueSummary(out, indent+"  ", edge, ctx, fieldName)
		if child, ok := graph[edge.Target]; ok {
			writeNodeValueSummary(out, indent+"  ", child.ValueRefs, nextCtx, fieldName)
		}

		if stack[edge.Target] {
			fmt.Fprintf(out, "%s  cycle\n", indent)
			continue
		}
		if _, ok := graph[edge.Target]; ok {
			stack[edge.Target] = true
			writeCallsSummary(out, edge.Target, indent+"  ", graph, sources, stack, nextCtx, fieldName)
			delete(stack, edge.Target)
		}
	}
}

func writeNodeValueSummary(out *strings.Builder, indent string, refs []string, ctx traceContext, fieldName string) {
	paths := inferOverridePaths(refs, ctx)
	matching := filterPathsByField(paths, fieldName)
	if len(matching) > 0 {
		fmt.Fprintf(out, "%smatching inferred values:\n", indent)
		writePaths(out, indent+"  ", matching)
	}
	if len(paths) > 0 {
		fmt.Fprintf(out, "%sinferred values:\n", indent)
		writePaths(out, indent+"  ", paths)
	}
}

func writeEdgeValueSummary(out *strings.Builder, indent string, edge callEdge, ctx traceContext, fieldName string) {
	candidateRefs := make([]string, 0, len(edge.Bindings)+1)
	if edge.DataExpr != "" && edge.DataExpr != "." {
		candidateRefs = append(candidateRefs, edge.DataExpr)
	}
	for _, value := range edge.Bindings {
		candidateRefs = append(candidateRefs, value)
	}
	candidateRefs = append(candidateRefs, collectInlineValueRefs(edge.Context)...)

	paths := inferOverridePaths(candidateRefs, ctx)
	matching := filterPathsByField(paths, fieldName)
	if len(matching) > 0 {
		fmt.Fprintf(out, "%smatching passed values:\n", indent)
		writePaths(out, indent+"  ", matching)
	}
	if len(paths) > 0 {
		fmt.Fprintf(out, "%spassed values:\n", indent)
		writePaths(out, indent+"  ", paths)
	}
}

func writePaths(out *strings.Builder, indent string, paths []string) {
	for _, overridePath := range paths {
		fmt.Fprintf(out, "%spath: %s\n", indent, overridePath)
		for _, line := range yamlSnippetLines(overridePath) {
			fmt.Fprintf(out, "%s%s\n", indent, line)
		}
	}
}

func filterPathsByField(paths []string, fieldName string) []string {
	if fieldName == "" {
		return nil
	}
	seen := make(map[string]bool)
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		last := lastPathSegment(path)
		if last != fieldName || seen[path] {
			continue
		}
		seen[path] = true
		filtered = append(filtered, path)
	}
	sort.Strings(filtered)
	return filtered
}

func selectedFieldName(fieldPath string) string {
	if fieldPath == "" {
		return ""
	}
	parts := strings.Split(fieldPath, ".")
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.TrimSuffix(parts[i], "[]")
		if part != "" {
			return part
		}
	}
	return ""
}

func lastPathSegment(path string) string {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSuffix(parts[len(parts)-1], "[]")
}
