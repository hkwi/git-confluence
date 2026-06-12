package confluence

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	acNS = "http://atlassian.com/content"
	riNS = "http://atlassian.com/resource/identifier"

	DefaultMarkdownRecursionDepth = 32
)

var (
	headingRE        = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*\s*$`)
	unorderedListRE  = regexp.MustCompile(`^\s*[-*+]\s+(.*)$`)
	orderedListRE    = regexp.MustCompile(`^\s*\d+[.)]\s+(.*)$`)
	listItemRE       = regexp.MustCompile(`^([ \t]*)([-*+]|\d+[.)])\s+(.*)$`)
	admonitionRE     = regexp.MustCompile(`^\[!([A-Za-z][A-Za-z0-9_-]*)\](?:\s+(.*))?$`)
	tableSeparatorRE = regexp.MustCompile(`^:?-{3,}:?$`)
	xmlDeclRE        = regexp.MustCompile(`^\s*<\?xml[^>]*\?>`)
	namedEntityRE    = regexp.MustCompile(`&([A-Za-z][A-Za-z0-9]+);`)
	spaceRE          = regexp.MustCompile(`\s+`)
)

var rawInlineStorageTags = []string{"span", "sub", "sup", "u", "del", "s", "strike", "kbd", "mark", "small", "big", "tt"}

type fence struct {
	marker byte
	length int
	info   string
}

type part struct {
	text string
	elem *element
}

type element struct {
	name     xml.Name
	attr     []xml.Attr
	children []part
}

type macroOption struct {
	name  string
	value string
}

type macroAttribute struct {
	prefix string
	name   string
	value  string
}

type pageReference struct {
	title string
	space string
}

// MarkdownToStorage converts the working-tree Markdown form into a Confluence
// storage XML fragment suitable for Git's clean filter.
func MarkdownToStorage(markdown string) string {
	storage, err := MarkdownToStorageWithMaxDepth(markdown, DefaultMarkdownRecursionDepth)
	if err != nil {
		return ""
	}
	return storage
}

func MarkdownToStorageWithMaxDepth(markdown string, maxDepth int) (string, error) {
	if maxDepth < 1 {
		return "", fmt.Errorf("markdown conversion recursion depth must be positive")
	}
	return markdownToStorage(markdown, 1, maxDepth)
}

func markdownToStorage(markdown string, depth, maxDepth int) (string, error) {
	if raw, ok := parseSingleRawStorageFence(markdown); ok {
		return raw, nil
	}
	if depth > maxDepth {
		return "", fmt.Errorf("markdown conversion recursion depth exceeded: depth %d exceeds limit %d", depth, maxDepth)
	}

	lines := strings.Split(normalizeNewlines(markdown), "\n")
	var blocks []string

	for i := 0; i < len(lines); {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}

		if f, ok := parseOpeningFence(line); ok {
			body, next := collectFence(lines, i+1, f)
			switch firstInfoWord(f.info) {
			case "confluence-storage":
				if raw := strings.TrimRight(body, "\n\r"); raw != "" {
					blocks = append(blocks, raw)
				}
			case "confluence-noformat":
				blocks = append(blocks, noformatMacroWithInfo(strings.TrimRight(body, "\n\r"), f.info))
			case "confluence-html":
				blocks = append(blocks, htmlMacroWithInfo(strings.TrimRight(body, "\n\r"), f.info))
			default:
				blocks = append(blocks, codeMacroWithInfo(strings.TrimRight(body, "\n\r"), f.info))
			}
			i = next
			continue
		}

		block, next, ok, err := collectRoundTripContainer(lines, i, depth, maxDepth)
		if err != nil {
			return "", err
		}
		if ok {
			blocks = append(blocks, block)
			i = next
			continue
		}

		if isDetailsStart(line) {
			block, next, ok, err := collectDetails(lines, i, depth, maxDepth)
			if err != nil {
				return "", err
			}
			if ok {
				blocks = append(blocks, block)
				i = next
				continue
			}
			return "", fmt.Errorf("unclosed <details> block")
		}

		if block, ok := parseRoundTripBlockMacroLine(line); ok {
			blocks = append(blocks, block)
			i++
			continue
		}

		if rows, next, ok := parseMarkdownTable(lines, i); ok {
			blocks = append(blocks, tableToStorage(rows))
			i = next
			continue
		}

		if match := headingRE.FindStringSubmatch(line); match != nil {
			level := len(match[1])
			blocks = append(blocks, fmt.Sprintf("<h%d>%s</h%d>", level, inlineMarkdownToStorage(match[2]), level))
			i++
			continue
		}

		if isHorizontalRule(line) {
			blocks = append(blocks, "<hr />")
			i++
			continue
		}

		if unorderedListRE.MatchString(line) || orderedListRE.MatchString(line) {
			block, next, err := collectList(lines, i, depth, maxDepth)
			if err != nil {
				return "", err
			}
			blocks = append(blocks, block)
			i = next
			continue
		}

		if startsQuote(line) {
			var quoteLines []string
			for i < len(lines) && (startsQuote(lines[i]) || strings.TrimSpace(lines[i]) == "") {
				if startsQuote(lines[i]) {
					quoteLines = append(quoteLines, stripQuoteMarker(lines[i]))
				} else {
					quoteLines = append(quoteLines, "")
				}
				i++
			}
			macro, err := quoteLinesToMacroStorage(quoteLines, depth, maxDepth)
			if err != nil {
				return "", err
			}
			if macro != "" {
				blocks = append(blocks, macro)
				continue
			}
			converted, err := markdownToStorage(strings.Join(quoteLines, "\n"), depth+1, maxDepth)
			if err != nil {
				return "", err
			}
			inner := strings.TrimSpace(converted)
			blocks = append(blocks, "<blockquote>"+inner+"</blockquote>")
			continue
		}

		var paragraphLines []string
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" && !startsBlock(lines, i) {
			paragraphLines = append(paragraphLines, strings.TrimSpace(lines[i]))
			i++
		}
		paragraph := strings.Join(paragraphLines, " ")
		blocks = append(blocks, "<p>"+inlineMarkdownToStorage(paragraph)+"</p>")
	}

	if len(blocks) == 0 {
		return "", nil
	}
	return strings.Join(blocks, "\n\n") + "\n", nil
}

// StorageToMarkdown converts a Confluence storage XML fragment into the
// working-tree Markdown form suitable for Git's smudge filter.
func StorageToMarkdown(storage string) string {
	markdown, err := StorageToMarkdownWithMaxDepth(storage, DefaultMarkdownRecursionDepth)
	if err != nil {
		return rawStorageFence(storage)
	}
	return markdown
}

func StorageToMarkdownWithMaxDepth(storage string, maxDepth int) (string, error) {
	if maxDepth < 1 {
		return "", fmt.Errorf("storage conversion recursion depth must be positive")
	}
	if strings.TrimSpace(storage) == "" {
		return "", nil
	}
	if requiresExactRawStorage(storage) {
		return rawStorageFence(storage), nil
	}

	root, err := parseStorageFragment(storage)
	if err != nil {
		return rawStorageFence(storage), nil
	}

	var blocks []string
	for _, child := range root.children {
		if child.text != "" {
			if text := plainText(child.text); text != "" {
				blocks = append(blocks, text)
			}
			continue
		}
		if child.elem != nil {
			if rendered := strings.Trim(blockToMarkdown(child.elem, 1, maxDepth), "\n"); rendered != "" {
				blocks = append(blocks, rendered)
			}
		}
	}

	if len(blocks) == 0 {
		return "", nil
	}
	return strings.Join(blocks, "\n\n") + "\n", nil
}

func normalizeNewlines(input string) string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	return strings.ReplaceAll(input, "\r", "\n")
}

func startsBlock(lines []string, index int) bool {
	line := lines[index]
	if _, ok := parseOpeningFence(line); ok {
		return true
	}
	if _, _, _, _, ok := parseRoundTripContainerStart(line); ok {
		return true
	}
	if isDetailsStart(line) {
		return true
	}
	if headingRE.MatchString(line) || isHorizontalRule(line) {
		return true
	}
	if unorderedListRE.MatchString(line) || orderedListRE.MatchString(line) {
		return true
	}
	if startsQuote(line) {
		return true
	}
	if _, ok := parseRoundTripBlockMacroLine(line); ok {
		return true
	}
	_, _, ok := parseMarkdownTable(lines, index)
	return ok
}

func parseOpeningFence(line string) (fence, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) < 3 || (trimmed[0] != '`' && trimmed[0] != '~') {
		return fence{}, false
	}
	marker := trimmed[0]
	length := 0
	for length < len(trimmed) && trimmed[length] == marker {
		length++
	}
	if length < 3 {
		return fence{}, false
	}
	return fence{marker: marker, length: length, info: strings.TrimSpace(trimmed[length:])}, true
}

func collectFence(lines []string, start int, f fence) (string, int) {
	body := make([]string, 0)
	for i := start; i < len(lines); i++ {
		if isClosingFence(lines[i], f) {
			return strings.Join(body, "\n"), i + 1
		}
		body = append(body, lines[i])
	}
	return strings.Join(body, "\n"), len(lines)
}

func isClosingFence(line string, f fence) bool {
	trimmed := strings.TrimSpace(line)
	count := 0
	for count < len(trimmed) && trimmed[count] == f.marker {
		count++
	}
	if count < f.length {
		return false
	}
	return strings.TrimSpace(trimmed[count:]) == ""
}

func parseSingleRawStorageFence(markdown string) (string, bool) {
	lines := strings.Split(normalizeNewlines(markdown), "\n")
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	if start >= len(lines) {
		return "", false
	}

	f, ok := parseOpeningFence(lines[start])
	if !ok || firstInfoWord(f.info) != "confluence-storage" {
		return "", false
	}

	var body []string
	end := start + 1
	for ; end < len(lines); end++ {
		if isClosingFence(lines[end], f) {
			break
		}
		body = append(body, lines[end])
	}
	if end >= len(lines) {
		return "", false
	}
	for i := end + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			return "", false
		}
	}
	return strings.Join(body, "\n"), true
}

