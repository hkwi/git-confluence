package pagecache

import (
	"bytes"
	"testing"
)

func TestCacheRestoresOriginalByContentAndPath(t *testing.T) {
	cache := Cache{root: t.TempDir()}
	markdown := []byte("same Markdown\n")
	originals := map[string][]byte{
		"1.md": []byte(`<ul style="list-style-type: square;"><li>one</li></ul>`),
		"2.md": []byte(`<ul style="list-style-type: disc;"><li>two</li></ul>`),
	}
	for path, storage := range originals {
		if err := cache.Remember(markdown, path, storage); err != nil {
			t.Fatal(err)
		}
	}
	for path, want := range originals {
		got, ok, err := cache.Original(markdown, path)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || !bytes.Equal(got, want) {
			t.Fatalf("Original(%q) = %q, %v; want %q", path, got, ok, want)
		}
	}
	if _, ok, err := cache.Original([]byte("edited Markdown\n"), "1.md"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("edited Markdown unexpectedly matched the cache")
	}
}
