package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestFilterCleanReportsPageConversion(t *testing.T) {
	var output bytes.Buffer
	var logs bytes.Buffer
	if err := filterClean("1.md", strings.NewReader("吾輩は猫である。\n"), &output, &logs, defaultMaxInputBytes, 32); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`level=INFO msg="converting Markdown to Confluence storage" app=git-confluence path=1.md bytes=`,
		`level=INFO msg="converted Markdown to Confluence storage" app=git-confluence path=1.md bytes=`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log missing %q:\n%s", want, logs.String())
		}
	}
}