func firstInfoWord(info string) string {
	fields := strings.Fields(info)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func isHorizontalRule(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 {
		return false
	}
	var marker rune
	count := 0
	for _, r := range trimmed {
		if r == ' ' || r == '\t' {
			continue
		}
		if marker == 0 {
			if r != '-' && r != '*' && r != '_' {
				return false
			}
			marker = r
		}
		if r != marker {
			return false
		}
		count++
	}
	return count >= 3
}

func collectList(lines []string, start, depth, maxDepth int) (string, int, error) {
	item, ok := parseMarkdownListItem(lines[start])
	if !ok {
		return "", start, nil
	}
	return collectListAtIndent(lines, start, item.indent, item.ordered, depth, maxDepth)
}

type markdownListItem struct {
	indent  int
	ordered bool
	text    string
}

func parseMarkdownListItem(line string) (markdownListItem, bool) {
	match := listItemRE.FindStringSubmatch(line)
	if match == nil {
		return markdownListItem{}, false
	}
	return markdownListItem{
		indent: visualIndent(match[1]),
		ordered: match[2] != "" &&
			match[2][0] >= '0' && match[2][0] <= '9',
		text: strings.TrimSpace(match[3]),
	}, true
}

func visualIndent(indent string) int {
	width := 0
	for _, r := range indent {
		if r == '\t' {
			width += 4
		} else {
			width++
		}
	}
	return width
}

func lineIndent(line string) int {
	width := 0
	for _, r := range line {
		switch r {
		case ' ':
			width++
		case '\t':
			width += 4
		default:
			return width
		}
	}
	return width
}

func stripVisualIndent(line string, width int) string {
	seen := 0
	for i, r := range line {
		if r != ' ' && r != '\t' {
			return line[i:]
		}
		if r == '\t' {
			seen += 4
		} else {
			seen++
		}
		if seen >= width {
			return line[i+utf8.RuneLen(r):]
		}
	}
	return ""
}

func collectListAtIndent(lines []string, start, indent int, ordered bool, depth, maxDepth int) (string, int, error) {
	tag := "ul"
	if ordered {
		tag = "ol"
	}

	var items []string
	i := start
	for i < len(lines) {
		item, ok := parseMarkdownListItem(lines[i])
		if !ok || item.indent < indent {
			break
		}
		if item.indent > indent {
			break
		}
		if item.ordered != ordered {
			break
		}

		i++
		var children []string
		for i < len(lines) {
			next, ok := parseMarkdownListItem(lines[i])
			if ok && next.indent <= indent {
				break
			}
			if ok {
				block, nextIndex, err := collectListAtIndent(lines, i, next.indent, next.ordered, depth, maxDepth)
				if err != nil {
					return "", start, err
				}
				children = append(children, block)
				i = nextIndex
				continue
			}
			if strings.TrimSpace(lines[i]) == "" {
				i++
				continue
			}
			if lineIndent(lines[i]) <= indent {
				break
			}
			blockLines, nextIndex := collectListContinuation(lines, i, indent+2)
			converted, err := markdownToStorage(strings.Join(blockLines, "\n"), depth+1, maxDepth)
			if err != nil {
				return "", start, err
			}
			block := strings.TrimSpace(converted)
			if block != "" {
				children = append(children, block)
			}
			i = nextIndex
		}

		body := inlineMarkdownToStorage(item.text)
		if len(children) > 0 {
			body += "\n" + strings.Join(children, "\n")
		}
		items = append(items, "<li>"+body+"</li>")
	}
	return "<" + tag + ">\n" + strings.Join(items, "\n") + "\n</" + tag + ">", i, nil
}

func collectListContinuation(lines []string, start, stripIndent int) ([]string, int) {
	var block []string
	i := start
	var openFence fence
	inFence := false
	inDetails := false
	for i < len(lines) {
		line := stripVisualIndent(lines[i], stripIndent)
		if !inFence && !inDetails {
			if _, ok := parseMarkdownListItem(lines[i]); ok {
				break
			}
			if strings.TrimSpace(lines[i]) != "" && lineIndent(lines[i]) < stripIndent {
				break
			}
		}

		block = append(block, line)
		if inFence {
			if isClosingFence(line, openFence) {
				inFence = false
			}
			i++
			continue
		}
		if inDetails {
			if strings.EqualFold(strings.TrimSpace(line), "</details>") {
				inDetails = false
			}
			i++
			continue
		}
		if f, ok := parseOpeningFence(line); ok {
			openFence = f
			inFence = true
			i++
			continue
		}
		if isDetailsStart(line) {
			inDetails = true
			i++
			continue
		}
		if strings.TrimSpace(lines[i]) != "" && lineIndent(lines[i]) < stripIndent {
			break
		}
		i++
	}
	return trimBlankLines(block), i
}

func startsQuote(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " \t"), ">")
}

func stripQuoteMarker(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	trimmed = strings.TrimPrefix(trimmed, ">")
	return strings.TrimPrefix(trimmed, " ")
}

func quoteLinesToMacroStorage(lines []string, depth, maxDepth int) (string, error) {
	if len(lines) == 0 {
		return "", nil
	}
	first := strings.TrimSpace(lines[0])
	match := admonitionRE.FindStringSubmatch(first)
	if match == nil {
		return "", nil
	}

	name := strings.ToLower(match[1])
	title := strings.TrimSpace(match[2])
	body := trimBlankLines(lines[1:])

	switch name {
	case "info", "note", "tip", "warning":
		return richTextMacro(name, title, strings.Join(body, "\n"), depth, maxDepth)
	case "panel":
		return richTextMacro("panel", title, strings.Join(body, "\n"), depth, maxDepth)
	default:
		return "", nil
	}
}

func isDetailsStart(line string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(line))
	return trimmed == "<details>"
}

func collectDetails(lines []string, start, depth, maxDepth int) (string, int, bool, error) {
	i := start + 1
	title := ""
	if i < len(lines) {
		if parsed, ok := parseSummaryLine(lines[i]); ok {
			title = parsed
			i++
		}
	}

	var body []string
	nested := 0
	for i < len(lines) {
		if isDetailsStart(lines[i]) {
			nested++
			body = append(body, lines[i])
			i++
			continue
		}
		if strings.EqualFold(strings.TrimSpace(lines[i]), "</details>") {
			if nested > 0 {
				nested--
				body = append(body, lines[i])
				i++
				continue
			}
			macro, err := richTextMacro("expand", title, strings.Join(trimBlankLines(body), "\n"), depth, maxDepth)
			return macro, i + 1, true, err
		}
		body = append(body, lines[i])
		i++
	}
	return "", start, false, nil
}

func parseSummaryLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "<summary>") || !strings.HasSuffix(lower, "</summary>") {
		return "", false
	}
	title := trimmed[len("<summary>") : len(trimmed)-len("</summary>")]
	return plainText(title), true
}

func trimBlankLines(lines []string) []string {
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[start:end]
}

func parseMarkdownTable(lines []string, start int) ([][]string, int, bool) {
	if start+1 >= len(lines) {
		return nil, start, false
	}
	if !looksLikeTableRow(lines[start]) || !isTableSeparator(lines[start+1]) {
		return nil, start, false
	}

	rows := [][]string{splitTableRow(lines[start])}
	i := start + 2
	for i < len(lines) && looksLikeTableRow(lines[i]) {
		rows = append(rows, splitTableRow(lines[i]))
		i++
	}
	return rows, i, true
}

func looksLikeTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.Contains(trimmed, "|") && !strings.HasPrefix(trimmed, "```")
}

func isTableSeparator(line string) bool {
	cells := splitTableRow(line)
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		if !tableSeparatorRE.MatchString(strings.TrimSpace(cell)) {
			return false
		}
	}
	return true
}

func splitTableRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	raw := strings.Split(trimmed, "|")
	cells := make([]string, 0, len(raw))
	for _, cell := range raw {
		cells = append(cells, strings.TrimSpace(cell))
	}
	return cells
}

func tableToStorage(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	width := 0
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}

	rendered := make([]string, 0, len(rows))
	for rowIndex, row := range rows {
		cellTag := "td"
		if rowIndex == 0 {
			cellTag = "th"
		}
		var cells strings.Builder
		for i := 0; i < width; i++ {
			value := ""
			if i < len(row) {
				value = row[i]
			}
			cells.WriteString("<" + cellTag + ">")
			cells.WriteString(inlineMarkdownToStorage(value))
			cells.WriteString("</" + cellTag + ">")
		}
		rendered = append(rendered, "<tr>"+cells.String()+"</tr>")
	}
	return "<table><tbody>\n" + strings.Join(rendered, "\n") + "\n</tbody></table>"
}

func parseRoundTripBlockMacroLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "{{") || !strings.HasSuffix(trimmed, "}}") {
		return "", false
	}
	body := strings.TrimSpace(trimmed[len("{{") : len(trimmed)-len("}}")])
	fields := splitInfoFields(body)
	if len(fields) == 0 {
		return "", false
	}

	name, attrs, options, page, ok := parseStructuredMacroFields(fields)
	if !ok {
		return "", false
	}
	return structuredMacroStorage(name, attrs, options, page, "", false), true
}

func isRoundTripBlockMacro(name string) bool {
	switch name {
	case "toc", "children", "include", "excerpt-include":
		return true
	default:
		return false
	}
}

func parseStructuredMacroFields(fields []string) (string, []macroAttribute, []macroOption, pageReference, bool) {
	if len(fields) == 0 {
		return "", nil, nil, pageReference{}, false
	}
	name := strings.ToLower(fields[0])
	if !isStructuredMacroName(name) {
		return "", nil, nil, pageReference{}, false
	}
	attrs, options, page, ok := parseStructuredMacroOptions(name, fields[1:])
	return name, attrs, options, page, ok
}

func isStructuredMacroName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}

