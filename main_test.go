package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hkwi/git-confluence/internal/attachment"
)

func TestReadLimitedInputAcceptsInputAtLimit(t *testing.T) {
	input, err := readLimitedInput(strings.NewReader("abcd"), 4)
	if err != nil {
		t.Fatalf("readLimitedInput returned error: %v", err)
	}
	if string(input) != "abcd" {
		t.Fatalf("unexpected input: %q", input)
	}
}

func TestReadLimitedInputRejectsInputOverLimit(t *testing.T) {
	_, err := readLimitedInput(strings.NewReader("abcde"), 4)
	if err == nil {
		t.Fatal("readLimitedInput returned nil error")
	}
	if !strings.Contains(err.Error(), maxInputBytesEnv) {
		t.Fatalf("error should mention override env var: %v", err)
	}
}

func TestMaxInputBytesFromEnv(t *testing.T) {
	t.Setenv(maxInputBytesEnv, "123")

	maxInput, err := maxInputBytes()
	if err != nil {
		t.Fatalf("maxInputBytes returned error: %v", err)
	}
	if maxInput != 123 {
		t.Fatalf("unexpected max input: %d", maxInput)
	}
}

func TestMaxInputBytesRejectsInvalidEnv(t *testing.T) {
	t.Setenv(maxInputBytesEnv, "0")

	_, err := maxInputBytes()
	if err == nil {
		t.Fatal("maxInputBytes returned nil error")
	}
}

func TestVersionOutput(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, date
	version, commit, date = "v0.1.0", "abc1234", "2026-06-12T00:00:00Z"
	t.Cleanup(func() {
		version, commit, date = oldVersion, oldCommit, oldDate
	})

	got := versionOutput()
	want := "git-confluence v0.1.0\ncommit: abc1234\nbuilt: 2026-06-12T00:00:00Z\n"
	if got != want {
		t.Fatalf("versionOutput() = %q, want %q", got, want)
	}
}

func TestHelpOutput(t *testing.T) {
	got := helpOutput()
	want := "usage: git-confluence clean|smudge|filter-clean <path>|filter-smudge <path>|install [--global|--local]|pull [path...]|version|help\n"
	if got != want {
		t.Fatalf("helpOutput() = %q, want %q", got, want)
	}
}

func TestUnifiedFilterMaterializesAttachmentAndCleansBackToPointer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/download/file.bin" || r.URL.Query().Get("version") != "3" {
			t.Errorf("request URL = %s", r.URL.String())
		}
		_, _ = w.Write([]byte("attachment bytes"))
	}))
	defer server.Close()

	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	filterPath := filepath.Join(binDir, "git-confluence")
	build := exec.Command("go", "build", "-o", filterPath, ".")
	build.Env = append(os.Environ(), "GOCACHE="+filepath.Join(tmp, "gocache"))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}
	if err := os.MkdirAll(filepath.Join(repo, "1", "attachments"), 0o755); err != nil {
		t.Fatal(err)
	}
	pointer := attachment.Pointer{
		SourceURL:         server.URL,
		PageID:            "1",
		AttachmentID:      "2",
		AttachmentVersion: 3,
		Filename:          "file.bin",
		Size:              int64(len("attachment bytes")),
		DownloadPath:      "/download/file.bin",
	}.Canonical()
	attachmentPath := filepath.Join(repo, "1", "attachments", "file.bin")
	if err := os.WriteFile(attachmentPath, pointer, 0o644); err != nil {
		t.Fatal(err)
	}
	pageStorage := []byte(`<ul style="list-style-type: square;"><li>one</li></ul>`)
	pagePath := filepath.Join(repo, "1.md")
	if err := os.WriteFile(pagePath, pageStorage, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.md filter=confluence diff=markdown\n**/attachments/** filter=confluence -text\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"CONFLUENCE_PAT=secret",
		"GIT_CONFLUENCE_CACHE_DIR="+filepath.Join(tmp, "cache"),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	)
	runGit(t, repo, env, "init")
	runGit(t, repo, env, "config", "user.name", "Test")
	runGit(t, repo, env, "config", "user.email", "test@example.invalid")
	runGit(t, repo, env, "add", ".")
	runGit(t, repo, env, "commit", "-m", "pointer and page storage")

	install := exec.Command(filterPath, "install", "--local")
	install.Dir = repo
	install.Env = env
	if output, err := install.CombinedOutput(); err != nil {
		t.Fatalf("install filter: %v\n%s", err, output)
	}
	if status := runGitStdout(t, repo, env, "status", "--short"); status != "" {
		t.Fatalf("install after checkout marked indexed storage modified: %q", status)
	}
	if err := os.Remove(pagePath); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, env, "checkout", "HEAD", "--", "1.md")
	markdown, err := os.ReadFile(pagePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(markdown, pageStorage) || !bytes.Contains(markdown, []byte("- one")) {
		t.Fatalf("smudged page = %q", markdown)
	}
	if status := runGitStdout(t, repo, env, "status", "--short"); status != "" {
		t.Fatalf("worktree status after page smudge = %q", status)
	}

	if err := os.Remove(attachmentPath); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, env, "checkout", "HEAD", "--", "1/attachments/file.bin")
	materialized, err := os.ReadFile(attachmentPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(materialized) != "attachment bytes" {
		t.Fatalf("materialized attachment = %q", materialized)
	}
	if status := runGitStdout(t, repo, env, "status", "--short"); status != "" {
		attr := runGit(t, repo, env, "check-attr", "filter", "--", "1/attachments/file.bin")
		cleanConfig := runGit(t, repo, env, "config", "--get", "filter.confluence.clean")
		indexOID := runGit(t, repo, env, "rev-parse", ":1/attachments/file.bin")
		cleanOID := runGit(t, repo, env, "hash-object", "--path=1/attachments/file.bin", "1/attachments/file.bin")
		t.Fatalf("worktree status after smudge = %q\nattr: %s\nclean: %s\nindex: %s\ncleaned: %s", status, attr, cleanConfig, indexOID, cleanOID)
	}
	if err := os.Remove(attachmentPath); err != nil {
		t.Fatal(err)
	}
	skipEnv := append(append([]string{}, env...), "GIT_CONFLUENCE_SKIP_SMUDGE=1")
	runGit(t, repo, skipEnv, "checkout", "HEAD", "--", "1/attachments/file.bin")
	pointerData, err := os.ReadFile(attachmentPath)
	if err != nil {
		t.Fatal(err)
	}
	if !attachment.IsPointer(pointerData) {
		t.Fatalf("skip-smudge checkout = %q, want pointer", pointerData)
	}
	pull := exec.Command(filterPath, "pull", "1/attachments/file.bin")
	pull.Dir = repo
	pull.Env = env
	if output, err := pull.CombinedOutput(); err != nil {
		t.Fatalf("pull attachment: %v\n%s", err, output)
	}
	materialized, err = os.ReadFile(attachmentPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(materialized) != "attachment bytes" {
		t.Fatalf("pulled attachment = %q", materialized)
	}
	if err := os.WriteFile(attachmentPath, []byte("locally changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	add := exec.Command("git", "add", "1/attachments/file.bin")
	add.Dir = repo
	add.Env = env
	output, err := add.CombinedOutput()
	if err == nil || !bytes.Contains(output, []byte("read-only")) {
		t.Fatalf("git add modified attachment error = %v\n%s", err, output)
	}
}

func runGit(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func runGitStdout(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}
