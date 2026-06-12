package confluence

import (
	"strings"
	"testing"
)

func TestMarkdownToStorageBasicBlocks(t *testing.T) {
	markdown := `# 吾輩は猫である

名前は**まだ** *無い*。` + "`吾輩`" + `は[猫](https://example.com)である。

- 吾輩は猫である
- 名前はまだ無い

` + "```python" + `
print("吾輩は猫である。名前はまだ無い。")
` + "```" + `
`

	storage := MarkdownToStorage(markdown)

	assertContains(t, storage, "<h1>吾輩は猫である</h1>")
	assertContains(t, storage, "<strong>まだ</strong>")
	assertContains(t, storage, "<em>無い</em>")
	assertContains(t, storage, `<a href="https://example.com">猫</a>`)
	assertContains(t, storage, "<ul>")
	assertContains(t, storage, `ac:name="code"`)
	assertContains(t, storage, `ac:name="language">python</ac:parameter>`)
}

func TestStorageToMarkdownCodeMacro(t *testing.T) {
	storage := `<ac:structured-macro ac:name="code">
<ac:parameter ac:name="language">python</ac:parameter>
<ac:plain-text-body>print(&quot;吾輩は猫である。名前はまだ無い。&quot;)</ac:plain-text-body>
</ac:structured-macro>`

	markdown := StorageToMarkdown(storage)

	if markdown != "```python\nprint(\"吾輩は猫である。名前はまだ無い。\")\n```\n" {
		t.Fatalf("unexpected markdown:\n%s", markdown)
	}
}

func TestCodeMacroMetadataRoundTrip(t *testing.T) {
	storage := `<ac:structured-macro ac:macro-id="554d7df7-f9b2-4fa4-9e0b-e8007ba29890" ac:name="code" ac:schema-version="1"><ac:parameter ac:name="language">bash</ac:parameter><ac:plain-text-body><![CDATA[$ echo > out
PS C:\Users\admin> dir]]></ac:plain-text-body></ac:structured-macro>`

	markdown := StorageToMarkdown(storage)
	cleaned := MarkdownToStorage(markdown)

	if strings.Contains(markdown, "confluence-storage") {
		t.Fatalf("unexpected raw storage fence:\n%s", markdown)
	}
	assertContains(t, markdown, "```bash macro-id=554d7df7-f9b2-4fa4-9e0b-e8007ba29890 schema-version=1")
	assertContains(t, markdown, "$ echo > out")
	assertContains(t, cleaned, `ac:macro-id="554d7df7-f9b2-4fa4-9e0b-e8007ba29890"`)
	assertContains(t, cleaned, `ac:schema-version="1"`)
	assertContains(t, cleaned, `$ echo &gt; out`)
}

func TestCodeMacroMetadataWithoutLanguageRoundTrip(t *testing.T) {
	storage := `<ac:structured-macro ac:name="code" ac:schema-version="1" ac:macro-id="e8e6493e-635d-4350-b087-b902805e10be"><ac:plain-text-body><![CDATA[echo "名前はまだ無い"]]></ac:plain-text-body></ac:structured-macro>`

	markdown := StorageToMarkdown(storage)
	cleaned := MarkdownToStorage(markdown)

	if strings.Contains(markdown, "confluence-storage") {
		t.Fatalf("unexpected raw storage fence:\n%s", markdown)
	}
	assertContains(t, markdown, "```schema-version=1 macro-id=e8e6493e-635d-4350-b087-b902805e10be")
	assertContains(t, cleaned, `ac:schema-version="1"`)
	assertContains(t, cleaned, `ac:macro-id="e8e6493e-635d-4350-b087-b902805e10be"`)
	if strings.Contains(cleaned, `ac:name="language"`) {
		t.Fatalf("unexpected language parameter:\n%s", cleaned)
	}
}

func TestCodeMacroParametersRoundTrip(t *testing.T) {
	markdown := "```python title=\"script.py\" linenumbers=true collapse=true theme=RDark\nprint(\"吾輩は猫である。名前はまだ無い。\")\n```\n"

	storage := MarkdownToStorage(markdown)
	rendered := StorageToMarkdown(storage)

	assertContains(t, storage, `ac:name="title">script.py</ac:parameter>`)
	assertContains(t, storage, `ac:name="linenumbers">true</ac:parameter>`)
	assertContains(t, storage, `ac:name="collapse">true</ac:parameter>`)
	assertContains(t, storage, `ac:name="theme">RDark</ac:parameter>`)
	if rendered != "```python title=script.py linenumbers=true theme=RDark collapse=true\nprint(\"吾輩は猫である。名前はまだ無い。\")\n```\n" {
		t.Fatalf("unexpected code macro round trip:\n%s", rendered)
	}
}

