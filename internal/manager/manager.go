package manager

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	Path       string `json:"path"`
	Source     string `json:"source"`
	SourceKind string `json:"sourceKind,omitempty"`
	Download   string `json:"download,omitempty"`
}

type Extension struct {
	Entry
	Name     string        `json:"name"`
	Version  string        `json:"version"`
	Icon     string        `json:"icon,omitempty"`
	Browsers []string      `json:"browsers"`
	Loads    []BrowserLoad `json:"-"`
}

type manifest struct {
	Name    string
	Version string
	Kind    int `json:"manifest_version"`
	Key     string
	Locale  string `json:"default_locale"`
	Icons   map[string]string
}

type Store struct {
	Dir      string
	Legacy   string
	Browsers []Browser
	Client   *http.Client
	mu       sync.Mutex
}

func New(dir, legacy string, browsers []Browser) *Store {
	return &Store{Dir: dir, Legacy: legacy, Browsers: browsers, Client: &http.Client{Timeout: 45 * time.Second}}
}

func readManifest(folder string) (manifest, error) {
	var m manifest
	file := filepath.Join(folder, "manifest.json")
	f, err := os.Open(file)
	if err != nil {
		return m, fmt.Errorf("could not read manifest.json: %w", err)
	}
	defer f.Close()
	if err := json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&m); err != nil {
		return m, fmt.Errorf("invalid extension manifest: %w", err)
	}
	if strings.TrimSpace(m.Name) == "" || (m.Kind != 2 && m.Kind != 3) {
		return m, fmt.Errorf("folder does not contain a browser extension")
	}
	_, err = version(m.Version)
	return m, err
}

func displayName(m manifest, folder string) string {
	if strings.HasPrefix(m.Name, "__MSG_") && strings.HasSuffix(m.Name, "__") && m.Locale != "" && filepath.Base(m.Locale) == m.Locale && m.Locale != ".." {
		var messages map[string]struct{ Message string }
		key := strings.TrimSuffix(strings.TrimPrefix(m.Name, "__MSG_"), "__")
		if raw, err := os.ReadFile(filepath.Join(folder, "_locales", m.Locale, "messages.json")); err == nil && json.Unmarshal(raw, &messages) == nil {
			for name, value := range messages {
				if strings.EqualFold(key, name) && value.Message != "" {
					return value.Message
				}
			}
		}
		return filepath.Base(folder)
	}
	return m.Name
}

func icon(folder string, m manifest) string {
	for _, size := range []string{"48", "32", "128", "16"} {
		name := m.Icons[size]
		if name == "" || !filepath.IsLocal(name) {
			continue
		}
		f, err := os.Open(filepath.Join(folder, name))
		if err != nil {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(f, 128<<10))
		f.Close()
		if err != nil || len(raw) < 8 {
			continue
		}
		kind := http.DetectContentType(raw)
		if kind == "image/png" || kind == "image/jpeg" || kind == "image/gif" {
			return "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(raw)
		}
	}
	return ""
}

func fileExists(name string) bool { _, err := os.Stat(name); return err == nil }