func parseStructuredMacroOptions(name string, fields []string) ([]macroAttribute, []macroOption, pageReference, bool) {
	var attrs []macroAttribute
	var options []macroOption
	var page pageReference
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, nil, pageReference{}, false
		}
		if !ok {
			value = "true"
		} else {
			value = strings.TrimSpace(value)
		}

		if attr, handled, ok := parseMacroAttributeOption(key, value); handled {
			if !ok {
				return nil, nil, pageReference{}, false
			}
			attrs = append(attrs, attr)
			continue
		}

		if name == "include" || name == "excerpt-include" || name == "children" {
			switch key {
			case "page":
				if page.title != "" {
					return nil, nil, pageReference{}, false
				}
				page.title = value
				continue
			case "space":
				if page.space != "" {
					return nil, nil, pageReference{}, false
				}
				page.space = value
				continue
			}
		}
		options = append(options, macroOption{name: key, value: value})
	}

	if (name == "include" || name == "excerpt-include") && page.title == "" {
		return nil, nil, pageReference{}, false
	}
	if name == "children" && page.title == "" && page.space != "" {
		return nil, nil, pageReference{}, false
	}
	return attrs, options, page, true
}

func parseMacroAttributeOption(key, value string) (macroAttribute, bool, bool) {
	switch key {
	case "macro-id", "ac:macro-id":
		return macroAttribute{prefix: "ac", name: "macro-id", value: value}, true, true
	case "schema-version", "ac:schema-version":
		return macroAttribute{prefix: "ac", name: "schema-version", value: value}, true, true
	}

	if !strings.HasPrefix(key, "attr:") {
		return macroAttribute{}, false, true
	}
	prefix, name, ok := splitMacroAttributeOptionName(strings.TrimPrefix(key, "attr:"))
	if !ok || (prefix == "ac" && name == "name") {
		return macroAttribute{}, true, false
	}
	return macroAttribute{prefix: prefix, name: name, value: value}, true, true
}

func splitMacroAttributeOptionName(name string) (string, string, bool) {
	if name == "" {
		return "", "", false
	}
	prefix := ""
	local := name
	if before, after, ok := strings.Cut(name, ":"); ok {
		prefix = before
		local = after
	}
	if local == "" {
		return "", "", false
	}
	switch prefix {
	case "", "ac", "ri":
		return prefix, local, true
	default:
		return "", "", false
	}
}

func roundTripBlockMacroStorage(name string, options []macroOption, page pageReference) string {
	return structuredMacroStorage(name, nil, options, page, "", false)
}

func structuredMacroStorage(name string, attrs []macroAttribute, options []macroOption, page pageReference, richTextBody string, hasRichTextBody bool) string {
	ns := ` xmlns:ac="` + acNS + `"`
	if page.title != "" {
		ns += ` xmlns:ri="` + riNS + `"`
	}
	for _, attr := range attrs {
		if attr.prefix == "ri" {
			ns += ` xmlns:ri="` + riNS + `"`
			break
		}
	}

	var parameters []string
	if page.title != "" {
		paramName := ""
		if name == "children" {
			paramName = "page"
		}
		space := ""
		if page.space != "" {
			space = ` ri:space-key="` + escapeAttr(page.space) + `"`
		}
		parameters = append(parameters,
			`<ac:parameter ac:name="`+paramName+`"><ac:link><ri:page ri:content-title="`+
				escapeAttr(page.title)+`"`+space+` /></ac:link></ac:parameter>`)
	}
	for _, option := range options {
		parameters = append(parameters,
			`<ac:parameter ac:name="`+escapeAttr(option.name)+`">`+
				escapeText(option.value)+`</ac:parameter>`)
	}

	start := `<ac:structured-macro` + ns + ` ac:name="` + escapeAttr(name) + `"`
	for _, attr := range attrs {
		start += ` ` + macroAttributeStorageName(attr) + `="` + escapeAttr(attr.value) + `"`
	}
	if len(parameters) == 0 && !hasRichTextBody {
		return start + ` />`
	}
	body := "\n"
	if len(parameters) > 0 {
		body += strings.Join(parameters, "\n") + "\n"
	}
	if hasRichTextBody {
		if richTextBody != "" {
			richTextBody = "\n" + richTextBody + "\n"
		}
		body += `<ac:rich-text-body>` + richTextBody + `</ac:rich-text-body>` + "\n"
	}
	return start + `>` + body + `</ac:structured-macro>`
}

func macroAttributeStorageName(attr macroAttribute) string {
	switch attr.prefix {
	case "ac":
		return "ac:" + escapeAttr(attr.name)
	case "ri":
		return "ri:" + escapeAttr(attr.name)
	default:
		return escapeAttr(attr.name)
	}
}

func collectRoundTripContainer(lines []string, start, depth, maxDepth int) (string, int, bool, error) {
	name, attrs, options, page, ok := parseRoundTripContainerStart(lines[start])
	if !ok {
		return "", start, false, nil
	}
	var body []string
	closing := "{{/" + name + "}}"
	for i := start + 1; i < len(lines); i++ {
		if strings.EqualFold(strings.TrimSpace(lines[i]), closing) {
			macro, err := richTextMacroWithAttributesAndOptions(name, attrs, options, page, strings.Join(trimBlankLines(body), "\n"), depth, maxDepth)
			return macro, i + 1, true, err
		}
		body = append(body, lines[i])
	}
	return "", start, false, nil
}

func parseRoundTripContainerStart(line string) (string, []macroAttribute, []macroOption, pageReference, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "{{") || !strings.HasSuffix(trimmed, "}}") {
		return "", nil, nil, pageReference{}, false
	}
	body := strings.TrimSpace(trimmed[len("{{") : len(trimmed)-len("}}")])
	fields := splitInfoFields(body)
	if len(fields) == 0 {
		return "", nil, nil, pageReference{}, false
	}
	name, attrs, options, page, ok := parseStructuredMacroFields(fields)
	return name, attrs, options, page, ok
}

func inlineMarkdownToStorage(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); {
		if strings.HasPrefix(text[i:], "{{status") {
			if storage, end, ok := parseMarkdownStatus(text, i); ok {
				out.WriteString(storage)
				i = end
				continue
			}
		}

		if strings.HasPrefix(text[i:], "![") {
			if label, target, end, ok := parseMarkdownLink(text, i+1); ok {
				out.WriteString(imageToStorage(label, target))
				i = end
				continue
			}
		}

		if strings.HasPrefix(text[i:], "[") {
			if label, target, end, ok := parseMarkdownLink(text, i); ok {
				out.WriteString(linkToStorage(label, target))
				i = end
				continue
			}
		}

		if text[i] == '<' {
			if raw, end, ok := parseMarkdownBreak(text, i); ok {
				out.WriteString(raw)
				i = end
				continue
			}
			if raw, end, ok := parseRawInlineStorage(text, i); ok {
				out.WriteString(raw)
				i = end
				continue
			}
		}

		if text[i] == '`' {
			if rel := strings.IndexByte(text[i+1:], '`'); rel >= 0 {
				end := i + 1 + rel
				out.WriteString("<code>")
				out.WriteString(escapeText(text[i+1 : end]))
				out.WriteString("</code>")
				i = end + 1
				continue
			}
		}

		if strings.HasPrefix(text[i:], "**") {
			if rel := strings.Index(text[i+2:], "**"); rel >= 0 {
				end := i + 2 + rel
				out.WriteString("<strong>")
				out.WriteString(inlineMarkdownToStorage(text[i+2 : end]))
				out.WriteString("</strong>")
				i = end + 2
				continue
			}
		}

		if text[i] == '*' {
			if rel := strings.IndexByte(text[i+1:], '*'); rel >= 0 {
				end := i + 1 + rel
				out.WriteString("<em>")
				out.WriteString(inlineMarkdownToStorage(text[i+1 : end]))
				out.WriteString("</em>")
				i = end + 1
				continue
			}
		}

		if text[i] == '\\' && i+1 < len(text) {
			r, size := utf8.DecodeRuneInString(text[i+1:])
			out.WriteString(escapeText(string(r)))
			i += 1 + size
			continue
		}

		r, size := utf8.DecodeRuneInString(text[i:])
		out.WriteString(escapeText(string(r)))
		i += size
	}
	return out.String()
}

func parseMarkdownBreak(text string, start int) (string, int, bool) {
	lower := strings.ToLower(text[start:])
	switch {
	case strings.HasPrefix(lower, "<br>"):
		return "<br />", start + len("<br>"), true
	case strings.HasPrefix(lower, "<br/>"):
		return "<br />", start + len("<br/>"), true
	case strings.HasPrefix(lower, "<br />"):
		return "<br />", start + len("<br />"), true
	default:
		return "", start, false
	}
}

func parseMarkdownStatus(text string, start int) (string, int, bool) {
	closeRel := strings.Index(text[start:], "}}")
	if closeRel < 0 {
		return "", start, false
	}
	end := start + closeRel + len("}}")
	body := strings.TrimSpace(text[start+len("{{") : end-len("}}")])
	lower := strings.ToLower(body)
	if !strings.HasPrefix(lower, "status") {
		return "", start, false
	}

	rest := strings.TrimSpace(body[len("status"):])
	color := ""
	title := ""
	switch {
	case strings.HasPrefix(rest, ":"):
		parts := strings.SplitN(strings.TrimPrefix(rest, ":"), "|", 2)
		color = strings.TrimSpace(parts[0])
		if len(parts) > 1 {
			title = strings.TrimSpace(parts[1])
		}
	case strings.HasPrefix(rest, "|"):
		title = strings.TrimSpace(strings.TrimPrefix(rest, "|"))
	default:
		return "", start, false
	}
	if title == "" {
		return "", start, false
	}
	return statusMacro(title, color), end, true
}

func parseMarkdownLink(text string, start int) (string, string, int, bool) {
	closeLabelRel := strings.IndexByte(text[start+1:], ']')
	if closeLabelRel < 0 {
		return "", "", start, false
	}
	closeLabel := start + 1 + closeLabelRel
	if closeLabel+1 >= len(text) || text[closeLabel+1] != '(' {
		return "", "", start, false
	}
	closeTargetRel := strings.IndexByte(text[closeLabel+2:], ')')
	if closeTargetRel < 0 {
		return "", "", start, false
	}
	closeTarget := closeLabel + 2 + closeTargetRel
	return text[start+1 : closeLabel], strings.TrimSpace(text[closeLabel+2 : closeTarget]), closeTarget + 1, true
}

