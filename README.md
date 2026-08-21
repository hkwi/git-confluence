# git-confluence

`git-confluence` is a unified Git filter for Confluence pages and attachments.
It converts page bodies between Confluence storage XML and Markdown and
materializes attachment pointer files through the Confluence download API.

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

Attachment pointers remain small YAML files when the filter is not configured,
the PAT is unavailable, or smudge is explicitly skipped. With the filter
configured, checkout downloads attachment bytes into a local Git-specific cache
and exposes them at their normal paths. Attachment changes are read-only until
upload support is implemented.

## Storage And Working Formats

For files covered by this filter, the content stored by Git differs from the
content shown in the working tree.

| Location | Format |
| --- | --- |
| Git blob | Confluence storage XML |
| Working tree after checkout | Markdown |
| Index after `git add` | Confluence storage XML |

For attachments:

| Location | Format |
| --- | --- |
| Git blob | Confluence attachment pointer |
| Working tree after checkout | Attachment bytes, or pointer when unavailable/skipped |
| Index after `git add` | Original attachment pointer |

Attachment pointers use the `attachment/v1` YAML schema.

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

## Install

Install the tagged release with Go:

```sh
go install github.com/hkwi/git-confluence@v0.1.0
```

Prebuilt archives for Linux, macOS, and Windows are published on the GitHub
Releases page. Each release includes `checksums.txt`.

Check the installed binary:

```sh
git-confluence version
```

## Filter Configuration

Configure the unified page and attachment filter once for the current user:

```sh
git confluence install --global
```

`install` registers the `filter.confluence` clean/smudge commands in Git
configuration; it does not install the `git-confluence` executable or download
attachments.

Use `git confluence install --local` after a `--no-checkout` clone when the
configuration should apply to only one repository.

Limit the target files with `.gitattributes`:

```gitattributes
*.md filter=confluence diff=markdown
**/attachments/** filter=confluence -text
```

Repositories created by `git-remote-confluence` include this imported
`.gitattributes` entry:

```gitattributes
*.md filter=confluence diff=markdown
**/attachments/** filter=confluence -text
```

If you want the first checkout during clone to produce Markdown, configure the
filter before checkout. For a per-clone setup, clone without checkout, configure
the filter, then check out:

```sh
CONFLUENCE_PAT=... git clone --no-checkout \
  'confluence::https://confluence.example.com/pages/viewpage.action?pageId=123456789' \
  pages
cd pages
git confluence install --local
git checkout
```

If the filter is configured globally, a normal `git-remote-confluence` clone can
use it immediately:

```sh
CONFLUENCE_PAT=... git clone \
  'confluence::https://confluence.example.com/pages/viewpage.action?pageId=123456789'
```

Keep attachment pointers while still converting page XML to Markdown:

```sh
GIT_CONFLUENCE_SKIP_SMUDGE=1 git checkout
```

Attachment checkout progress is written to stderr as logfmt. Each record names
the working-tree path, attachment ID, and version, and reports cache hits and
download progress without exposing the Confluence PAT:

```text
level=INFO msg="downloading attachment" app=git-confluence path=123/attachments/diagram.png attachment_id=456 attachment_version=2 filter=smudge direction=attachment_pointer_to_bytes purpose=materialize_worktree
level=INFO msg="downloaded attachment" app=git-confluence path=123/attachments/diagram.png attachment_id=456 attachment_version=2 filter=smudge direction=attachment_pointer_to_bytes purpose=materialize_worktree bytes=42000
```

Page clean and smudge conversion also reports start and completion records,
including the path, filter, direction, purpose, and input or output byte count.
The clean filter normalizes worktree content for Git comparison or storage;
the smudge filter materializes Git content in the worktree. This makes a slow
page conversion distinguishable from an attachment download.

Materialize all attachments, or selected paths, after checkout:

```sh
git confluence pull
git confluence pull 123456789/attachments/diagram.png
```

`pull` enables the filter locally, refuses to overwrite attachment paths with
local changes, and reports an error if an attachment could not be materialized.

## Direct Use

You can also run the converter directly instead of using it as a Git filter:

```sh
go run . smudge < page.xml > page.md
go run . clean < page.md > page.xml
```

`smudge` converts Confluence storage XML to Markdown. `clean` converts Markdown
to Confluence storage XML.

## Attachment Authentication And Cache

Attachment download uses the first PAT found in `CONFLUENCE_PAT`,
`GIT_REMOTE_CONFLUENCE_PAT`, `confluence.pat`, or
`remote.confluence.pat`. Download URLs are constrained to the Confluence origin
recorded by the pointer.

Downloaded objects and clean-filter reverse mappings are stored below
`.git/confluence`. The cache avoids downloading the same pointer again and lets
the clean filter restore an unchanged attachment to its canonical pointer.
Unknown attachment bytes are rejected as read-only rather than accidentally
being committed as large Git blobs.

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
