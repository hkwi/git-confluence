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
		`level=INFO msg="clean filter started" app=git-confluence path=1.md filter=clean direction=markdown_to_confluence_storage purpose=normalize_for_git bytes=`,
		`level=INFO msg="clean filter completed" app=git-confluence path=1.md filter=clean direction=markdown_to_confluence_storage purpose=normalize_for_git bytes=`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log missing %q:\n%s", want, logs.String())
		}
	}
}

func TestFilterSmudgeReportsPageConversion(t *testing.T) {
	var output bytes.Buffer
	var logs bytes.Buffer
	if err := filterSmudge("1.md", strings.NewReader("<p>吾輩は猫である。</p>\n"), &output, &logs, defaultMaxInputBytes, 32); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`level=INFO msg="smudge filter started" app=git-confluence path=1.md filter=smudge direction=confluence_storage_to_markdown purpose=materialize_worktree bytes=`,
		`level=INFO msg="smudge filter completed" app=git-confluence path=1.md filter=smudge direction=confluence_storage_to_markdown purpose=materialize_worktree bytes=`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log missing %q:\n%s", want, logs.String())
		}
	}
}