func parseRawInlineStorage(text string, start int) (string, int, bool) {
	for _, tag := range rawInlineStorageTags {
		raw, end, ok := parseRawInlineTag(text, start, tag)
		if ok {
			return raw, end, true
		}
	}
	return "", start, false
}

func parseRawInlineTag(text string, start int, tag string) (string, int, bool) {
	lower := strings.ToLower(text[start:])
	open := "<" + tag
	if !strings.HasPrefix(lower, open) {
		return "", start, false
	}
	if len(lower) <= len(open) {
		return "", start, false
	}
	next := lower[len(open)]
	if next != '>' && next != ' ' && next != '\t' && next != '\n' {
		return "", start, false
	}

	closeTag := "</" + tag + ">"
	closeRel := strings.Index(lower, closeTag)
	if closeRel < 0 {
		return "", start, false
	}
	end := start + closeRel + len(closeTag)
	raw := text[start:end]

	root, err := parseStorageFragment(raw)
	if err != nil {
		return "", start, false
	}
	children := elementChildren(root)
	if len(children) != 1 || hasNonWhitespaceText(root) {
		return "", start, false
	}
	child := children[0]
	prefix, local := splitName(child.name)
	if prefix != "" || local != tag || !canRenderInline(child) {
		return "", start, false
	}
	return elementToStorage(child), end, true
}

func linkToStorage(label, target string) string {
	if title, space, ok := confluencePageTarget(target); ok {
		spaceAttr := ""
		if space != "" {
			spaceAttr = ` ri:space-key="` + escapeAttr(space) + `"`
		}
		return `<ac:link xmlns:ac="` + acNS + `" xmlns:ri="` + riNS + `">` +
			`<ri:page ri:content-title="` + escapeAttr(title) + `"` + spaceAttr + ` />` +
			`<ac:plain-text-link-body>` + escapeText(label) + `</ac:plain-text-link-body>` +
			`</ac:link>`
	}
	return `<a href="` + escapeAttr(target) + `">` + inlineMarkdownToStorage(label) + `</a>`
}

func imageToStorage(alt, target string) string {
	altAttr := ""
	if alt != "" {
		altAttr = ` ac:alt="` + escapeAttr(alt) + `"`
	}
	ref := `<ri:url ri:value="` + escapeAttr(target) + `" />`
	if filename, ok := confluenceTarget(target, "attachment"); ok {
		ref = `<ri:attachment ri:filename="` + escapeAttr(filename) + `" />`
	}
	return `<ac:image xmlns:ac="` + acNS + `" xmlns:ri="` + riNS + `"` + altAttr + `>` + ref + `</ac:image>`
}

func confluencePageTarget(target string) (string, string, bool) {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme != "confluence" || parsed.Host != "page" {
		return "", "", false
	}
	title, err := url.PathUnescape(strings.TrimPrefix(parsed.EscapedPath(), "/"))
	if err != nil || title == "" {
		return "", "", false
	}
	return title, parsed.Query().Get("space"), true
}

func confluenceTarget(target, host string) (string, bool) {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme != "confluence" || parsed.Host != host {
		return "", false
	}
	value, err := url.PathUnescape(strings.TrimPrefix(parsed.EscapedPath(), "/"))
	if err != nil {
		return "", false
	}
	return value, true
}

func codeMacro(code, language string) string {
	return codeMacroWithInfo(code, language)
}

func codeMacroWithInfo(code, info string) string {
	language, options := parseFenceInfoOptions(info)
	attrs, options := splitMacroAttributesFromOptionMap(options)
	var parameter string
	if language != "" {
		parameter = "\n" + `<ac:parameter ac:name="language">` + escapeText(language) + `</ac:parameter>`
	}
	for _, name := range sortedCodeOptionNames(options) {
		if value := options[name]; value != "" {
			parameter += "\n" + `<ac:parameter ac:name="` + escapeAttr(name) + `">` + escapeText(value) + `</ac:parameter>`
		}
	}
	return plainTextMacroStorage("code", attrs, parameter, code)
}

func plainTextMacroStorage(name string, attrs []macroAttribute, parameter, body string) string {
	ns := ` xmlns:ac="` + acNS + `"`
	for _, attr := range attrs {
		if attr.prefix == "ri" {
			ns += ` xmlns:ri="` + riNS + `"`
			break
		}
	}
	start := `<ac:structured-macro` + ns + ` ac:name="` + escapeAttr(name) + `"`
	for _, attr := range attrs {
		start += ` ` + macroAttributeStorageName(attr) + `="` + escapeAttr(attr.value) + `"`
	}
	return start + `>` +
		parameter + "\n" +
		`<ac:plain-text-body>` + escapeText(body) + `</ac:plain-text-body>` + "\n" +
		`</ac:structured-macro>`
}

func noformatMacro(body string) string {
	return noformatMacroWithInfo(body, "confluence-noformat")
}

func noformatMacroWithInfo(body, info string) string {
	attrs, options := parseFenceMacroOptionsAfterInfoWord(info)
	return plainTextMacroStorage("noformat", attrs, macroOptionsParameterXML(options), body)
}

func htmlMacro(body string) string {
	return htmlMacroWithInfo(body, "confluence-html")
}

func htmlMacroWithInfo(body, info string) string {
	attrs, options := parseFenceMacroOptionsAfterInfoWord(info)
	return plainTextMacroStorage("html", attrs, macroOptionsParameterXML(options), body)
}

func macroOptionsParameterXML(options []macroOption) string {
	var parameter string
	for _, option := range options {
		parameter += "\n" + `<ac:parameter ac:name="` + escapeAttr(option.name) + `">` + escapeText(option.value) + `</ac:parameter>`
	}
	return parameter
}

func sortedCodeOptionNames(options map[string]string) []string {
	preferred := []string{"title", "linenumbers", "theme", "collapse"}
	seen := map[string]bool{}
	var names []string
	for _, name := range preferred {
		if _, ok := options[name]; ok {
			names = append(names, name)
			seen[name] = true
		}
	}
	var rest []string
	for name := range options {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	return append(names, rest...)
}

func parseFenceInfoOptions(info string) (string, map[string]string) {
	fields := splitInfoFields(info)
	options := map[string]string{}
	if len(fields) == 0 {
		return "", options
	}
	language := ""
	start := 0
	if !strings.Contains(fields[0], "=") {
		language = fields[0]
		start = 1
	}
	for _, field := range fields[start:] {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			options[strings.ToLower(field)] = "true"
			continue
		}
		options[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return language, options
}

func parseFenceMacroOptionsAfterInfoWord(info string) ([]macroAttribute, []macroOption) {
	fields := splitInfoFields(info)
	if len(fields) <= 1 {
		return nil, nil
	}
	attrs, options, _, ok := parseStructuredMacroOptions("", fields[1:])
	if !ok {
		return nil, nil
	}
	return attrs, options
}

func splitMacroAttributesFromOptionMap(options map[string]string) ([]macroAttribute, map[string]string) {
	attrs := []macroAttribute{}
	remaining := map[string]string{}
	var keys []string
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := options[key]
		if attr, handled, ok := parseMacroAttributeOption(key, value); handled && ok {
			attrs = append(attrs, attr)
			continue
		}
		remaining[key] = value
	}
	return attrs, remaining
}

func splitInfoFields(info string) []string {
	var fields []string
	var current strings.Builder
	var quote rune
	escaped := false

	for _, r := range strings.TrimSpace(info) {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && quote != 0 {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			continue
		}
		if r == '"' || r == '\'' {
			quote = r
			continue
		}
		if r == ' ' || r == '\t' {
			if current.Len() > 0 {
				fields = append(fields, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		fields = append(fields, current.String())
	}
	return fields
}

func statusMacro(title, color string) string {
	var parameters []string
	if color != "" {
		parameters = append(parameters, `<ac:parameter ac:name="colour">`+escapeText(color)+`</ac:parameter>`)
	}
	parameters = append(parameters, `<ac:parameter ac:name="title">`+escapeText(title)+`</ac:parameter>`)
	return `<ac:structured-macro xmlns:ac="` + acNS + `" ac:name="status">` +
		"\n" + strings.Join(parameters, "\n") + "\n" +
		`</ac:structured-macro>`
}

func richTextMacro(name, title, markdown string, depth, maxDepth int) (string, error) {
	var options []macroOption
	if title != "" {
		options = append(options, macroOption{name: "title", value: title})
	}
	return richTextMacroWithOptions(name, options, markdown, depth, maxDepth)
}

func richTextMacroWithOptions(name string, options []macroOption, markdown string, depth, maxDepth int) (string, error) {
	return richTextMacroWithAttributesAndOptions(name, nil, options, pageReference{}, markdown, depth, maxDepth)
}

func richTextMacroWithAttributesAndOptions(name string, attrs []macroAttribute, options []macroOption, page pageReference, markdown string, depth, maxDepth int) (string, error) {
	converted, err := markdownToStorage(markdown, depth+1, maxDepth)
	if err != nil {
		return "", err
	}
	body := strings.TrimSpace(converted)
	return structuredMacroStorage(name, attrs, options, page, body, true), nil
}

func parseStorageFragment(storage string) (*element, error) {
	cleaned := normalizeNamedEntities(xmlDeclRE.ReplaceAllString(storage, ""))
	wrapped := `<git_confluence_root xmlns:ac="` + acNS + `" xmlns:ri="` + riNS + `">` + cleaned + `</git_confluence_root>`

	decoder := xml.NewDecoder(strings.NewReader(wrapped))
	var stack []*element
	var root *element

	for {
		token, err := decoder.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		switch token := token.(type) {
		case xml.StartElement:
			stack = append(stack, &element{name: token.Name, attr: append([]xml.Attr(nil), token.Attr...)})
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].children = append(stack[len(stack)-1].children, part{text: string(token)})
			}
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("unexpected end element %s", token.Name.Local)
			}
			current := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				root = current
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, part{elem: current})
			}
		}
	}

	if root == nil {
		return nil, fmt.Errorf("empty XML fragment")
	}
	return root, nil
}