func TestInlineSpacingIsPreserved(t *testing.T) {
	storage := "<p>吾輩は<strong>猫</strong>である。名前はまだ無い。</p>"

	markdown := StorageToMarkdown(storage)

	if markdown != "吾輩は**猫**である。名前はまだ無い。\n" {
		t.Fatalf("unexpected markdown: %q", markdown)
	}
}

func TestConfluenceLinksAndAttachments(t *testing.T) {
	markdown := "[吾輩](confluence://page/%E5%90%BE%E8%BC%A9)\n\n![猫](confluence://attachment/neko.png)\n"

	storage := MarkdownToStorage(markdown)
	roundTripped := StorageToMarkdown(storage)

	assertContains(t, roundTripped, "[吾輩](confluence://page/吾輩)")
	assertContains(t, roundTripped, "![猫](confluence://attachment/neko.png)")
}

func TestSpaceKeyPageLinkRoundTrip(t *testing.T) {
	storage := `<p><ac:link><ri:page ri:space-key="NEKO" ri:content-title="吾輩は猫である" /></ac:link> 名前はまだ無い。</p>`

	markdown := StorageToMarkdown(storage)
	cleaned := MarkdownToStorage(markdown)

	if strings.Contains(markdown, "confluence-storage") {
		t.Fatalf("unexpected raw storage fence:\n%s", markdown)
	}
	assertContains(t, markdown, "confluence://page/吾輩は猫である?space=NEKO")
	assertContains(t, cleaned, `ri:space-key="NEKO"`)
	assertContains(t, cleaned, `ri:content-title="吾輩は猫である"`)
}

func TestUnknownMacroRoundTripsAsStructuredMacroTemplate(t *testing.T) {
	storage := `<ac:structured-macro ac:name="jira"><ac:parameter ac:name="key">PROJ-1</ac:parameter></ac:structured-macro>`

	markdown := StorageToMarkdown(storage)
	cleaned := MarkdownToStorage(markdown)

	if strings.Contains(markdown, "confluence-storage") {
		t.Fatalf("unexpected raw storage fence:\n%s", markdown)
	}
	assertContains(t, markdown, "{{jira key=PROJ-1}}")
	assertContains(t, cleaned, `ac:name="jira"`)
	assertContains(t, cleaned, `ac:name="key"`)
}

func TestRoundTripBlockMacros(t *testing.T) {
	markdown := `{{toc maxLevel=3 outline=true}}

{{children depth=5}}

{{include page="吾輩は猫である"}}

{{excerpt-include page="名前はまだ無い" nopanel=true}}
`

	storage := MarkdownToStorage(markdown)
	rendered := StorageToMarkdown(storage)

	assertContains(t, storage, `ac:name="toc"`)
	assertContains(t, storage, `ac:name="children"`)
	assertContains(t, storage, `ac:name="include"`)
	assertContains(t, storage, `ri:content-title="吾輩は猫である"`)
	assertContains(t, storage, `ac:name="excerpt-include"`)
	if rendered != markdown {
		t.Fatalf("unexpected round-trip macro markdown:\n%s", rendered)
	}
}

func TestExcerptHtmlRoundTrip(t *testing.T) {
	storage := `<ac:structured-macro xmlns:ac="http://atlassian.com/content" ac:name="excerpt"><ac:parameter ac:name="atlassian-macro-output-type">INLINE</ac:parameter><ac:rich-text-body><p><br /></p><ac:structured-macro ac:name="html"><ac:plain-text-body>&lt;script&gt;alert("吾輩は猫である。名前はまだ無い。")&lt;/script&gt;</ac:plain-text-body></ac:structured-macro></ac:rich-text-body></ac:structured-macro>`

	markdown := StorageToMarkdown(storage)
	cleaned := MarkdownToStorage(markdown)

	if strings.Contains(markdown, "confluence-storage") {
		t.Fatalf("unexpected raw storage fence:\n%s", markdown)
	}
	assertContains(t, markdown, "{{excerpt atlassian-macro-output-type=INLINE}}")
	assertContains(t, markdown, "```confluence-html")
	assertContains(t, markdown, `<script>alert("吾輩は猫である。名前はまだ無い。")</script>`)
	assertContains(t, markdown, "{{/excerpt}}")
	assertContains(t, cleaned, `ac:name="excerpt"`)
	assertContains(t, cleaned, `ac:name="html"`)
	assertContains(t, cleaned, `&lt;script&gt;alert("吾輩は猫である。名前はまだ無い。")&lt;/script&gt;`)
}

