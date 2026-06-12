# git-confluence

`git-confluence` is a Git clean/smudge filter that converts between
Confluence storage XML and Markdown.

It keeps Confluence storage XML as the content stored in Git while exposing the
same files as Markdown in the working tree. This lets Confluence keep the XML
representation it needs while people edit and review pages with normal Markdown
tools.

## Intended Use

This filter is normally used together with `git-remote-confluence`.

`git-remote-confluence` imports and pushes Confluence page trees or spaces as a
Git remote. Page bodies are stored in Git as Confluence storage XML.

`git-confluence` is the filter for those page body files.

1. `git-remote-confluence` fetches page bodies and metadata from Confluence.
2. Git stores each page body as Confluence storage XML.
3. On checkout, the `git-confluence` smudge filter converts XML to Markdown.
4. You edit the Markdown in the working tree and run `git add`.
5. The clean filter converts Markdown back to Confluence storage XML.
6. `git-remote-confluence` pushes the committed XML to existing Confluence page bodies.

This repository does not call the Confluence API by itself. It reads storage XML
or Markdown from standard input, converts it, and writes the result to standard
output.

## Storage And Working Formats

For files covered by this filter, the content stored by Git differs from the
content shown in the working tree.

| Location | Format |
| --- | --- |
| Git blob | Confluence storage XML |
| Working tree after checkout | Markdown |
| Index after `git add` | Confluence storage XML |

This split preserves storage XML for Confluence synchronization while allowing
people to edit Markdown.

## Conversion Model

The converter is a small standard-library-only implementation that handles
common Markdown and Confluence storage XML round trips.

The main supported surface includes headings, paragraphs, emphasis, links,
images, lists, block quotes, code blocks, tables, horizontal rules, selected
Confluence macros, page links, and attachment references.

When a Confluence storage XML block cannot be represented naturally as Markdown,
the converter preserves it as a `confluence-storage` fenced block instead of
rewriting it lossy.

````markdown
```confluence-storage
<ac:structured-macro ac:name="toc">...</ac:structured-macro>
```
````

On clean, the fenced block body is emitted back as XML. See
[docs/conversion-rules.md](docs/conversion-rules.md) for detailed conversion
rules and limitations. A Japanese version is available at
[docs/conversion-rules_ja.md](docs/conversion-rules_ja.md).

## Build

```sh
go build .
```

`go build .` writes a `git-confluence` binary at the repository root. During
development, `go run . ...` also works.

## Filter Configuration

Place this repository wherever you want, then configure the filter in the
repository that contains Confluence pages:

```sh
git config filter.confluence-storage.clean "/path/to/git-confluence/git-confluence clean"
git config filter.confluence-storage.smudge "/path/to/git-confluence/git-confluence smudge"
git config filter.confluence-storage.required true
```

Limit the target files with `.gitattributes`:

```gitattributes
pages/**/*.md filter=confluence-storage diff=markdown
```

Repositories created by `git-remote-confluence` include this imported
`.gitattributes` entry:

```gitattributes
*.md filter=confluence-storage diff=markdown
```

If you want the first checkout during clone to produce Markdown, configure the
filter before checkout. For a per-clone setup, clone without checkout, configure
the filter, then check out:

```sh
CONFLUENCE_PAT=... git clone --no-checkout \
  'confluence::https://confluence.example.com/pages/viewpage.action?pageId=123456789' \
  pages
cd pages
git config filter.confluence-storage.clean "/path/to/git-confluence/git-confluence clean"
git config filter.confluence-storage.smudge "/path/to/git-confluence/git-confluence smudge"
git config filter.confluence-storage.required true
git checkout
```

If the filter is configured globally, a normal `git-remote-confluence` clone can
use it immediately:

```sh
CONFLUENCE_PAT=... git clone \
  'confluence::https://confluence.example.com/pages/viewpage.action?pageId=123456789'
```

## Direct Use

You can also run the converter directly instead of using it as a Git filter:

```sh
go run . smudge < page.xml > page.md
go run . clean < page.md > page.xml
```

`smudge` converts Confluence storage XML to Markdown. `clean` converts Markdown
to Confluence storage XML.

## Input Size And Recursion Depth

The filter reads up to 64 MiB by default. Set a byte limit when converting a
larger expected page:

```sh
GIT_CONFLUENCE_MAX_INPUT_BYTES=134217728 git checkout
```

Rich-text macro bodies and similar nested structures have a recursion depth
limit. The default is `32`. Set a positive integer to stop earlier or allow
deeper valid nesting:

```sh
GIT_CONFLUENCE_MAX_RECURSION_DEPTH=16 git checkout
```

When smudge conversion reaches the depth limit, the affected storage XML is
emitted as a Markdown `confluence-storage` raw block.

## Tests

```sh
go test ./...
```
