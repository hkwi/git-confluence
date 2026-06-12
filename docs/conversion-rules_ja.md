# Confluence storage XML と Markdown の変換規則

English: [conversion-rules.md](conversion-rules.md)

この filter では Git に保存する形式を Confluence storage XML、作業ツリーで編集する形式を Markdown とします。

## 基本規則

| Markdown | Confluence storage XML |
| --- | --- |
| `#` から `######` | `<h1>` から `<h6>` |
| 段落 | `<p>` |
| `**bold**` | `<strong>` |
| `*italic*` | `<em>` |
| `` `code` `` | `<code>` |
| `[label](https://example.com)` | `<a href="https://example.com">label</a>` |
| `![alt](https://example.com/a.png)` | `<ac:image><ri:url ... /></ac:image>` |
| `- item` / `1. item` | `<ul>` / `<ol>` |
| nested list | nested `<ul>` / `<ol>` |
| fenced code block | Confluence の `code` structured macro |
| Markdown pipe table | `<table>` |
| `---` | `<hr />` |

## Confluence 固有の特殊ルール

### Confluence ページリンク

Confluence のページリンクは Markdown では `confluence://page/<title>` として表します。

```markdown
[吾輩](confluence://page/%E5%90%BE%E8%BC%A9)
[名前はまだ無い](confluence://page/%E5%90%8D%E5%89%8D%E3%81%AF%E3%81%BE%E3%81%A0%E7%84%A1%E3%81%84?space=NEKO)
```

clean 時には `<ac:link><ri:page ri:content-title="吾輩" />...` に変換します。`space` query がある場合は `ri:space-key` に戻します。

### Confluence 添付ファイル

添付ファイル参照は `confluence://attachment/<filename>` として表します。

```markdown
![猫の絵端書](confluence://attachment/neko-ehagaki.png)
```

clean 時には `<ac:image><ri:attachment ri:filename="neko-ehagaki.png" /></ac:image>` に変換します。

### Code macro

Markdown の fenced code block は Confluence の `code` structured macro に変換します。言語名は `ac:parameter ac:name="language"` に入ります。`title` と `linenumbers` は fence info の key/value として保持します。

````markdown
```python title=script.py linenumbers=true
print("吾輩は猫である。名前はまだ無い。")
```
````

`theme` や `collapse` など、その他の key/value も `code` macro parameter として保持します。

### Noformat macro

Confluence の `noformat` macro は Markdown では専用の fenced block として出力します。

````markdown
```confluence-noformat
どこで生れたかとんと見当がつかぬ
```
````

clean 時には `ac:name="noformat"` macro に戻します。

### Status macro

Confluence の `status` macro は Markdown では inline の短い記法として出力します。

```markdown
{{status:green|猫}}
```

clean 時には `ac:name="status"` macro に戻します。色がない場合は `{{status|猫}}` として扱います。

### Expand macro

Confluence の `expand` macro は Markdown では HTML の `<details>` として出力します。

```markdown
<details>
<summary>名前はまだ無い</summary>

どこで生れたかとんと見当がつかぬ。

</details>
```

clean 時には `ac:name="expand"` macro に戻します。

### Info / Note / Tip / Warning / Panel macro

Confluence の `info`、`note`、`tip`、`warning`、`panel` macro は Markdown では GitHub-style admonition に近い引用として出力します。

```markdown
> [!INFO] 吾輩は猫である
> 名前はまだ無い。

> [!PANEL] 教師というものは実に楽なものだ
> 猫にでも出来ぬ事はない。
```

clean 時には対応する Confluence macro に戻します。

### TOC / Children / Include / Excerpt Include macro

Confluence 側で動的に解決される参照系 macro は、Markdown では単独行の短い記法として出力します。

```markdown
{{toc maxLevel=3 outline=true}}
{{children depth=5}}
{{children page="名前はまだ無い" depth=2}}
{{include page="吾輩は猫である"}}
{{excerpt-include page="教師というものは実に楽なものだ" nopanel=true}}
```

clean 時には対応する Confluence structured macro に戻します。`include` と `excerpt-include` の `page` は、Confluence storage XML では `ac:parameter ac:name=""` 内の `ri:page ri:content-title` として表します。別 space のページを参照する場合は `space=SPACEKEY` を併記できます。

### 汎用 structured macro template

単純な `ac:parameter`、`ac:rich-text-body`、macro 属性だけで表せる Confluence structured macro は、Markdown では `{{...}}` template として出力できます。