func TestParagraphWrappedRoundTripMacro(t *testing.T) {
	storage := `<p xmlns:ac="http://atlassian.com/content"><ac:structured-macro ac:name="children"><ac:parameter ac:name="depth">5</ac:parameter></ac:structured-macro></p>`

	markdown := StorageToMarkdown(storage)

	if markdown != "{{children depth=5}}\n" {
		t.Fatalf("unexpected paragraph wrapped macro:\n%s", markdown)
	}
}

func TestChildrenPageRoundTripMacro(t *testing.T) {
	storage := `<p xmlns:ac="http://atlassian.com/content" xmlns:ri="http://atlassian.com/resource/identifier"><ac:structured-macro ac:name="children"><ac:parameter ac:name="depth">2</ac:parameter><ac:parameter ac:name="page"><ac:link><ri:page ri:content-title="吾輩は猫である" /></ac:link></ac:parameter></ac:structured-macro></p>`

	markdown := StorageToMarkdown(storage)
	cleaned := MarkdownToStorage(markdown)

	if markdown != "{{children page=\"吾輩は猫である\" depth=2}}\n" {
		t.Fatalf("unexpected children page macro:\n%s", markdown)
	}
	assertContains(t, cleaned, `ac:name="page"`)
	assertContains(t, cleaned, `ri:content-title="吾輩は猫である"`)
}

func TestMarkdownTable(t *testing.T) {
	markdown := `| 吾輩 | 猫 |
| --- | --- |
| 名前 | まだ無い |
`

	storage := MarkdownToStorage(markdown)
	rendered := StorageToMarkdown(storage)

	assertContains(t, storage, "<table>")
	if rendered != markdown {
		t.Fatalf("unexpected table markdown:\n%s", rendered)
	}
}

func TestStorageTableWithParagraphCells(t *testing.T) {
	storage := `<table class="wrapped"><tbody><tr><th>吾輩</th><th>猫</th></tr><tr><td><p>名前<br />まだ無い</p></td><td class="highlight-yellow">である</td></tr></tbody></table>`

	markdown := StorageToMarkdown(storage)
	cleaned := MarkdownToStorage(markdown)

	if strings.Contains(markdown, "confluence-storage") {
		t.Fatalf("unexpected raw storage fence:\n%s", markdown)
	}
	assertContains(t, markdown, "| 吾輩 | 猫 |")
	assertContains(t, markdown, "名前<br>まだ無い")
	assertContains(t, cleaned, "<table>")
	assertContains(t, cleaned, "<br />")
}

func TestListItemTableStorageToMarkdown(t *testing.T) {
	storage := `<ul><li><p>吾輩は猫である</p><table><tbody><tr><th>吾輩</th><th>猫</th></tr><tr><td><p>名前</p></td><td>まだ無い</td></tr></tbody></table></li></ul>`

	markdown := StorageToMarkdown(storage)

	if strings.Contains(markdown, "confluence-storage") {
		t.Fatalf("unexpected raw storage fence:\n%s", markdown)
	}
	assertContains(t, markdown, "- 吾輩は猫である\n  | 吾輩 | 猫 |")
}

func TestSingleColumnLayoutStorageToMarkdown(t *testing.T) {
	storage := `<ac:layout xmlns:ac="http://atlassian.com/content"><ac:layout-section ac:type="single"><ac:layout-cell><h1>吾輩は猫である</h1><p>名前はまだ無い。</p></ac:layout-cell></ac:layout-section></ac:layout>`

	markdown := StorageToMarkdown(storage)

	if strings.Contains(markdown, "confluence-storage") {
		t.Fatalf("unexpected raw storage fence:\n%s", markdown)
	}
	assertContains(t, markdown, "# 吾輩は猫である")
	assertContains(t, markdown, "名前はまだ無い。")
}