func normalizeNamedEntities(input string) string {
	entities := map[string]string{
		"nbsp":   "&#160;",
		"ndash":  "&#8211;",
		"mdash":  "&#8212;",
		"copy":   "&#169;",
		"reg":    "&#174;",
		"trade":  "&#8482;",
		"hellip": "&#8230;",
		"laquo":  "&#171;",
		"raquo":  "&#187;",
	}
	return namedEntityRE.ReplaceAllStringFunc(input, func(match string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(match, "&"), ";")
		switch name {
		case "amp", "lt", "gt", "quot", "apos":
			return match
		}
		if value, ok := entities[name]; ok {
			return value
		}
		return match
	})
}

func blockToMarkdown(el *element, depth, maxDepth int) string {
	if depth > maxDepth {
		return rawStorageFence(elementToStorage(el))
	}
	prefix, local := splitName(el.name)

	if prefix == "" && len(local) == 2 && local[0] == 'h' && local[1] >= '1' && local[1] <= '6' {
		if !canRenderInline(el) {
			return rawStorageFence(elementToStorage(el))
		}
		level := int(local[1] - '0')
		return strings.Repeat("#", level) + " " + strings.TrimSpace(inlineStorageToMarkdown(el)) + "\n"
	}

	if prefix == "" && local == "p" {
		if macro, ok := singleStructuredMacro(el); ok {
			return macroToMarkdown(macro, depth, maxDepth)
		}
		if !canRenderInline(el) {
			return rawStorageFence(elementToStorage(el))
		}
		return strings.TrimSpace(inlineStorageToMarkdown(el)) + "\n"
	}

	if prefix == "" && local == "blockquote" {
		inner := strings.TrimSpace(childrenToMarkdown(el, depth+1, maxDepth))
		if inner == "" {
			return ">\n"
		}
		var lines []string
		for _, line := range strings.Split(inner, "\n") {
			if line == "" {
				lines = append(lines, ">")
			} else {
				lines = append(lines, "> "+line)
			}
		}
		return strings.Join(lines, "\n") + "\n"
	}

	if prefix == "" && (local == "ul" || local == "ol") {
		if !canRenderList(el) {
			return rawStorageFence(elementToStorage(el))
		}
		return listToMarkdown(el, 0, depth, maxDepth)
	}

	if prefix == "" && local == "table" {
		if rendered, ok := storageTableToMarkdown(el); ok {
			return rendered
		}
		return rawStorageFence(elementToStorage(el))
	}

	if prefix == "" && local == "pre" {
		return markdownCodeFence(textContent(el), "")
	}

	if prefix == "" && local == "br" {
		return "\n"
	}

	if prefix == "" && local == "hr" {
		return "---\n"
	}

	if prefix == "" && isTransparentBlock(local) && len(el.attr) == 0 {
		return strings.TrimSpace(childrenToMarkdown(el, depth+1, maxDepth)) + "\n"
	}

	if prefix == "ac" && isTransparentLayoutBlock(local) {
		if canRenderTransparentLayout(el) {
			return strings.TrimSpace(childrenToMarkdown(el, depth+1, maxDepth)) + "\n"
		}
		return rawStorageFence(elementToStorage(el))
	}

	if prefix == "ac" && local == "structured-macro" {
		return macroToMarkdown(el, depth, maxDepth)
	}

	if prefix == "ac" && local == "image" {
		return imageStorageToMarkdown(el) + "\n"
	}

	return rawStorageFence(elementToStorage(el))
}

func isTransparentBlock(local string) bool {
	switch local {
	case "div", "section", "article", "main", "tbody", "thead", "tfoot":
		return true
	default:
		return false
	}
}

func isTransparentLayoutBlock(local string) bool {
	switch local {
	case "layout", "layout-section", "layout-cell":
		return true
	default:
		return false
	}
}