```markdown
{{jira key=PROJ-1}}

{{info macro-id=90a06dbb-7d7a-4b92-8492-20fa2a5bde12 schema-version=1 icon=false}}

名前はまだ無い。

{{status macro-id=9bbb1a94-622f-4222-aab3-c09c2029be73 schema-version=1 colour=Green title="猫"}}

{{/info}}
```

clean 時には、開始 token の名前を `ac:name` に、本文を `ac:rich-text-body` に戻します。属性と parameter は次の規則で振り分けます。

- `macro-id` と `ac:macro-id` は `ac:macro-id` 属性に戻します。
- `schema-version` と `ac:schema-version` は `ac:schema-version` 属性に戻します。
- `attr:name`、`attr:ac:name`、`attr:ri:name` は、それぞれ無 prefix、`ac:`、`ri:` の XML 属性に戻します。
- それ以外の `key=value` は `<ac:parameter ac:name="key">value</ac:parameter>` に戻します。

`code`、`noformat`、`html` macro の fenced block info string でも同じ属性指定を使えます。

````markdown
```bash macro-id=554d7df7-f9b2-4fa4-9e0b-e8007ba29890 schema-version=1
echo "名前はまだ無い"
```
````

Confluence storage XML の `CDATA` は Markdown では通常の fenced block 本文として出力します。clean 時には `ac:plain-text-body` の文字データとして戻すため、`CDATA` という XML 上の書き方自体は保持しません。

### Excerpt / HTML macro

Confluence の `excerpt` macro は body を持つ round-trip container として出力します。`html` macro は専用の fenced block として出力します。

````markdown
{{excerpt atlassian-macro-output-type=INLINE}}

```confluence-html
<script>
console.log("吾輩は猫である")
</script>
```

{{/excerpt}}
````

clean 時には `excerpt` の `ac:rich-text-body` と `html` の `ac:plain-text-body` に戻します。

### Styled span

Confluence storage XML の `<span style="...">...</span>` や、`<u>`、`<del>`、`<kbd>`、`<mark>` などの単純な inline HTML は、Markdown でも inline HTML として出力します。

```markdown
<span style="color: rgb(255,0,0);">吾輩</span>
<kbd>名前</kbd>
```

clean 時にはこの inline HTML を storage XML に戻します。これにより、色付きの語句を含むリスト全体を raw passthrough にせず Markdown のリストとして編集できます。

### リスト項目内のブロック

リスト項目の中にある `code`、`noformat`、`expand`、`panel`、`info`、`note`、`tip`、`warning`、`toc`、`children`、`include`、`excerpt-include` は、Markdown ではリスト continuation block として出力します。

````markdown
- 吾輩は猫である
  ```bash
  echo "名前はまだ無い"
  ```

- どこで生れたか
  <details>
  <summary>名前はまだ無い</summary>

  何でも薄暗いじめじめした所でニャーニャー泣いていた。

  </details>
````

clean 時にはインデントされた block を `<li>` の内側に戻します。

### Transparent wrappers

属性を持たない `<div>`、`<section>`、`<article>`、`<main>` は Markdown では透過的に扱い、中の段落、リスト、表などを展開します。リスト項目内の同様の wrapper も展開します。

Confluence の `ac:layout` は、すべての `ac:layout-section` が `ac:type="single"` の場合だけ透明に扱います。複数カラム layout は表示上の意味が変わるため raw passthrough のまま保持します。

### 未対応 XML の raw passthrough

template 記法でも表現できない Confluence storage XML ブロックは、次の fenced block として Markdown に出力します。

````markdown
```confluence-storage
<ac:structured-macro ac:name="toc">...</ac:structured-macro>
```
````

clean 時には fenced block の中身をそのまま XML として出力します。これにより、構造化 parameter や複数カラム layout などを壊さずに Git の clean/smudge を通せます。

## 制約

- Markdown parser は CommonMark 全体を実装していません。複雑な表、参照リンク、HTML block の完全変換は対象外です。
- 属性付き block wrapper は raw passthrough になります。
- Confluence XML の未知の inline 要素を含む段落やリストは、要素全体を raw passthrough として出力します。
- リスト項目内の template 記法で表現できない block 要素や macro は raw passthrough になります。
- XML entity は XML 標準 entity と一般的な HTML named entity の一部を処理します。未知 entity を含む壊れた XML は raw passthrough になります。
