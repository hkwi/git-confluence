package attachment

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPointerCanonicalRoundTrip(t *testing.T) {
	pointer := Pointer{
		SourceURL:         "https://cf.example.test/wiki",
		PageID:            "123",
		AttachmentID:      "456",
		AttachmentVersion: 7,
		Filename:          "diagram one.png",
		Size:              42,
		MediaType:         "image/png",
		DownloadPath:      "/wiki/download/attachments/123/diagram%20one.png?api=v2",
	}
	data := pointer.Canonical()
	if !bytes.HasPrefix(data, []byte("version: "+SpecURL+"\n")) {
		t.Fatalf("canonical pointer is not YAML:\n%s", data)
	}
	parsed, err := ParsePointer(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != pointer {
		t.Fatalf("parsed pointer = %#v, want %#v", parsed, pointer)
	}
	downloadURL, err := parsed.DownloadURL()
	if err != nil {
		t.Fatal(err)
	}
	if got := downloadURL.String(); got != "https://cf.example.test/wiki/download/attachments/123/diagram%20one.png?api=v2&version=7" {
		t.Fatalf("download URL = %q", got)
	}
}

func TestYAMLPointerAllowsReorderedAndQuotedFields(t *testing.T) {
	data := []byte(`source: https://cf.example.test
version: "https://github.com/hkwi/git-remote-confluence/spec/attachment/v1"
attachment_id: "456"
page_id: "123"
filename: diagram.png
attachment_version: 7
download_path: /download/attachments/123/diagram.png
size: 42
`)
	if !IsPointer(data) {
		t.Fatal("reordered YAML was not recognized as a pointer")
	}
	if _, err := ParsePointer(data); err != nil {
		t.Fatal(err)
	}
}

func TestSmudgeDownloadsCachesAndCleanRestoresPointer(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/download/attachments/123/file.bin" || r.URL.Query().Get("version") != "2" {
			t.Errorf("request URL = %s", r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte("attachment data"))
	}))
	defer server.Close()

	t.Setenv(cacheDirEnv, t.TempDir())
	t.Setenv("CONFLUENCE_PAT", "secret")
	pointer := Pointer{
		SourceURL:         server.URL,
		PageID:            "123",
		AttachmentID:      "456",
		AttachmentVersion: 2,
		Filename:          "file.bin",
		Size:              int64(len("attachment data")),
		MediaType:         "application/octet-stream",
		DownloadPath:      "/download/attachments/123/file.bin",
	}.Canonical()

	for attempt := 0; attempt < 2; attempt++ {
		var output bytes.Buffer
		var logs bytes.Buffer
		if err := Smudge(pointer, "123/attachments/file.bin", &output, &logs); err != nil {
			t.Fatal(err)
		}
		if output.String() != "attachment data" {
			t.Fatalf("smudge output = %q", output.String())
		}
		if !strings.Contains(logs.String(), `level=INFO msg="materializing attachment" app=git-confluence path=123/attachments/file.bin attachment_id=456 attachment_version=2`) {
			t.Fatalf("materialization log missing:\n%s", logs.String())
		}
		if attempt == 0 && !strings.Contains(logs.String(), `level=INFO msg="downloaded attachment" app=git-confluence path=123/attachments/file.bin attachment_id=456 attachment_version=2 filter=smudge direction=attachment_pointer_to_bytes purpose=materialize_worktree bytes=15`) {
			t.Fatalf("download log missing:\n%s", logs.String())
		}
		if attempt == 1 && !strings.Contains(logs.String(), `level=INFO msg="using cached attachment" app=git-confluence path=123/attachments/file.bin attachment_id=456 attachment_version=2`) {
			t.Fatalf("cache log missing:\n%s", logs.String())
		}
	}
	t.Setenv("CONFLUENCE_PAT", "")
	t.Setenv("GIT_REMOTE_CONFLUENCE_PAT", "")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	var cachedOutput bytes.Buffer
	if err := Smudge(pointer, "123/attachments/file.bin", &cachedOutput, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if cachedOutput.String() != "attachment data" {
		t.Fatalf("cached smudge output = %q", cachedOutput.String())
	}
	if requests.Load() != 1 {
		t.Fatalf("download requests = %d, want 1", requests.Load())
	}

	var cleaned bytes.Buffer
	if err := Clean(strings.NewReader("attachment data"), "123/attachments/file.bin", &cleaned); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cleaned.Bytes(), pointer) {
		t.Fatalf("cleaned pointer:\n%s\nwant:\n%s", cleaned.Bytes(), pointer)
	}
}

