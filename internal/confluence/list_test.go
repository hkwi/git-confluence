package confluence

import (
	"strings"
	"testing"
)

func TestMarkdownListStopsBeforeInsufficientlyIndentedFollowingLine(t *testing.T) {
	markdown := "- list item\n continuation with one leading space\n"
	storage, err := MarkdownToStorageWithMaxDepth(markdown, DefaultMarkdownRecursionDepth)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<ul>\n<li>list item</li>\n</ul>",
		"<p>continuation with one leading space</p>",
	} {
		if !strings.Contains(storage, want) {
			t.Fatalf("storage missing %q:\n%s", want, storage)
		}
	}
}
