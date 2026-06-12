# Conversion Rules For Confluence Storage XML And Markdown

Japanese: [conversion-rules_ja.md](conversion-rules_ja.md)

This filter stores Confluence storage XML in Git and exposes Markdown in the
working tree.

## Basic Rules

| Markdown | Confluence storage XML |
| --- | --- |
| `#` through `######` | `<h1>` through `<h6>` |
| paragraph | `<p>` |
| `**bold**` | `<strong>` |
| `*italic*` | `<em>` |
| `` `code` `` | `<code>` |
| `[label](https://example.com)` | `<a href="https://example.com">label</a>` |
| `![alt](https://example.com/a.png)` | `<ac:image><ri:url ... /></ac:image>` |
| `- item` / `1. item` | `<ul>` / `<ol>` |
| nested list | nested `<ul>` / `<ol>` |
| fenced code block | Confluence `code` structured macro |
| Markdown pipe table | `<table>` |
| `---` | `<hr />` |

## Confluence-Specific Rules

### Confluence Page Links

Confluence page links are represented in Markdown as
`confluence://page/<title>`.

```markdown
[Ishmael](confluence://page/Ishmael)
[The Pequod](confluence://page/The%20Pequod?space=SEA)
```

On clean, these links are converted to
`<ac:link><ri:page ri:content-title="Ishmael" />...`. When the `space` query
parameter is present, it is restored as `ri:space-key`.

### Confluence Attachments

Attachment references are represented as `confluence://attachment/<filename>`.

```markdown
![Queequeg's harpoon](confluence://attachment/queequeg-harpoon.png)
```

On clean, this becomes
`<ac:image><ri:attachment ri:filename="queequeg-harpoon.png" /></ac:image>`.

### Code Macro

Markdown fenced code blocks are converted to Confluence `code` structured
macros. The language name is stored as `ac:parameter ac:name="language"`.
`title` and `linenumbers` are preserved as key/value pairs in the fence info
string.

````markdown
```python title=script.py linenumbers=true
print("Call me Ishmael.")
```
````

Other key/value pairs such as `theme` and `collapse` are also preserved as
`code` macro parameters.

### Noformat Macro

Confluence `noformat` macros are emitted in Markdown as a dedicated fenced
block.

````markdown
```confluence-noformat
Some years ago--never mind how long precisely.
```
````

On clean, this is restored as an `ac:name="noformat"` macro.

### Status Macro

Confluence `status` macros are emitted in Markdown as a compact inline syntax.

```markdown
{{status:green|Ahab}}
```

On clean, this is restored as an `ac:name="status"` macro. When the color is
omitted, `{{status|Ahab}}` is accepted.

### Expand Macro

Confluence `expand` macros are emitted in Markdown as HTML `<details>` blocks.

```markdown
<details>
<summary>The Quarter-Deck</summary>

Ahab studies the white whale's course.

</details>
```

On clean, this is restored as an `ac:name="expand"` macro.

### Info / Note / Tip / Warning / Panel Macro

Confluence `info`, `note`, `tip`, `warning`, and `panel` macros are emitted in
Markdown as block quotes close to GitHub-style admonitions.

```markdown
> [!INFO] The Pequod
> The ship is ready for a long voyage.

> [!PANEL] Ahab's Chart
> The course bends toward the white whale.
```

On clean, these are restored as the corresponding Confluence macros.

### TOC / Children / Include / Excerpt Include Macro

Confluence macros that are resolved dynamically by Confluence are emitted in
Markdown as short standalone lines.

```markdown
{{toc maxLevel=3 outline=true}}
{{children depth=5}}
{{children page="The Pequod" depth=2}}
{{include page="Ishmael"}}
{{excerpt-include page="Ahab's Chart" nopanel=true}}
```

On clean, these are restored as the corresponding Confluence structured macros.
For `include` and `excerpt-include`, the `page` value is represented in
Confluence storage XML as `ri:page ri:content-title` inside an
`ac:parameter ac:name=""` element. Add `space=SPACEKEY` to reference a page in
another space.

### Generic Structured Macro Template

Confluence structured macros that can be represented with simple
`ac:parameter`, `ac:rich-text-body`, and macro attributes can be emitted in
Markdown as `{{...}}` templates.