func TestSmudgeWithoutPATLeavesPointer(t *testing.T) {
	t.Setenv(cacheDirEnv, t.TempDir())
	t.Setenv("CONFLUENCE_PAT", "")
	t.Setenv("GIT_REMOTE_CONFLUENCE_PAT", "")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	pointer := Pointer{
		SourceURL:         "https://cf.example.test",
		PageID:            "1",
		AttachmentID:      "2",
		AttachmentVersion: 3,
		Filename:          "file.bin",
		DownloadPath:      "/download/file.bin",
	}.Canonical()
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	if err := Smudge(pointer, "1/attachments/file.bin", &output, &errorOutput); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), pointer) {
		t.Fatalf("smudge output:\n%s", output.Bytes())
	}
	if !strings.Contains(errorOutput.String(), "leaving attachment pointer") {
		t.Fatalf("stderr = %q", errorOutput.String())
	}
}

func TestSmudgeSizeMismatchWarnsAndUsesDownloadedContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("wrong size"))
	}))
	defer server.Close()

	t.Setenv(cacheDirEnv, t.TempDir())
	t.Setenv("CONFLUENCE_PAT", "secret")
	pointer := Pointer{
		SourceURL:         server.URL,
		PageID:            "1",
		AttachmentID:      "2",
		AttachmentVersion: 3,
		Filename:          "file.bin",
		Size:              42,
		DownloadPath:      "/download/file.bin",
	}.Canonical()
	const worktreePath = "1/attachments/file.bin"
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	if err := Smudge(pointer, worktreePath, &output, &errorOutput); err != nil {
		t.Fatal(err)
	}
	if output.String() != "wrong size" {
		t.Fatalf("smudge output = %q", output.String())
	}
	wantWarning := `level=WARN msg="attachment size differs from pointer; using downloaded content" app=git-confluence path=` + worktreePath + ` attachment_id=2 attachment_version=3 filter=smudge direction=attachment_pointer_to_bytes purpose=materialize_worktree pointer_size=42 downloaded_size=10`
	if !strings.Contains(errorOutput.String(), wantWarning) {
		t.Fatalf("stderr = %q", errorOutput.String())
	}
}

func TestCleanRejectsUnknownAttachment(t *testing.T) {
	t.Setenv(cacheDirEnv, t.TempDir())
	err := Clean(strings.NewReader("locally modified data"), "1/attachments/file.bin", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("Clean error = %v", err)
	}
}

func TestCleanDistinguishesSameContentAtDifferentPaths(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("shared bytes"))
	}))
	defer server.Close()

	t.Setenv(cacheDirEnv, t.TempDir())
	t.Setenv("CONFLUENCE_PAT", "secret")
	pointers := []Pointer{
		{SourceURL: server.URL, PageID: "1", AttachmentID: "11", AttachmentVersion: 1, Filename: "a.bin", Size: 12, DownloadPath: "/a.bin"},
		{SourceURL: server.URL, PageID: "2", AttachmentID: "22", AttachmentVersion: 1, Filename: "b.bin", Size: 12, DownloadPath: "/b.bin"},
	}
	paths := []string{"1/attachments/a.bin", "2/attachments/b.bin"}
	for index, pointer := range pointers {
		var output bytes.Buffer
		if err := Smudge(pointer.Canonical(), paths[index], &output, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
	}
	for index, pointer := range pointers {
		var cleaned bytes.Buffer
		if err := Clean(strings.NewReader("shared bytes"), paths[index], &cleaned); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(cleaned.Bytes(), pointer.Canonical()) {
			t.Fatalf("path %s cleaned to:\n%s\nwant:\n%s", paths[index], cleaned.Bytes(), pointer.Canonical())
		}
	}
	if requests.Load() != 2 {
		t.Fatalf("download requests = %d, want 2", requests.Load())
	}
}
