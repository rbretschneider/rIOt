package redact

import (
	"bytes"
	"errors"
	"gopkg.in/yaml.v3"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type sourceEdit struct {
	start, end int
	text       string
}
type composeVisit struct {
	node *yaml.Node
	mode int
}

func redactComposeSource(data []byte) ([]byte, int, error) {
	fail := func() ([]byte, int, error) { return nil, 0, errors.New("redaction_failed") }
	if len(data) > 1<<20 {
		return fail()
	}
	source := newComposeSource(data)
	var doc yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if dec.Decode(&doc) != nil || dec.Decode(new(yaml.Node)) != io.EOF {
		return fail()
	}
	visited, active := map[*yaml.Node]bool{}, map[*yaml.Node]bool{}
	nodes := 0
	var validate func(*yaml.Node, int) bool
	validate = func(n *yaml.Node, depth int) bool {
		if n == nil || depth > 64 || active[n] {
			return false
		}
		if visited[n] {
			return true
		}
		nodes++
		if nodes > 50000 {
			return false
		}
		active[n] = true
		if n.Kind == yaml.AliasNode {
			if !validate(n.Alias, depth+1) {
				return false
			}
		}
		if n.Kind == yaml.MappingNode {
			for i := 0; i < len(n.Content); i += 2 {
				key := n.Content[i]
				if key.Kind != yaml.ScalarNode || (key.Tag != "!!str" && key.Tag != "!!merge") {
					return false
				}
			}
		}
		for _, child := range n.Content {
			if !validate(child, depth+1) {
				return false
			}
		}
		delete(active, n)
		visited[n] = true
		return true
	}
	if !validate(&doc, 0) {
		return fail()
	}
	replacements := map[*yaml.Node]string{}
	parents := map[*yaml.Node]int{}
	flow := map[*yaml.Node]bool{}
	seen := map[composeVisit]bool{}
	resolve := func(n *yaml.Node) *yaml.Node {
		for n.Kind == yaml.AliasNode {
			n = n.Alias
		}
		return n
	}
	mark := func(n *yaml.Node, value string) {
		n = resolve(n)
		if n.Kind == yaml.ScalarNode {
			if old, ok := replacements[n]; !ok || value == "<redacted>" || old != "<redacted>" {
				replacements[n] = value
			}
		}
	}
	var hide func(*yaml.Node)
	hidden := map[*yaml.Node]bool{}
	hide = func(n *yaml.Node) {
		n = resolve(n)
		if hidden[n] {
			return
		}
		hidden[n] = true
		switch n.Kind {
		case yaml.ScalarNode:
			mark(n, "<redacted>")
		case yaml.MappingNode:
			for i := 1; i < len(n.Content); i += 2 {
				hide(n.Content[i])
			}
		default:
			for _, child := range n.Content {
				hide(child)
			}
		}
	}
	var walk func(*yaml.Node, int, int, bool)
	walk = func(n *yaml.Node, mode, parent int, inFlow bool) {
		n = resolve(n)
		id := composeVisit{n, mode}
		if seen[id] {
			return
		}
		seen[id] = true
		if _, ok := parents[n]; !ok {
			parents[n] = parent
		}
		switch n.Kind {
		case yaml.DocumentNode:
			for _, child := range n.Content {
				walk(child, 0, -1, false)
			}
		case yaml.MappingNode:
			for i := 0; i < len(n.Content); i += 2 {
				key, val := n.Content[i], n.Content[i+1]
				name := key.Value
				indent := key.Column - 1
				if mode == 1 {
					if name == "<<" {
						walk(val, 1, indent, inFlow || n.Style&yaml.FlowStyle != 0)
					} else if !SafeEnv[name] {
						hide(val)
					} else {
						walk(val, 0, indent, inFlow || n.Style&yaml.FlowStyle != 0)
					}
					continue
				}
				if name == "<<" {
					walk(val, mode, indent, inFlow || n.Style&yaml.FlowStyle != 0)
					continue
				}
				if strings.EqualFold(name, "environment") {
					walk(val, 1, indent, inFlow || n.Style&yaml.FlowStyle != 0)
					continue
				}
				if SensitiveKey(name) {
					hide(val)
					continue
				}
				if strings.EqualFold(name, "command") || strings.EqualFold(name, "entrypoint") || strings.EqualFold(name, "test") {
					walk(val, 2, indent, inFlow || n.Style&yaml.FlowStyle != 0)
					continue
				}
				walk(val, 0, indent, inFlow || n.Style&yaml.FlowStyle != 0)
			}
		case yaml.SequenceNode:
			if mode == 2 {
				args := make([]string, len(n.Content))
				all := true
				for i, child := range n.Content {
					child = resolve(child)
					if child.Kind != yaml.ScalarNode {
						all = false
						break
					}
					args[i] = child.Value
				}
				if all {
					safe, _ := Argv(args)
					for i, child := range n.Content {
						child = resolve(child)
						if safe[i] != child.Value {
							mark(child, safe[i])
						}
					}
					return
				}
			}
			for _, child := range n.Content {
				walk(child, mode, parent, inFlow || n.Style&yaml.FlowStyle != 0)
			}
		case yaml.ScalarNode:
			if mode == 1 {
				if key, value, ok := strings.Cut(n.Value, "="); ok {
					safe, _ := EnvPair(key, value)
					safe, _ = MaskText(safe, "<redacted>")
					if safe != value {
						mark(n, key+"="+safe)
					}
				}
				return
			}
			if mode == 2 {
				mark(n, "<redacted>")
				return
			}
			safe, _ := MaskText(n.Value, "<redacted>")
			if safe != n.Value {
				mark(n, safe)
			}
		}
	}
	// Record lexical context for every source node before aliases reach it under
	// a different semantic context (for example a merge into environment).
	var locations func(*yaml.Node, int, bool)
	locations = func(n *yaml.Node, parent int, inFlow bool) {
		if n.Kind == yaml.AliasNode {
			return
		}
		parents[n] = parent
		flow[n] = inFlow
		switch n.Kind {
		case yaml.MappingNode:
			for i := 0; i < len(n.Content); i += 2 {
				locations(n.Content[i+1], n.Content[i].Column-1, inFlow || n.Style&yaml.FlowStyle != 0)
			}
		case yaml.SequenceNode:
			for _, child := range n.Content {
				locations(child, source.indent(child.Line), inFlow || n.Style&yaml.FlowStyle != 0)
			}
		default:
			for _, child := range n.Content {
				locations(child, parent, inFlow)
			}
		}
	}
	locations(&doc, -1, false)
	walk(&doc, 0, -1, false)
	var edits []sourceEdit
	count := 0
	for node, replacement := range replacements {
		if node.Value == replacement {
			continue
		}
		spans, err := scalarSourceEdits(source, node, replacement, parents[node], flow[node])
		if err != nil {
			return fail()
		}
		edits = append(edits, spans...)
		count++
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var output bytes.Buffer
	output.Grow(len(data))
	cursor := 0
	for _, edit := range edits {
		if edit.start < cursor || edit.end < edit.start || edit.end > len(data) {
			return fail()
		}
		output.Write(data[cursor:edit.start])
		output.WriteString(edit.text)
		cursor = edit.end
	}
	output.Write(data[cursor:])
	masked, n := URLCreds(output.String())
	count += n
	if strings.Count(masked, "\n") != bytes.Count(data, []byte{'\n'}) {
		return fail()
	}
	return []byte(masked), count, nil
}

type composeSource struct {
	data     []byte
	lines    []int
	nonASCII map[int]bool
	columns  map[int][]int
}

func newComposeSource(data []byte) *composeSource {
	s := &composeSource{data: data, lines: []int{0}, nonASCII: map[int]bool{}, columns: map[int][]int{}}
	line := 1
	for i, ch := range data {
		if ch == '\n' {
			s.lines = append(s.lines, i+1)
			line++
		} else if ch >= utf8.RuneSelf {
			s.nonASCII[line] = true
		}
	}
	return s
}
func (s *composeSource) bounds(line int) (int, int, bool) {
	if line < 1 || line > len(s.lines) {
		return 0, 0, false
	}
	start := s.lines[line-1]
	end := len(s.data)
	if line < len(s.lines) {
		end = s.lines[line] - 1
	}
	return start, end, true
}
func (s *composeSource) indent(line int) int {
	start, end, ok := s.bounds(line)
	if !ok {
		return 0
	}
	i := start
	for i < end && (s.data[i] == ' ' || s.data[i] == '\t') {
		i++
	}
	return i - start
}
func (s *composeSource) offset(line, column int) (int, error) {
	start, end, ok := s.bounds(line)
	if !ok || column < 1 {
		return 0, errors.New("span")
	}
	if !s.nonASCII[line] {
		pos := start + column - 1
		if pos > end {
			return 0, errors.New("span")
		}
		return pos, nil
	}
	offsets := s.columns[line]
	if offsets == nil {
		for pos := start; pos < end; {
			offsets = append(offsets, pos)
			_, size := utf8.DecodeRune(s.data[pos:])
			pos += size
		}
		offsets = append(offsets, end)
		s.columns[line] = offsets
	}
	if column > len(offsets) {
		return 0, errors.New("span")
	}
	return offsets[column-1], nil
}
func scalarSourceEdits(source *composeSource, node *yaml.Node, replacement string, parent int, inFlow bool) ([]sourceEdit, error) {
	data := source.data
	start, err := source.offset(node.Line, node.Column)
	if err != nil {
		return nil, err
	}
	_, lineEnd, _ := source.bounds(node.Line)
	// yaml.v3 points at an anchor or explicit tag, rather than the scalar token.
	for start < lineEnd {
		if data[start] == '&' || data[start] == '!' {
			if data[start] == '!' && start+1 < lineEnd && data[start+1] == '<' {
				end := bytes.IndexByte(data[start:], '>')
				if end < 0 {
					return nil, errors.New("tag")
				}
				start += end + 1
			} else {
				for start < lineEnd && !strings.ContainsRune(" \t\r,[]{}", rune(data[start])) {
					start++
				}
			}
			for start < lineEnd && (data[start] == ' ' || data[start] == '\t') {
				start++
			}
			continue
		}
		break
	}
	if node.Tag == "!!null" && node.Value == "" && start == lineEnd {
		prefix := ""
		if start > 0 && data[start-1] != ' ' && data[start-1] != '\t' {
			prefix = " "
		}
		return []sourceEdit{{start, start, prefix + "<redacted>"}}, nil
	}
	if start >= len(data) {
		return nil, errors.New("span")
	}
	if node.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		var edits []sourceEdit
		for line := node.Line + 1; ; line++ {
			a, b, ok := source.bounds(line)
			if !ok {
				break
			}
			text := data[a:b]
			trim := bytes.TrimSpace(text)
			if len(trim) > 0 && source.indent(line) <= parent {
				break
			}
			if len(trim) == 0 {
				continue
			}
			indent := source.indent(line)
			end := b
			if end > a && data[end-1] == '\r' {
				end--
			}
			edits = append(edits, sourceEdit{a + indent, end, "<redacted>"})
		}
		if len(edits) == 0 {
			end := start
			for end < lineEnd && !strings.ContainsRune(" \t\r#", rune(data[end])) {
				end++
			}
			return []sourceEdit{{start, end, `"<redacted>"`}}, nil
		}
		return edits, nil
	}
	end := start
	quote := byte(0)
	if data[start] == '\'' || data[start] == '"' {
		quote = data[start]
		end = start + 1
		closed := false
		for end < len(data) {
			if data[end] == quote {
				if quote == '\'' && end+1 < len(data) && data[end+1] == '\'' {
					end += 2
					continue
				}
				end++
				closed = true
				break
			}
			if quote == '"' && data[end] == '\\' {
				end += 2
			} else {
				end++
			}
		}
		if !closed || end > len(data) {
			return nil, errors.New("quote")
		}
	} else {
		end = lineEnd
		for i := start; i < lineEnd; i++ {
			if data[i] == '#' && (i == start || data[i-1] == ' ' || data[i-1] == '\t') {
				end = i
				break
			}
			if inFlow && strings.ContainsRune(",]}", rune(data[i])) {
				end = i
				break
			}
		}
		if !inFlow {
			for line := node.Line + 1; ; line++ {
				a, b, ok := source.bounds(line)
				if !ok {
					break
				}
				if len(bytes.TrimSpace(data[a:b])) == 0 {
					continue
				}
				if source.indent(line) <= parent {
					break
				}
				end = b
			}
		}
		for end > start && (data[end-1] == ' ' || data[end-1] == '\t' || data[end-1] == '\r') {
			end--
		}
	}
	raw := string(data[start:end])
	newlines := strings.Count(raw, "\n")
	if newlines == 0 {
		var encoded string
		if quote == '\'' {
			encoded = "'" + strings.ReplaceAll(replacement, "'", "''") + "'"
		} else if quote == '"' || strings.ContainsAny(replacement, "\n\r\t:[]{},#") {
			encoded = strconv.Quote(replacement)
		} else {
			encoded = replacement
		}
		return []sourceEdit{{start, end, encoded}}, nil
	}
	// Keep every original physical line. Multiline replacements remain a quoted
	// scalar, so a trailing flow delimiter or comment stays at its old location.
	parts := strings.Split(raw, "\n")
	for i := range parts {
		crlf := strings.HasSuffix(parts[i], "\r")
		indent := ""
		if i > 0 {
			for j := 0; j < len(parts[i]) && (parts[i][j] == ' ' || parts[i][j] == '\t'); j++ {
				indent += parts[i][j : j+1]
			}
		}
		parts[i] = indent + "<redacted>"
		if i == 0 {
			parts[i] = "\"" + parts[i]
		}
		if i == len(parts)-1 {
			parts[i] += "\""
		}
		if crlf {
			parts[i] += "\r"
		}
	}
	return []sourceEdit{{start, end, strings.Join(parts, "\n")}}, nil
}