func pathKey(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

func (s *Store) readEntries() ([]Entry, error) {
	file := filepath.Join(s.Dir, "extensions.json")
	b, err := os.ReadFile(file)
	if os.IsNotExist(err) && s.Legacy != "" {
		b, err = os.ReadFile(filepath.Join(s.Legacy, "extensions.json"))
	}
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	if err = json.Unmarshal(b, &entries); err != nil {
		return nil, err
	}
	if s.Legacy != "" && !fileExists(file) {
		var feeds map[string]struct{ Latest, Download string }
		if b, e := os.ReadFile(filepath.Join(s.Legacy, "ReleaseSources.json")); e == nil && json.Unmarshal(b, &feeds) == nil {
			for i := range entries {
				m, e := readManifest(entries[i].Path)
				feed := feeds[m.Name]
				if e == nil && entries[i].Source == "" && feed.Latest != "" {
					entries[i].Source, entries[i].SourceKind, entries[i].Download = feed.Latest, "manifest", feed.Download
				}
			}
		}
	}
	return entries, nil
}

func (s *Store) writeEntries(entries []Entry) error {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Dir, ".catalog-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(s.Dir, "extensions.json"))
}

func (s *Store) list(only bool) ([]Extension, error) {
	entries, err := s.readEntries()
	if err != nil {
		return nil, err
	}
	loaded := scanBrowsers(s.Browsers)
	seen := map[string]bool{}
	for _, entry := range entries {
		seen[pathKey(entry.Path)] = true
	}
	for _, found := range loaded {
		if !seen[pathKey(found.Path)] {
			entries = append(entries, Entry{Path: found.Path})
			seen[pathKey(found.Path)] = true
		}
	}
	if !only {
		if err := s.writeEntries(entries); err != nil {
			return nil, err
		}
	}
	out := make([]Extension, 0, len(entries))
	for _, entry := range entries {
		m, err := readManifest(entry.Path)
		ext := Extension{Entry: entry, Name: filepath.Base(entry.Path), Browsers: []string{}}
		if err == nil {
			ext.Name, ext.Version, ext.Icon = displayName(m, entry.Path), m.Version, icon(entry.Path, m)
		}
		for _, found := range loaded {
			if pathKey(found.Path) == pathKey(entry.Path) {
				if !slices.Contains(ext.Loads, found.BrowserLoad) {
					ext.Loads = append(ext.Loads, found.BrowserLoad)
				}
				if !slices.Contains(ext.Browsers, found.Browser) {
					ext.Browsers = append(ext.Browsers, found.Browser)
				}
			}
		}
		out = append(out, ext)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Source != "") != (out[j].Source != "") {
			return out[i].Source != ""
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *Store) List(importInstalled bool) ([]Extension, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list(!importInstalled)
}

func (s *Store) find(folder string) (Entry, error) {
	items, err := s.list(true)
	if err != nil {
		return Entry{}, err
	}
	for _, item := range items {
		if pathKey(item.Path) == pathKey(folder) {
			return item.Entry, nil
		}
	}
	return Entry{}, fmt.Errorf("extension is not in the catalog")
}

func (s *Store) Save(folder, source, kind string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.list(true)
	if err != nil {
		return Entry{}, err
	}
	entry := Entry{Path: strings.TrimSpace(folder), Source: strings.TrimSpace(source), SourceKind: kind}
	if entry.Source == "" {
		entry.SourceKind = ""
	}
	if entry.Source != "" && entry.SourceKind != "manifest" && !strings.Contains(entry.Source, "://") {
		if !filepath.IsAbs(entry.Source) {
			return Entry{}, fmt.Errorf("enter an absolute local path or HTTPS URL")
		}
		entry.Source = filepath.Clean(entry.Source)
	}
	if entry.SourceKind == "manifest" || strings.Contains(entry.Source, "://") {
		url, err := httpsURL(entry.Source)
		if err != nil {
			return Entry{}, err
		}
		if strings.HasSuffix(strings.ToLower(url.Path), ".json") {
			entry.SourceKind = "manifest"
		}
	}
	if entry.SourceKind == "manifest" {
		if _, err := s.release(entry); err != nil {
			return Entry{}, err
		}
	}
	if entry.Path == "" {
		if entry.Source == "" {
			return Entry{}, fmt.Errorf("choose an update source or a loaded extension folder")
		}
		var suffix [8]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return Entry{}, err
		}
		entry.Path = filepath.Join(s.Dir, "packages", hex.EncodeToString(suffix[:]))
		if _, err = s.sync(entry); err != nil {
			return Entry{}, err
		}
	} else {
		if !filepath.IsAbs(entry.Path) {
			return Entry{}, fmt.Errorf("choose an absolute extension folder")
		}
		entry.Path = filepath.Clean(entry.Path)
		if _, err = readManifest(entry.Path); err != nil {
			return Entry{}, err
		}
	}
	entries := make([]Entry, 0, len(items)+1)
	for _, item := range items {
		if pathKey(item.Path) != pathKey(entry.Path) {
			entries = append(entries, item.Entry)
		}
	}
	return entry, s.writeEntries(append(entries, entry))
}

func (s *Store) ConfigPath() string { return filepath.Join(s.Dir, "extensions.json") }

func DefaultPaths() (string, string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", "", err
	}
	var legacy string
	if runtime.GOOS == "windows" && os.Getenv("LOCALAPPDATA") != "" {
		legacy = filepath.Join(os.Getenv("LOCALAPPDATA"), "Harness", "ExtensionManager")
	}
	return filepath.Join(root, "BrowserExtensionManager"), legacy, nil
}

func fileDigest(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