func TestNestedSingleColumnLayoutStorageToMarkdown(t *testing.T) {
	storage := `<ac:layout xmlns:ac="http://atlassian.com/content"><ac:layout-section ac:type="single"><ac:layout-cell><ac:layout><ac:layout-section ac:type="single"><ac:layout-cell><p>名前はまだ無い。</p></ac:layout-cell></ac:layout-section></ac:layout></ac:layout-cell></ac:layout-section></ac:layout>`

	markdown := StorageToMarkdown(storage)

	if strings.Contains(markdown, "confluence-storage") {
		t.Fatalf("unexpected raw storage fence:\n%s", markdown)
	}
	assertContains(t, markdown, "名前はまだ無い。")
}

func TestSingleColumnLayoutWithGenericStructuredMacroToMarkdown(t *testing.T) {
	storage := `<ac:layout xmlns:ac="http://atlassian.com/content"><ac:layout-section ac:type="single"><ac:layout-cell><ac:structured-macro ac:name="jira"><ac:parameter ac:name="key">PROJ-1</ac:parameter></ac:structured-macro></ac:layout-cell></ac:layout-section></ac:layout>`

	markdown := StorageToMarkdown(storage)

	if strings.Contains(markdown, "confluence-storage") {
		t.Fatalf("unexpected raw storage fence:\n%s", markdown)
	}
	assertContains(t, markdown, "{{jira key=PROJ-1}}")
}

func TestMultiColumnLayoutIsRawStorage(t *testing.T) {
	storage := `<ac:layout xmlns:ac="http://atlassian.com/content"><ac:layout-section ac:type="two_equal"><ac:layout-cell><p>吾輩は猫である。</p></ac:layout-cell><ac:layout-cell><p>名前はまだ無い。</p></ac:layout-cell></ac:layout-section></ac:layout>`

	markdown := StorageToMarkdown(storage)

	assertContains(t, markdown, "```confluence-storage")
}

func TestJapaneseTextRoundTrip(t *testing.T) {
	markdown := "# 吾輩は猫である\n\n名前はまだ無い。[猫](confluence://page/猫)。\n"

	storage := MarkdownToStorage(markdown)
	rendered := StorageToMarkdown(storage)

	if rendered != markdown {
		t.Fatalf("unexpected round trip:\n%s", rendered)
	}
}

func TestNestedListAndStyledSpanStorageToMarkdown(t *testing.T) {
	storage := `<ul><li>吾輩は猫である。<ul><li>名前はまだ無い。</li></ul></li><li><span style="color: rgb(255,0,0);">吾輩</span>は猫である。</li></ul>`

	markdown := StorageToMarkdown(storage)

	if strings.Contains(markdown, "confluence-storage") {
		t.Fatalf("unexpected raw storage fence:\n%s", markdown)
	}
	assertContains(t, markdown, "吾輩は猫である。")
	assertContains(t, markdown, "  - 名前はまだ無い。")
	assertContains(t, markdown, `- <span style="color: rgb(255,0,0);">吾輩</span>は猫である。`)
}

func TestNestedMarkdownListRoundTrip(t *testing.T) {
	markdown := "- 吾輩は猫である\n  - 名前はまだ無い\n- <span style=\"color: rgb(255,0,0);\">吾輩</span>は猫である\n"

	storage := MarkdownToStorage(markdown)
	rendered := StorageToMarkdown(storage)

	if rendered != markdown {
		t.Fatalf("unexpected nested list round trip:\n%s", rendered)
	}
}

func TestListItemCodeBlockRoundTrip(t *testing.T) {
	storage := `<ul xmlns:ac="http://atlassian.com/content"><li><p>吾輩は猫である</p><ac:structured-macro ac:name="code"><ac:parameter ac:name="language">bash</ac:parameter><ac:plain-text-body>echo "名前はまだ無い。"
- 名前はまだ無い</ac:plain-text-body></ac:structured-macro></li></ul>`

	markdown := StorageToMarkdown(storage)
	cleaned := MarkdownToStorage(markdown)

	if strings.Contains(markdown, "confluence-storage") {
		t.Fatalf("unexpected raw storage fence:\n%s", markdown)
	}
	assertContains(t, markdown, "- 吾輩は猫である\n  ```bash\n  echo \"名前はまだ無い。\"\n  - 名前はまだ無い\n  ```")
	assertContains(t, cleaned, `<li>吾輩は猫である`)
	assertContains(t, cleaned, `ac:name="code"`)
	assertContains(t, cleaned, `echo "名前はまだ無い。"`)
}

