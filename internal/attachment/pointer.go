package attachment

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
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

type pointerYAML struct {
	Version           string `yaml:"version"`
	SourceURL         string `yaml:"source"`
	PageID            string `yaml:"page_id"`
	AttachmentID      string `yaml:"attachment_id"`
	AttachmentVersion int    `yaml:"attachment_version"`
	Filename          string `yaml:"filename"`
	Size              int64  `yaml:"size"`
	MediaType         string `yaml:"media_type,omitempty"`
	DownloadPath      string `yaml:"download_path"`
}

func IsPointer(data []byte) bool {
	var marker struct {
		Version string `yaml:"version"`
	}
	return yaml.Unmarshal(data, &marker) == nil && marker.Version == SpecURL
}

func ParsePointer(data []byte) (Pointer, error) {
	var document pointerYAML
	if err := yaml.Unmarshal(data, &document); err != nil {
		return Pointer{}, fmt.Errorf("parse Confluence attachment pointer YAML: %w", err)
	}
	if document.Version != SpecURL {
		return Pointer{}, fmt.Errorf("unsupported attachment pointer version %q", document.Version)
	}
	pointer := Pointer{
		SourceURL:         document.SourceURL,
		PageID:            document.PageID,
		AttachmentID:      document.AttachmentID,
		AttachmentVersion: document.AttachmentVersion,
		Filename:          document.Filename,
		Size:              document.Size,
		MediaType:         document.MediaType,
		DownloadPath:      document.DownloadPath,
	}
	if err := pointer.validate(); err != nil {
		return Pointer{}, err
	}
	return pointer, nil
}

func (p Pointer) Canonical() []byte {
	data, err := yaml.Marshal(pointerYAML{
		Version:           SpecURL,
		SourceURL:         strings.TrimRight(p.SourceURL, "/"),
		PageID:            p.PageID,
		AttachmentID:      p.AttachmentID,
		AttachmentVersion: p.AttachmentVersion,
		Filename:          p.Filename,
		Size:              p.Size,
		MediaType:         p.MediaType,
		DownloadPath:      p.DownloadPath,
	})
	if err != nil {
		panic(fmt.Sprintf("marshal Confluence attachment pointer: %v", err))
	}
	return data
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
