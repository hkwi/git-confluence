package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestFilterCleanReportsPageConversion(t *testing.T) {
	t.Setenv("GIT_CONFLUENCE_CACHE_DIR", t.TempDir())
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
	t.Setenv("GIT_CONFLUENCE_CACHE_DIR", t.TempDir())
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

func TestPageFilterRestoresExactOriginalStorageWhenMarkdownIsUnchanged(t *testing.T) {
	t.Setenv("GIT_CONFLUENCE_CACHE_DIR", t.TempDir())
	storage := []byte(`<ul style="list-style-type: square;"><li>one</li></ul>`)
	var markdown bytes.Buffer
	if err := filterSmudge("1.md", bytes.NewReader(storage), &markdown, &bytes.Buffer{}, defaultMaxInputBytes, 32); err != nil {
		t.Fatal(err)
	}
	var cleaned bytes.Buffer
	if err := filterClean("1.md", bytes.NewReader(markdown.Bytes()), &cleaned, &bytes.Buffer{}, defaultMaxInputBytes, 32); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cleaned.Bytes(), storage) {
		t.Fatalf("clean(smudge(storage)):\n%s\nwant:\n%s", cleaned.Bytes(), storage)
	}

	edited := append(bytes.Clone(markdown.Bytes()), []byte("\nedited\n")...)
	cleaned.Reset()
	if err := filterClean("1.md", bytes.NewReader(edited), &cleaned, &bytes.Buffer{}, defaultMaxInputBytes, 32); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(cleaned.Bytes(), storage) || !bytes.Contains(cleaned.Bytes(), []byte("edited")) {
		t.Fatalf("edited Markdown was restored from cache:\n%s", cleaned.Bytes())
	}
}