func TestListItemNoformatRoundTrip(t *testing.T) {
	storage := `<ul xmlns:ac="http://atlassian.com/content"><li><p>吾輩は猫である</p><ac:structured-macro ac:name="noformat"><ac:plain-text-body>名前はまだ無い。
吾輩は猫である。</ac:plain-text-body></ac:structured-macro></li></ul>`

	markdown := StorageToMarkdown(storage)
	cleaned := MarkdownToStorage(markdown)

	if strings.Contains(markdown, "confluence-storage") {
		t.Fatalf("unexpected raw storage fence:\n%s", markdown)
	}
	assertContains(t, markdown, "- 吾輩は猫である\n  ```confluence-noformat\n  名前はまだ無い。")
	assertContains(t, cleaned, `ac:name="noformat"`)
	assertContains(t, cleaned, `名前はまだ無い。`)
}

func TestListItemExpandRoundTrip(t *testing.T) {
	markdown := `- 吾輩は猫である
  <details>
  <summary>名前はまだ無い</summary>

  吾輩は猫である。名前はまだ無い。

  - 吾輩は猫である

  </details>
`

	storage := MarkdownToStorage(markdown)
	rendered := StorageToMarkdown(storage)

	assertContains(t, storage, `ac:name="expand"`)
	assertContains(t, storage, `<ul>`)
	if rendered != markdown {
		t.Fatalf("unexpected list expand round trip:\n%s", rendered)
	}
}

func TestTransparentWrappersAndInlineHTMLStorageToMarkdown(t *testing.T) {
	storage := `<div><p>吾輩は<u>猫</u>である。<kbd>名前</kbd>はまだ無い。</p><ul><li><div><p>吾輩は<mark>猫</mark>である。</p><ul><li>名前はまだ無い。</li></ul></div></li></ul></div>`

	markdown := StorageToMarkdown(storage)

	if strings.Contains(markdown, "confluence-storage") {
		t.Fatalf("unexpected raw storage fence:\n%s", markdown)
	}
	assertContains(t, markdown, "吾輩は<u>猫</u>である。<kbd>名前</kbd>はまだ無い。")
	assertContains(t, markdown, "- 吾輩は<mark>猫</mark>である。")
	assertContains(t, markdown, "  - 名前はまだ無い。")
}

func TestRawInlineHTMLRoundTrip(t *testing.T) {
	markdown := "吾輩は<u>猫</u>である。<del>名前</del>は<kbd>まだ無い</kbd>\n"

	storage := MarkdownToStorage(markdown)
	rendered := StorageToMarkdown(storage)

	if rendered != markdown {
		t.Fatalf("unexpected inline HTML round trip:\n%s", rendered)
	}
}

func TestStatusMacroRoundTrip(t *testing.T) {
	markdown := "吾輩は{{status:green|猫}}である\n"

	storage := MarkdownToStorage(markdown)
	rendered := StorageToMarkdown(storage)

	assertContains(t, storage, `ac:name="status"`)
	assertContains(t, storage, `ac:name="colour">green</ac:parameter>`)
	if rendered != markdown {
		t.Fatalf("unexpected status round trip:\n%s", rendered)
	}
}

func TestExpandMacroRoundTrip(t *testing.T) {
	markdown := `<details>
<summary>吾輩は猫である</summary>

名前はまだ無い。

- 吾輩は猫である

</details>
`

	storage := MarkdownToStorage(markdown)
	rendered := StorageToMarkdown(storage)

	assertContains(t, storage, `ac:name="expand"`)
	assertContains(t, storage, `ac:name="title">吾輩は猫である</ac:parameter>`)
	if rendered != markdown {
		t.Fatalf("unexpected expand round trip:\n%s", rendered)
	}
}

func TestMarkdownToStorageWithMaxDepthRejectsDeepRichTextMacros(t *testing.T) {
	markdown := `<details>
<summary>吾輩は猫である</summary>

<details>
<summary>名前はまだ無い</summary>

吾輩は猫である。

</details>

</details>
`

	if _, err := MarkdownToStorageWithMaxDepth(markdown, 2); err == nil {
		t.Fatalf("expected recursion depth error")
	} else if !strings.Contains(err.Error(), "recursion depth exceeded") {
		t.Fatalf("unexpected error: %v", err)
	}

	storage, err := MarkdownToStorageWithMaxDepth(markdown, 3)
	if err != nil {
		t.Fatalf("unexpected conversion error: %v", err)
	}
	assertContains(t, storage, `ac:name="expand"`)
}

