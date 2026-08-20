package attachment

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const SpecURL = "https://github.com/hkwi/git-remote-confluence/spec/attachment/v1"

type Pointer struct {
	SourceURL         string
	PageID            string
	AttachmentID      string
	AttachmentVersion int
	Filename          string
	Size              int64
	MediaType         string
	DownloadPath      string
}

func IsPointer(data []byte) bool {
	first, _, _ := bytes.Cut(data, []byte{'\n'})
	return string(bytes.TrimSuffix(first, []byte{'\r'})) == "version "+SpecURL
}

func ParsePointer(data []byte) (Pointer, error) {
	if !IsPointer(data) {
		return Pointer{}, fmt.Errorf("not a Confluence attachment pointer")
	}
	values := map[string]string{}
	knownFields := map[string]bool{
		"version":            true,
		"source":             true,
		"page-id":            true,
		"attachment-id":      true,
		"attachment-version": true,
		"filename":           true,
		"size":               true,
		"media-type":         true,
		"download-path":      true,
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for index, line := range lines {
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, " ")
		if !ok || value == "" {
			return Pointer{}, fmt.Errorf("invalid pointer line %d", index+1)
		}
		if !knownFields[key] {
			return Pointer{}, fmt.Errorf("unknown pointer field %q", key)
		}
		if _, exists := values[key]; exists {
			return Pointer{}, fmt.Errorf("duplicate pointer field %q", key)
		}
		values[key] = value
	}
	if values["version"] != SpecURL {
		return Pointer{}, fmt.Errorf("unsupported attachment pointer version %q", values["version"])
	}
	version, err := strconv.Atoi(values["attachment-version"])
	if err != nil || version <= 0 {
		return Pointer{}, fmt.Errorf("invalid attachment-version %q", values["attachment-version"])
	}
	size, err := strconv.ParseInt(values["size"], 10, 64)
	if err != nil || size < 0 {
		return Pointer{}, fmt.Errorf("invalid attachment size %q", values["size"])
	}
	pointer := Pointer{
		SourceURL:         values["source"],
		PageID:            values["page-id"],
		AttachmentID:      values["attachment-id"],
		AttachmentVersion: version,
		Filename:          values["filename"],
		Size:              size,
		MediaType:         values["media-type"],
		DownloadPath:      values["download-path"],
	}
	if err := pointer.validate(); err != nil {
		return Pointer{}, err
	}
	return pointer, nil
}

func (p Pointer) Canonical() []byte {
	var out strings.Builder
	fmt.Fprintf(&out, "version %s\n", SpecURL)
	fmt.Fprintf(&out, "source %s\n", strings.TrimRight(p.SourceURL, "/"))
	fmt.Fprintf(&out, "page-id %s\n", p.PageID)
	fmt.Fprintf(&out, "attachment-id %s\n", p.AttachmentID)
	fmt.Fprintf(&out, "attachment-version %d\n", p.AttachmentVersion)
	fmt.Fprintf(&out, "filename %s\n", p.Filename)
	fmt.Fprintf(&out, "size %d\n", p.Size)
	if p.MediaType != "" {
		fmt.Fprintf(&out, "media-type %s\n", p.MediaType)
	}
	fmt.Fprintf(&out, "download-path %s\n", p.DownloadPath)
	return []byte(out.String())
}

func (p Pointer) DownloadURL() (*url.URL, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	base, _ := url.Parse(strings.TrimRight(p.SourceURL, "/") + "/")
	path, _ := url.Parse(p.DownloadPath)
	resolved := base.ResolveReference(path)
	if resolved.Scheme != base.Scheme || resolved.Host != base.Host {
		return nil, fmt.Errorf("attachment download URL points outside Confluence origin")
	}
	query := resolved.Query()
	query.Set("version", strconv.Itoa(p.AttachmentVersion))
	resolved.RawQuery = query.Encode()
	return resolved, nil
}

func PointerOID(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (p Pointer) validate() error {
	base, err := url.Parse(p.SourceURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return fmt.Errorf("invalid Confluence source URL %q", p.SourceURL)
	}
	if p.PageID == "" || p.AttachmentID == "" || p.Filename == "" {
		return fmt.Errorf("attachment pointer is missing identity fields")
	}
	if p.AttachmentVersion <= 0 || p.Size < 0 {
		return fmt.Errorf("attachment pointer has invalid version or size")
	}
	download, err := url.Parse(p.DownloadPath)
	if err != nil || download.IsAbs() || !strings.HasPrefix(download.Path, "/") {
		return fmt.Errorf("invalid attachment download path %q", p.DownloadPath)
	}
	return nil
}