func canRenderTransparentLayout(el *element) bool {
	prefix, local := splitName(el.name)
	if prefix != "ac" || !isTransparentLayoutBlock(local) {
		return false
	}
	switch local {
	case "layout":
		for _, child := range elementChildren(el) {
			childPrefix, childLocal := splitName(child.name)
			if childPrefix != "ac" || childLocal != "layout-section" || !canRenderTransparentLayout(child) {
				return false
			}
		}
		return true
	case "layout-section":
		if sectionType, _ := attr(el, "ac", "type"); sectionType != "" && sectionType != "single" {
			return false
		}
		for _, child := range elementChildren(el) {
			childPrefix, childLocal := splitName(child.name)
			if childPrefix != "ac" || childLocal != "layout-cell" || !canRenderTransparentLayout(child) {
				return false
			}
		}
		return true
	case "layout-cell":
		for _, child := range elementChildren(el) {
			if !canRenderBlockWithoutRawStorage(child) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func canRenderBlockWithoutRawStorage(el *element) bool {
	prefix, local := splitName(el.name)

	if prefix == "" && len(local) == 2 && local[0] == 'h' && local[1] >= '1' && local[1] <= '6' {
		return canRenderInline(el)
	}
	if prefix == "" && local == "p" {
		if macro, ok := singleStructuredMacro(el); ok {
			return canRenderMacroWithoutRawStorage(macro)
		}
		return canRenderInline(el)
	}
	if prefix == "" && local == "blockquote" {
		return canRenderChildrenWithoutRawStorage(el)
	}
	if prefix == "" && (local == "ul" || local == "ol") {
		return canRenderList(el)
	}
	if prefix == "" && local == "table" {
		return canRenderStorageTable(el)
	}
	if prefix == "" && (local == "pre" || local == "br" || local == "hr") {
		return true
	}
	if prefix == "" && isTransparentBlock(local) && len(el.attr) == 0 {
		return canRenderChildrenWithoutRawStorage(el)
	}
	if prefix == "ac" && isTransparentLayoutBlock(local) {
		return canRenderTransparentLayout(el)
	}
	if prefix == "ac" && local == "structured-macro" {
		return canRenderMacroWithoutRawStorage(el)
	}
	if prefix == "ac" && local == "image" {
		return true
	}
	return false
}

func canRenderChildrenWithoutRawStorage(el *element) bool {
	for _, child := range el.children {
		if child.elem == nil {
			continue
		}
		if !canRenderBlockWithoutRawStorage(child.elem) {
			return false
		}
	}
	return true
}

func canRenderMacroWithoutRawStorage(el *element) bool {
	name := macroName(el)
	if isRoundTripBlockMacro(name) {
		_, _, ok := roundTripMacroOptionsFromStorage(el)
		return ok
	}

	switch name {
	case "code", "noformat", "html":
		return canRenderPlainTextMacroWithoutRawStorage(el)
	case "status":
		return canRenderGenericStructuredMacroWithoutRawStorage(el)
	case "excerpt", "expand", "panel", "info", "note", "tip", "warning":
		return canRenderGenericStructuredMacroWithoutRawStorage(el)
	default:
		return canRenderGenericStructuredMacroWithoutRawStorage(el)
	}
}

func canRenderPlainTextMacroWithoutRawStorage(el *element) bool {
	for _, child := range elementChildren(el) {
		prefix, local := splitName(child.name)
		if prefix == "ac" && local == "parameter" && !hasElementChild(child) {
			continue
		}
		if prefix == "ac" && local == "plain-text-body" {
			continue
		}
		return false
	}
	return true
}

func canRenderGenericStructuredMacroWithoutRawStorage(el *element) bool {
	seenRichTextBody := false
	for _, child := range elementChildren(el) {
		prefix, local := splitName(child.name)
		if prefix == "ac" && local == "parameter" && !hasElementChild(child) {
			continue
		}
		if prefix == "ac" && local == "rich-text-body" && !seenRichTextBody {
			seenRichTextBody = true
			if !canRenderChildrenWithoutRawStorage(child) {
				return false
			}
			continue
		}
		return false
	}
	return true
}

func childrenToMarkdown(el *element, depth, maxDepth int) string {
	var blocks []string
	for _, child := range el.children {
		if child.text != "" {
			if text := plainText(child.text); text != "" {
				blocks = append(blocks, text)
			}
			continue
		}
		if child.elem != nil {
			if rendered := strings.Trim(blockToMarkdown(child.elem, depth, maxDepth), "\n"); rendered != "" {
				blocks = append(blocks, rendered)
			}
		}
	}
	return strings.Join(blocks, "\n\n")
}

func macroToMarkdown(el *element, depth, maxDepth int) string {
	name := macroName(el)
	if rendered, ok := roundTripBlockMacroToMarkdown(el); ok {
		return rendered + "\n"
	}

	if name == "excerpt" {
		return richTextContainerMacroToMarkdown(el, depth, maxDepth) + "\n"
	}

	if name == "html" {
		body := ""
		for _, child := range elementChildren(el) {
			prefix, local := splitName(child.name)
			if prefix == "ac" && local == "plain-text-body" {
				body = textContent(child)
			}
		}
		return markdownCodeFence(body, plainTextFenceInfo(el, "confluence-html"))
	}

	if name == "status" {
		if shouldUseGenericSelfClosingMacro(el, "colour", "color", "title") {
			return selfClosingStructuredMacroToMarkdown(el) + "\n"
		}
		return statusMacroToMarkdown(el) + "\n"
	}

	if name == "code" {
		body := ""
		for _, child := range elementChildren(el) {
			prefix, local := splitName(child.name)
			if prefix == "ac" && local == "plain-text-body" {
				body = textContent(child)
			}
		}
		return markdownCodeFence(body, codeFenceInfo(el))
	}

	if name == "noformat" {
		body := ""
		for _, child := range elementChildren(el) {
			prefix, local := splitName(child.name)
			if prefix == "ac" && local == "plain-text-body" {
				body = textContent(child)
			}
		}
		return markdownCodeFence(body, plainTextFenceInfo(el, "confluence-noformat"))
	}

	if name == "expand" {
		if shouldUseGenericRichTextMacro(el, "title") {
			return richTextContainerMacroToMarkdown(el, depth, maxDepth) + "\n"
		}
		title := macroParameter(el, "title")
		body := strings.TrimSpace(macroRichTextBodyMarkdown(el, depth+1, maxDepth))
		var lines []string
		lines = append(lines, "<details>")
		if title != "" {
			lines = append(lines, "<summary>"+title+"</summary>")
		}
		lines = append(lines, "")
		if body != "" {
			lines = append(lines, strings.Split(body, "\n")...)
		}
		lines = append(lines, "", "</details>")
		return strings.Join(lines, "\n") + "\n"
	}

	if name == "panel" || name == "info" || name == "note" || name == "tip" || name == "warning" {
		if shouldUseGenericRichTextMacro(el, "title") {
			return richTextContainerMacroToMarkdown(el, depth, maxDepth) + "\n"
		}
		kind := strings.ToUpper(name)
		title := macroParameter(el, "title")
		body := strings.TrimSpace(macroRichTextBodyMarkdown(el, depth+1, maxDepth))
		header := "> [!" + kind + "]"
		if title != "" {
			header += " " + title
		}
		lines := []string{header}
		if body != "" {
			for _, line := range strings.Split(body, "\n") {
				if line == "" {
					lines = append(lines, ">")
				} else {
					lines = append(lines, "> "+line)
				}
			}
		}
		return strings.Join(lines, "\n") + "\n"
	}

	if canRenderGenericStructuredMacroWithoutRawStorage(el) {
		if hasRichTextBody(el) {
			return richTextContainerMacroToMarkdown(el, depth, maxDepth) + "\n"
		}
		return selfClosingStructuredMacroToMarkdown(el) + "\n"
	}

	return rawStorageFence(elementToStorage(el))
}

func richTextContainerMacroToMarkdown(el *element, depth, maxDepth int) string {
	fields := structuredMacroFieldsFromStorage(el, textMacroOptions(el), pageReference{})
	if len(fields) == 0 {
		fields = []string{macroName(el)}
	}
	body := strings.TrimSpace(macroRichTextBodyMarkdown(el, depth+1, maxDepth))
	var lines []string
	lines = append(lines, "{{"+strings.Join(fields, " ")+"}}")
	if body != "" {
		lines = append(lines, "")
		lines = append(lines, strings.Split(body, "\n")...)
	}
	lines = append(lines, "", "{{/"+macroName(el)+"}}")
	return strings.Join(lines, "\n")
}

func selfClosingStructuredMacroToMarkdown(el *element) string {
	return "{{" + strings.Join(structuredMacroFieldsFromStorage(el, textMacroOptions(el), pageReference{}), " ") + "}}"
}

func structuredMacroFieldsFromStorage(el *element, options []macroOption, page pageReference) []string {
	fields := []string{macroName(el)}
	for _, attr := range macroAttributes(el) {
		fields = append(fields, macroAttributeMarkdownField(attr))
	}
	if page.title != "" {
		fields = append(fields, "page="+quoteRoundTripValue(page.title))
		if page.space != "" {
			fields = append(fields, "space="+quoteRoundTripValue(page.space))
		}
	}
	for _, option := range options {
		fields = append(fields, option.name+"="+quoteRoundTripValue(option.value))
	}
	return fields
}

func textMacroOptions(el *element) []macroOption {
	var options []macroOption
	for _, child := range elementChildren(el) {
		prefix, local := splitName(child.name)
		if prefix != "ac" || local != "parameter" || hasElementChild(child) {
			continue
		}
		if name, _ := attr(child, "ac", "name"); name != "" {
			options = append(options, macroOption{name: name, value: strings.TrimSpace(textContent(child))})
		}
	}
	return options
}

func macroAttributes(el *element) []macroAttribute {
	var attrs []macroAttribute
	for _, attr := range el.attr {
		if attr.Name.Space == "xmlns" || attr.Name.Local == "xmlns" {
			continue
		}
		prefix, local := splitName(attr.Name)
		if prefix == "ac" && local == "name" {
			continue
		}
		attrs = append(attrs, macroAttribute{prefix: prefix, name: local, value: attr.Value})
	}
	return attrs
}

func macroAttributeMarkdownField(attr macroAttribute) string {
	name := ""
	switch {
	case attr.prefix == "ac" && attr.name == "macro-id":
		name = "macro-id"
	case attr.prefix == "ac" && attr.name == "schema-version":
		name = "schema-version"
	case attr.prefix != "":
		name = "attr:" + attr.prefix + ":" + attr.name
	default:
		name = "attr:" + attr.name
	}
	return name + "=" + quoteRoundTripValue(attr.value)
}

func shouldUseGenericSelfClosingMacro(el *element, allowedParams ...string) bool {
	if len(macroAttributes(el)) > 0 {
		return true
	}
	return hasParameterOutside(el, allowedParams...)
}

func shouldUseGenericRichTextMacro(el *element, allowedParams ...string) bool {
	if len(macroAttributes(el)) > 0 {
		return true
	}
	return hasParameterOutside(el, allowedParams...)
}

func hasParameterOutside(el *element, allowedParams ...string) bool {
	allowed := map[string]bool{}
	for _, name := range allowedParams {
		allowed[name] = true
	}
	for _, option := range textMacroOptions(el) {
		if !allowed[option.name] {
			return true
		}
	}
	return false
}

func hasRichTextBody(el *element) bool {
	for _, child := range elementChildren(el) {
		prefix, local := splitName(child.name)
		if prefix == "ac" && local == "rich-text-body" {
			return true
		}
	}
	return false
}

func plainTextFenceInfo(el *element, firstWord string) string {
	fields := []string{firstWord}
	for _, attr := range macroAttributes(el) {
		fields = append(fields, macroAttributeMarkdownField(attr))
	}
	for _, option := range textMacroOptions(el) {
		fields = append(fields, option.name+"="+quoteRoundTripValue(option.value))
	}
	return strings.Join(fields, " ")
}

func singleStructuredMacro(el *element) (*element, bool) {
	var macro *element
	for _, child := range el.children {
		if child.text != "" {
			if strings.TrimSpace(child.text) != "" {
				return nil, false
			}
			continue
		}
		if child.elem == nil {
			continue
		}
		prefix, local := splitName(child.elem.name)
		if prefix != "ac" || local != "structured-macro" || macro != nil {
			return nil, false
		}
		macro = child.elem
	}
	return macro, macro != nil
}

func roundTripBlockMacroToMarkdown(el *element) (string, bool) {
	name := macroName(el)
	if !isRoundTripBlockMacro(name) {
		return "", false
	}

	options, page, ok := roundTripMacroOptionsFromStorage(el)
	if !ok {
		return "", false
	}
	fields := structuredMacroFieldsFromStorage(el, options, page)
	return "{{" + strings.Join(fields, " ") + "}}", true
}

func roundTripMacroOptionsFromStorage(el *element) ([]macroOption, pageReference, bool) {
	name := macroName(el)
	var options []macroOption
	var page pageReference
	for _, child := range elementChildren(el) {
		prefix, local := splitName(child.name)
		if prefix != "ac" || local != "parameter" {
			return nil, pageReference{}, false
		}
		paramName, _ := attr(child, "ac", "name")
		if (paramName == "" && (name == "include" || name == "excerpt-include")) ||
			(paramName == "page" && name == "children" && hasElementChild(child)) {
			ref, ok := parameterPageReference(child)
			if !ok || page.title != "" {
				return nil, pageReference{}, false
			}
			page = ref
			continue
		}
		if paramName == "" || hasElementChild(child) {
			return nil, pageReference{}, false
		}
		options = append(options, macroOption{name: paramName, value: strings.TrimSpace(textContent(child))})
	}

	if (name == "include" || name == "excerpt-include") && page.title == "" {
		return nil, pageReference{}, false
	}
	return options, page, true
}

func parameterPageReference(el *element) (pageReference, bool) {
	var ref pageReference
	var found bool
	var invalid bool
	var walk func(*element)
	walk = func(current *element) {
		for _, child := range elementChildren(current) {
			prefix, local := splitName(child.name)
			if prefix == "ri" && local == "page" {
				if found {
					invalid = true
					return
				}
				title, ok := attr(child, "ri", "content-title")
				if !ok || title == "" || hasUnsupportedPageAttrs(child) {
					invalid = true
					return
				}
				space, _ := attr(child, "ri", "space-key")
				ref = pageReference{title: title, space: space}
				found = true
				continue
			}
			walk(child)
			if invalid {
				return
			}
		}
	}
	walk(el)
	return ref, found && !invalid
}

func hasUnsupportedPageAttrs(el *element) bool {
	for _, attr := range el.attr {
		prefix, local := splitName(attr.Name)
		if prefix == "ri" && (local == "content-title" || local == "space-key") {
			continue
		}
		return true
	}
	return false
}

func hasElementChild(el *element) bool {
	for _, child := range el.children {
		if child.elem != nil {
			return true
		}
	}
	return false
}

func quoteRoundTripValue(value string) string {
	if value == "" {
		return `""`
	}
	for _, r := range value {
		if r >= utf8.RuneSelf {
			return quoteRoundTripString(value)
		}
	}
	if !strings.ContainsAny(value, " \t\r\n\"'\\{}=") {
		return value
	}
	return quoteRoundTripString(value)
}

func quoteRoundTripString(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
	return `"` + escaped + `"`
}

func macroName(el *element) string {
	name, _ := attr(el, "ac", "name")
	return name
}

func statusMacroToMarkdown(el *element) string {
	title := macroParameter(el, "title")
	color := macroParameter(el, "colour")
	if color == "" {
		color = macroParameter(el, "color")
	}
	if title == "" {
		title = textContent(el)
	}
	if color == "" {
		return "{{status|" + title + "}}"
	}
	return "{{status:" + color + "|" + title + "}}"
}

func codeFenceInfo(el *element) string {
	var fields []string
	if language := macroParameter(el, "language"); language != "" {
		fields = append(fields, language)
	}
	for _, attr := range macroAttributes(el) {
		fields = append(fields, macroAttributeMarkdownField(attr))
	}
	options := macroParameters(el)
	delete(options, "language")
	for _, name := range sortedCodeOptionNames(options) {
		if value := macroParameter(el, name); value != "" {
			fields = append(fields, name+"="+quoteFenceInfoValue(value))
		}
	}
	return strings.Join(fields, " ")
}

func quoteFenceInfoValue(value string) string {
	if value == "" {
		return `""`
	}
	if !strings.ContainsAny(value, " \t\"'\\") {
		return value
	}
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
	return `"` + escaped + `"`
}

func macroParameter(el *element, name string) string {
	for _, child := range elementChildren(el) {
		prefix, local := splitName(child.name)
		if prefix == "ac" && local == "parameter" {
			if paramName, _ := attr(child, "ac", "name"); paramName == name {
				return strings.TrimSpace(textContent(child))
			}
		}
	}
	return ""
}

func macroParameters(el *element) map[string]string {
	parameters := map[string]string{}
	for _, child := range elementChildren(el) {
		prefix, local := splitName(child.name)
		if prefix == "ac" && local == "parameter" && !hasElementChild(child) {
			if name, _ := attr(child, "ac", "name"); name != "" {
				parameters[name] = strings.TrimSpace(textContent(child))
			}
		}
	}
	return parameters
}

func macroRichTextBodyMarkdown(el *element, depth, maxDepth int) string {
	for _, child := range elementChildren(el) {
		prefix, local := splitName(child.name)
		if prefix == "ac" && local == "rich-text-body" {
			return childrenToMarkdown(child, depth, maxDepth)
		}
	}
	return ""
}

func storageTableToMarkdown(table *element) (string, bool) {
	var rows [][]string
	for _, row := range descendantsByLocal(table, "tr") {
		var cells []string
		for _, child := range elementChildren(row) {
			_, local := splitName(child.name)
			if local != "th" && local != "td" {
				continue
			}
			cell, ok := storageTableCellToMarkdown(child)
			if !ok {
				return "", false
			}
			cells = append(cells, strings.ReplaceAll(strings.TrimSpace(cell), "|", `\|`))
		}
		if len(cells) > 0 {
			rows = append(rows, cells)
		}
	}
	if len(rows) == 0 {
		return "", false
	}

	width := 0
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	for i := range rows {
		for len(rows[i]) < width {
			rows[i] = append(rows[i], "")
		}
	}

	var rendered []string
	rendered = append(rendered, "| "+strings.Join(rows[0], " | ")+" |")
	rendered = append(rendered, "| "+strings.Join(repeat("---", width), " | ")+" |")
	for _, row := range rows[1:] {
		rendered = append(rendered, "| "+strings.Join(row, " | ")+" |")
	}
	return strings.Join(rendered, "\n") + "\n", true
}

func storageTableCellToMarkdown(cell *element) (string, bool) {
	if canRenderInline(cell) {
		return tableInlineMarkdown(cell), true
	}

	var parts []string
	for _, child := range cell.children {
		if child.text != "" {
			if text := plainInlineText(child.text); strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
			continue
		}
		if child.elem == nil {
			continue
		}
		prefix, local := splitName(child.elem.name)
		if prefix == "" && local == "p" {
			if !canRenderInline(child.elem) {
				return "", false
			}
			parts = append(parts, strings.TrimSpace(tableInlineMarkdown(child.elem)))
			continue
		}
		if prefix == "" && local == "br" {
			parts = append(parts, "<br>")
			continue
		}
		if !canRenderInline(child.elem) {
			return "", false
		}
		parts = append(parts, strings.TrimSpace(tableInlineElementMarkdown(child.elem)))
	}
	return strings.Join(nonEmptyStrings(parts), "<br>"), true
}

func canRenderStorageTable(table *element) bool {
	hasRows := false
	for _, row := range descendantsByLocal(table, "tr") {
		hasCells := false
		for _, child := range elementChildren(row) {
			_, local := splitName(child.name)
			if local != "th" && local != "td" {
				continue
			}
			hasCells = true
			if !canRenderStorageTableCell(child) {
				return false
			}
		}
		if hasCells {
			hasRows = true
		}
	}
	return hasRows
}

func canRenderStorageTableCell(cell *element) bool {
	if canRenderInline(cell) {
		return true
	}

	for _, child := range cell.children {
		if child.elem == nil {
			continue
		}
		prefix, local := splitName(child.elem.name)
		if prefix == "" && local == "p" {
			if !canRenderInline(child.elem) {
				return false
			}
			continue
		}
		if prefix == "" && local == "br" {
			continue
		}
		if !canRenderInline(child.elem) {
			return false
		}
	}
	return true
}

func tableInlineMarkdown(el *element) string {
	return strings.ReplaceAll(inlineStorageToMarkdown(el), "  \n", "<br>")
}

func tableInlineElementMarkdown(el *element) string {
	return strings.ReplaceAll(inlineElementToMarkdown(el), "  \n", "<br>")
}

func nonEmptyStrings(values []string) []string {
	var out []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func listToMarkdown(list *element, listDepth, storageDepth, maxDepth int) string {
	_, local := splitName(list.name)
	ordered := local == "ol"
	indent := strings.Repeat("  ", listDepth)

	var lines []string
	index := 1
	for _, child := range elementChildren(list) {
		_, childLocal := splitName(child.name)
		if childLocal != "li" {
			continue
		}

		marker := "-"
		if ordered {
			marker = strconv.Itoa(index) + "."
		}
		text := strings.TrimSpace(listItemInlineMarkdown(child))
		line := indent + marker + " "
		if text != "" {
			line += text
		}
		lines = append(lines, line)

		for _, rendered := range listItemBlockMarkdowns(child, listDepth, storageDepth, maxDepth) {
			if rendered != "" {
				lines = append(lines, rendered)
			}
		}
		index++
	}
	return strings.Join(lines, "\n") + "\n"
}

func listItemInlineMarkdown(item *element) string {
	var out strings.Builder
	for _, child := range item.children {
		if child.text != "" {
			out.WriteString(plainInlineText(child.text))
			continue
		}
		if child.elem == nil {
			continue
		}
		prefix, local := splitName(child.elem.name)
		if prefix == "" && (local == "ul" || local == "ol") {
			continue
		}
		if prefix == "" && isTransparentBlock(local) && len(child.elem.attr) == 0 {
			out.WriteString(listItemInlineMarkdown(child.elem))
			continue
		}
		if prefix == "" && local == "p" {
			if _, ok := singleStructuredMacro(child.elem); ok {
				continue
			}
			out.WriteString(inlineStorageToMarkdown(child.elem))
			continue
		}
		if isListBlockElement(child.elem) {
			continue
		}
		out.WriteString(inlineElementToMarkdown(child.elem))
	}
	return out.String()
}

func listItemBlockMarkdowns(item *element, listDepth, storageDepth, maxDepth int) []string {
	var blocks []string
	for _, child := range elementChildren(item) {
		prefix, local := splitName(child.name)
		if prefix == "" && (local == "ul" || local == "ol") {
			blocks = append(blocks, strings.TrimRight(listToMarkdown(child, listDepth+1, storageDepth+1, maxDepth), "\n"))
			continue
		}
		if prefix == "" && isTransparentBlock(local) && len(child.attr) == 0 {
			blocks = append(blocks, listItemBlockMarkdowns(child, listDepth, storageDepth+1, maxDepth)...)
			continue
		}
		if isListBlockElement(child) {
			rendered := strings.TrimRight(blockToMarkdown(child, storageDepth+1, maxDepth), "\n")
			if rendered != "" {
				blocks = append(blocks, indentMarkdownBlock(rendered, strings.Repeat("  ", listDepth+1)))
			}
		}
	}
	return blocks
}

func indentMarkdownBlock(markdown, indent string) string {
	var lines []string
	for _, line := range strings.Split(markdown, "\n") {
		if line == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, indent+line)
	}
	return strings.Join(lines, "\n")
}

func isListBlockElement(el *element) bool {
	prefix, local := splitName(el.name)
	if prefix == "" && local == "p" {
		macro, ok := singleStructuredMacro(el)
		return ok && canRenderStructuredBlockMacro(macro)
	}
	if prefix == "" && (local == "pre" || local == "table") {
		return true
	}
	if prefix == "ac" && local == "structured-macro" {
		return canRenderStructuredBlockMacro(el)
	}
	return false
}

func canRenderStructuredBlockMacro(el *element) bool {
	switch macroName(el) {
	case "code", "noformat", "expand", "panel", "info", "note", "tip", "warning":
		return true
	default:
		return isRoundTripBlockMacro(macroName(el))
	}
}

func canRenderList(el *element) bool {
	for _, child := range elementChildren(el) {
		_, local := splitName(child.name)
		if local != "li" || !canRenderListItem(child) {
			return false
		}
	}
	return true
}

func canRenderListItem(el *element) bool {
	for _, child := range elementChildren(el) {
		prefix, local := splitName(child.name)
		if prefix == "" && (local == "ul" || local == "ol") {
			if !canRenderList(child) {
				return false
			}
			continue
		}
		if prefix == "" && isTransparentBlock(local) && len(child.attr) == 0 {
			if !canRenderListItem(child) {
				return false
			}
			continue
		}
		if prefix == "" && local == "p" {
			if macro, ok := singleStructuredMacro(child); ok {
				if !canRenderStructuredBlockMacro(macro) {
					return false
				}
				continue
			}
			if !canRenderInline(child) {
				return false
			}
			continue
		}
		if isListBlockElement(child) {
			if prefix == "" && local == "table" {
				if _, ok := storageTableToMarkdown(child); !ok {
					return false
				}
			}
			continue
		}
		if prefix == "ac" && (local == "link" || local == "image") {
			continue
		}
		if prefix == "ac" && local == "structured-macro" && macroName(child) == "status" {
			continue
		}
		if prefix == "" && (local == "span" || isRawInlineStorageTag(local)) {
			// Styled spans can be represented as Markdown inline HTML and
			// passed back through clean without turning the whole list raw.
		} else if !isAllowedInline(prefix, local) {
			return false
		}
		if !canRenderInline(child) {
			return false
		}
	}
	return true
}

func canRenderInline(el *element) bool {
	for _, child := range elementChildren(el) {
		prefix, local := splitName(child.name)
		if prefix == "ac" && (local == "link" || local == "image") {
			continue
		}
		if prefix == "ac" && local == "structured-macro" && macroName(child) == "status" {
			continue
		}
		if prefix == "" && (local == "span" || isRawInlineStorageTag(local)) {
			// Preserve spans with attributes as inline HTML in Markdown.
		} else if !isAllowedInline(prefix, local) {
			return false
		}
		if !canRenderInline(child) {
			return false
		}
	}
	return true
}

func isAllowedInline(prefix, local string) bool {
	if prefix == "ac" && (local == "link" || local == "image") {
		return true
	}
	if prefix != "" {
		return false
	}
	switch local {
	case "strong", "b", "em", "i", "code", "br", "sub", "sup", "a":
		return true
	default:
		return false
	}
}

func isRawInlineStorageTag(local string) bool {
	for _, tag := range rawInlineStorageTags {
		if local == tag {
			return true
		}
	}
	return false
}

func inlineStorageToMarkdown(el *element) string {
	var out strings.Builder
	for _, child := range el.children {
		if child.text != "" {
			out.WriteString(plainInlineText(child.text))
			continue
		}
		if child.elem != nil {
			out.WriteString(inlineElementToMarkdown(child.elem))
		}
	}
	return out.String()
}

func inlineElementToMarkdown(el *element) string {
	prefix, local := splitName(el.name)
	content := inlineStorageToMarkdown(el)

	if prefix == "" {
		switch local {
		case "strong", "b":
			return "**" + content + "**"
		case "em", "i":
			return "*" + content + "*"
		case "code":
			return inlineCode(content)
		case "br":
			return "  \n"
		case "span":
			if len(el.attr) > 0 {
				return elementToStorage(el)
			}
			return content
		case "a":
			if href, ok := attr(el, "", "href"); ok && href != "" {
				return "[" + content + "](" + href + ")"
			}
			return content
		}
		if isRawInlineStorageTag(local) {
			return elementToStorage(el)
		}
	}

	if prefix == "ac" && local == "link" {
		return linkStorageToMarkdown(el)
	}
	if prefix == "ac" && local == "image" {
		return imageStorageToMarkdown(el)
	}
	if prefix == "ac" && local == "structured-macro" && macroName(el) == "status" {
		return statusMacroToMarkdown(el)
	}
	return content
}

func linkStorageToMarkdown(el *element) string {
	label := ""
	target := ""
	for _, child := range elementChildren(el) {
		prefix, local := splitName(child.name)
		switch {
		case prefix == "ac" && local == "plain-text-link-body":
			label = textContent(child)
		case prefix == "ri" && local == "page":
			if title, ok := attr(child, "ri", "content-title"); ok {
				space, _ := attr(child, "ri", "space-key")
				target = confluenceMarkdownURLWithSpace("page", title, space)
			}
		case prefix == "ri" && local == "attachment":
			if filename, ok := attr(child, "ri", "filename"); ok {
				target = confluenceMarkdownURL("attachment", filename)
			}
		case prefix == "ri" && local == "url":
			target, _ = attr(child, "ri", "value")
		}
	}
	if label == "" {
		label = target
	}
	return "[" + escapeMarkdownLabel(label) + "](" + target + ")"
}

func imageStorageToMarkdown(el *element) string {
	alt, _ := attr(el, "ac", "alt")
	target := ""
	for _, child := range elementChildren(el) {
		prefix, local := splitName(child.name)
		if prefix == "ri" && local == "attachment" {
			if filename, ok := attr(child, "ri", "filename"); ok {
				target = confluenceMarkdownURL("attachment", filename)
			}
		}
		if prefix == "ri" && local == "url" {
			target, _ = attr(child, "ri", "value")
		}
	}
	return "![" + escapeMarkdownLabel(alt) + "](" + target + ")"
}

func confluenceMarkdownURL(kind, value string) string {
	return confluenceMarkdownURLWithSpace(kind, value, "")
}

func confluenceMarkdownURLWithSpace(kind, value, space string) string {
	query := ""
	if space != "" {
		query = "?space=" + url.QueryEscape(space)
	}
	return "confluence://" + kind + "/" + escapeConfluenceMarkdownPath(value) + query
}

func escapeConfluenceMarkdownPath(value string) string {
	var out strings.Builder
	for _, r := range value {
		switch {
		case r >= 'A' && r <= 'Z':
			out.WriteRune(r)
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		case r == '-' || r == '.' || r == '_' || r == '~':
			out.WriteRune(r)
		case r >= utf8.RuneSelf:
			out.WriteRune(r)
		default:
			for _, b := range []byte(string(r)) {
				out.WriteString(fmt.Sprintf("%%%02X", b))
			}
		}
	}
	return out.String()
}

func markdownCodeFence(code, language string) string {
	code = strings.TrimRight(code, "\r\n")
	fence := strings.Repeat("`", max(3, maxRun(code, '`')+1))
	return fence + strings.TrimSpace(language) + "\n" + code + "\n" + fence + "\n"
}

func inlineCode(code string) string {
	fence := strings.Repeat("`", max(1, maxRun(code, '`')+1))
	padding := ""
	if strings.HasPrefix(code, "`") || strings.HasSuffix(code, "`") {
		padding = " "
	}
	return fence + padding + code + padding + fence
}

func rawStorageFence(storage string) string {
	raw := normalizeNewlines(storage)
	fence := strings.Repeat("`", max(3, maxRun(raw, '`')+1))
	var out strings.Builder
	out.WriteString(fence)
	out.WriteString("confluence-storage\n")
	out.WriteString(raw)
	if !strings.HasSuffix(raw, "\n") {
		out.WriteByte('\n')
	}
	out.WriteString(fence)
	out.WriteByte('\n')
	return out.String()
}

func maxRun(text string, marker byte) int {
	maxSeen := 0
	current := 0
	for i := 0; i < len(text); i++ {
		if text[i] == marker {
			current++
			if current > maxSeen {
				maxSeen = current
			}
		} else {
			current = 0
		}
	}
	return maxSeen
}

func textContent(el *element) string {
	var out strings.Builder
	var walk func(*element)
	walk = func(current *element) {
		for _, child := range current.children {
			if child.text != "" {
				out.WriteString(child.text)
			} else if child.elem != nil {
				walk(child.elem)
			}
		}
	}
	walk(el)
	return out.String()
}

func plainText(text string) string {
	return strings.TrimSpace(spaceRE.ReplaceAllString(text, " "))
}

func plainInlineText(text string) string {
	return spaceRE.ReplaceAllString(text, " ")
}

func escapeMarkdownLabel(text string) string {
	text = strings.ReplaceAll(text, `[`, `\[`)
	return strings.ReplaceAll(text, `]`, `\]`)
}

func splitName(name xml.Name) (string, string) {
	switch name.Space {
	case acNS:
		return "ac", name.Local
	case riNS:
		return "ri", name.Local
	default:
		return "", name.Local
	}
}

func attr(el *element, prefix, local string) (string, bool) {
	space := ""
	switch prefix {
	case "ac":
		space = acNS
	case "ri":
		space = riNS
	}
	for _, attr := range el.attr {
		if attr.Name.Space == space && attr.Name.Local == local {
			return attr.Value, true
		}
	}
	return "", false
}

func elementChildren(el *element) []*element {
	var children []*element
	for _, child := range el.children {
		if child.elem != nil {
			children = append(children, child.elem)
		}
	}
	return children
}

func hasNonWhitespaceText(el *element) bool {
	for _, child := range el.children {
		if child.text != "" && strings.TrimSpace(child.text) != "" {
			return true
		}
	}
	return false
}

func descendantsByLocal(el *element, local string) []*element {
	var result []*element
	var walk func(*element)
	walk = func(current *element) {
		for _, child := range elementChildren(current) {
			_, childLocal := splitName(child.name)
			if childLocal == local {
				result = append(result, child)
			}
			walk(child)
		}
	}
	walk(el)
	return result
}

func elementToStorage(el *element) string {
	var out strings.Builder
	usesAC := usesNamespace(el, acNS)
	usesRI := usesNamespace(el, riNS)
	writeElement(&out, el, true, usesAC, usesRI)
	return out.String()
}

func writeElement(out *strings.Builder, el *element, root bool, declareAC bool, declareRI bool) {
	out.WriteByte('<')
	out.WriteString(qualifiedName(el.name))
	if root && declareAC {
		out.WriteString(` xmlns:ac="` + acNS + `"`)
	}
	if root && declareRI {
		out.WriteString(` xmlns:ri="` + riNS + `"`)
	}
	for _, attr := range el.attr {
		if attr.Name.Space == "xmlns" || attr.Name.Local == "xmlns" {
			continue
		}
		out.WriteByte(' ')
		out.WriteString(qualifiedName(attr.Name))
		out.WriteString(`="`)
		out.WriteString(escapeAttr(attr.Value))
		out.WriteByte('"')
	}
	if len(el.children) == 0 {
		out.WriteString(" />")
		return
	}
	out.WriteByte('>')
	for _, child := range el.children {
		if child.text != "" {
			out.WriteString(escapeText(child.text))
		} else if child.elem != nil {
			writeElement(out, child.elem, false, false, false)
		}
	}
	out.WriteString("</")
	out.WriteString(qualifiedName(el.name))
	out.WriteByte('>')
}

func usesNamespace(el *element, namespace string) bool {
	if el.name.Space == namespace {
		return true
	}
	for _, attr := range el.attr {
		if attr.Name.Space == namespace {
			return true
		}
	}
	for _, child := range elementChildren(el) {
		if usesNamespace(child, namespace) {
			return true
		}
	}
	return false
}

func qualifiedName(name xml.Name) string {
	switch name.Space {
	case acNS:
		return "ac:" + name.Local
	case riNS:
		return "ri:" + name.Local
	default:
		return name.Local
	}
}

func escapeText(text string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(text)
}

func escapeAttr(text string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", `'`, "&apos;")
	return replacer.Replace(text)
}

func repeat(value string, count int) []string {
	values := make([]string, count)
	for i := range values {
		values[i] = value
	}
	return values
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func requiresExactRawStorage(storage string) bool {
	return false
}