func TestStorageToMarkdownWithMaxDepthUsesRawStorageBlock(t *testing.T) {
	storage := `<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">吾輩は猫である</ac:parameter><ac:rich-text-body><ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">名前はまだ無い</ac:parameter><ac:rich-text-body><p>吾輩は猫である。</p></ac:rich-text-body></ac:structured-macro></ac:rich-text-body></ac:structured-macro>`

	markdown, err := StorageToMarkdownWithMaxDepth(storage, 1)
	if err != nil {
		t.Fatalf("unexpected conversion error: %v", err)
	}
	assertContains(t, markdown, "<details>")
	assertContains(t, markdown, "```confluence-storage")
	assertContains(t, markdown, `ac:name="expand"`)

	cleaned, err := MarkdownToStorageWithMaxDepth(markdown, 1)
	if err != nil {
		t.Fatalf("raw storage block did not clean at the same depth limit: %v", err)
	}
	assertContains(t, cleaned, `ac:name="expand"`)
	assertContains(t, cleaned, `吾輩は猫である。`)
}

func TestPanelMacroRoundTrip(t *testing.T) {
	markdown := `> [!PANEL] 吾輩は猫である
> 名前はまだ無い。
`

	storage := MarkdownToStorage(markdown)
	rendered := StorageToMarkdown(storage)

	assertContains(t, storage, `ac:name="panel"`)
	assertContains(t, storage, `ac:name="title">吾輩は猫である</ac:parameter>`)
	if rendered != markdown {
		t.Fatalf("unexpected panel round trip:\n%s", rendered)
	}
}

func TestStorageMacrosToMarkdown(t *testing.T) {
	storage := `<ac:structured-macro ac:name="status"><ac:parameter ac:name="colour">red</ac:parameter><ac:parameter ac:name="title">猫</ac:parameter></ac:structured-macro>
<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">吾輩は猫である</ac:parameter><ac:rich-text-body><p>名前はまだ無い。</p></ac:rich-text-body></ac:structured-macro>
<ac:structured-macro ac:name="panel"><ac:parameter ac:name="title">吾輩は猫である</ac:parameter><ac:rich-text-body><p>名前はまだ無い。</p></ac:rich-text-body></ac:structured-macro>`

	markdown := StorageToMarkdown(storage)

	if strings.Contains(markdown, "confluence-storage") {
		t.Fatalf("unexpected raw storage fence:\n%s", markdown)
	}
	assertContains(t, markdown, "{{status:red|猫}}")
	assertContains(t, markdown, "<details>")
	assertContains(t, markdown, "<summary>吾輩は猫である</summary>")
	assertContains(t, markdown, "> [!PANEL] 吾輩は猫である")
}

func TestGenericStructuredMacroAttributesRoundTrip(t *testing.T) {
	storage := `<ac:structured-macro ac:name="info" ac:schema-version="1" ac:macro-id="90a06dbb-7d7a-4b92-8492-20fa2a5bde12"><ac:parameter ac:name="icon">false</ac:parameter><ac:rich-text-body><p>教師というものは実に楽なものだ。</p><p><ac:structured-macro ac:name="status" ac:schema-version="1" ac:macro-id="9bbb1a94-622f-4222-aab3-c09c2029be73"><ac:parameter ac:name="colour">Green</ac:parameter><ac:parameter ac:name="title">名前はまだ無い</ac:parameter></ac:structured-macro></p><ul><li>猫にでも出来ぬ事はない</li></ul></ac:rich-text-body></ac:structured-macro>`

	markdown := StorageToMarkdown(storage)
	cleaned := MarkdownToStorage(markdown)

	if strings.Contains(markdown, "confluence-storage") {
		t.Fatalf("unexpected raw storage fence:\n%s", markdown)
	}
	assertContains(t, markdown, `{{info schema-version=1 macro-id=90a06dbb-7d7a-4b92-8492-20fa2a5bde12 icon=false}}`)
	assertContains(t, markdown, `{{status schema-version=1 macro-id=9bbb1a94-622f-4222-aab3-c09c2029be73 colour=Green title="名前はまだ無い"}}`)
	assertContains(t, cleaned, `ac:name="info"`)
	assertContains(t, cleaned, `ac:schema-version="1"`)
	assertContains(t, cleaned, `ac:macro-id="90a06dbb-7d7a-4b92-8492-20fa2a5bde12"`)
	assertContains(t, cleaned, `<ac:parameter ac:name="icon">false</ac:parameter>`)
	assertContains(t, cleaned, `ac:macro-id="9bbb1a94-622f-4222-aab3-c09c2029be73"`)
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected %q in:\n%s", needle, haystack)
	}
}