```markdown
{{jira key=PROJ-1}}

{{info macro-id=90a06dbb-7d7a-4b92-8492-20fa2a5bde12 schema-version=1 icon=false}}

The sea is calm tonight.

{{status macro-id=9bbb1a94-622f-4222-aab3-c09c2029be73 schema-version=1 colour=Green title="Pequod"}}

{{/info}}
```

On clean, the opening token name becomes `ac:name`, and the body becomes
`ac:rich-text-body`. Attributes and parameters are assigned as follows:

- `macro-id` and `ac:macro-id` become the `ac:macro-id` attribute.
- `schema-version` and `ac:schema-version` become the `ac:schema-version` attribute.
- `attr:name`, `attr:ac:name`, and `attr:ri:name` become XML attributes with no
  prefix, the `ac:` prefix, and the `ri:` prefix respectively.
- Other `key=value` pairs become `<ac:parameter ac:name="key">value</ac:parameter>`.

The same attribute syntax can be used in the fence info string for `code`,
`noformat`, and `html` macros.

````markdown
```bash macro-id=554d7df7-f9b2-4fa4-9e0b-e8007ba29890 schema-version=1
echo "The Pequod is ready"
```
````

Confluence storage XML `CDATA` is emitted as normal fenced block text in
Markdown. On clean, it is restored as character data inside
`ac:plain-text-body`; the XML spelling as `CDATA` itself is not preserved.

### Excerpt / HTML Macro

Confluence `excerpt` macros are emitted as round-trip containers with a body.
`html` macros are emitted as dedicated fenced blocks.

````markdown
{{excerpt atlassian-macro-output-type=INLINE}}

```confluence-html
<script>
console.log("The Pequod sails at dawn")
</script>
```

{{/excerpt}}
````

On clean, these are restored to the `ac:rich-text-body` of `excerpt` and the
`ac:plain-text-body` of `html`.

### Styled Span

Confluence storage XML such as `<span style="...">...</span>` and simple inline
HTML elements like `<u>`, `<del>`, `<kbd>`, and `<mark>` are emitted as inline
HTML in Markdown.

```markdown
<span style="color: rgb(255,0,0);">Ishmael</span>
<kbd>Pequod</kbd>
```

On clean, this inline HTML is restored to storage XML. This allows lists that
contain colored words to remain editable as Markdown lists instead of becoming
raw passthrough blocks.

### Blocks Inside List Items

`code`, `noformat`, `expand`, `panel`, `info`, `note`, `tip`, `warning`, `toc`,
`children`, `include`, and `excerpt-include` inside list items are emitted as
Markdown list continuation blocks.

````markdown
- The Pequod sails
  ```bash
  echo "Ahab watches the horizon"
  ```

- Ahab's watch
  <details>
  <summary>The Quarter-Deck</summary>

  The watch is set before dawn.

  </details>
````

On clean, indented blocks are restored inside the `<li>`.

### Transparent Wrappers

`<div>`, `<section>`, `<article>`, and `<main>` elements without attributes are
treated as transparent wrappers in Markdown. Their paragraphs, lists, tables,
and other supported children are expanded. The same transparent handling applies
inside list items.

Confluence `ac:layout` is treated as transparent only when every
`ac:layout-section` has `ac:type="single"`. Multi-column layouts are preserved
as raw passthrough because converting them would change their presentation.

### Unsupported XML Raw Passthrough

Confluence storage XML blocks that cannot be represented by the template syntax
are emitted as fenced blocks.

````markdown
```confluence-storage
<ac:structured-macro ac:name="toc">...</ac:structured-macro>
```
````

On clean, the fenced block body is emitted unchanged as XML. This allows Git
clean/smudge to preserve structured parameters, multi-column layouts, and other
unsupported blocks without destroying them.

## Limitations

- The Markdown parser does not implement all of CommonMark. Complex tables,
  reference links, and complete HTML block conversion are out of scope.
- Block wrappers with attributes become raw passthrough.
- Paragraphs and lists that contain unknown Confluence XML inline elements are
  emitted as raw passthrough for the whole element.
- Block elements and macros inside list items that cannot be represented by the
  template syntax become raw passthrough.
- XML entities are handled for XML standard entities and a subset of common HTML
  named entities. Broken XML with unknown entities becomes raw passthrough.
