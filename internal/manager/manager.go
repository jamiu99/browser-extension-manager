package manager

import (
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
	Removed    bool   `json:"removed,omitempty"`
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
	Browsers func() []Browser
	Client   *http.Client
	mu       sync.Mutex
}

func New(dir, legacy string, browsers func() []Browser) *Store {
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
	return writeJSON(filepath.Join(s.Dir, "extensions.json"), entries)
}

func writeJSON(path string, value any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".settings-")
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
	return os.Rename(f.Name(), path)
}

func (s *Store) catalog() ([]Entry, []loadedExtension, error) {
	entries, err := s.readEntries()
	if err != nil {
		return nil, nil, err
	}
	var loaded []loadedExtension
	if s.Browsers != nil {
		loaded = scanBrowsers(s.Browsers())
	}
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
	return entries, loaded, nil
}

func (s *Store) list(only bool) ([]Extension, error) {
	entries, loaded, err := s.catalog()
	if err != nil {
		return nil, err
	}
	if !only {
		if err := s.writeEntries(entries); err != nil {
			return nil, err
		}
	}
	out := make([]Extension, 0, len(entries))
	for _, entry := range entries {
		if entry.Removed {
			continue
		}
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
	return s.Edit("", folder, source, kind)
}

func (s *Store) Edit(original, folder, source, kind string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, _, err := s.catalog()
	if err != nil {
		return Entry{}, err
	}
	var previous Entry
	if original != "" {
		for _, candidate := range entries {
			if !candidate.Removed && pathKey(candidate.Path) == pathKey(original) {
				previous = candidate
				break
			}
		}
		if previous.Path == "" {
			return Entry{}, fmt.Errorf("插件已移除，请刷新列表")
		}
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
	if previous.Path != "" && entry.SourceKind == previous.SourceKind && entry.Source == previous.Source {
		entry.Download = previous.Download
	}
	if entry.SourceKind == "manifest" && (previous.Path == "" || entry.Source != previous.Source || entry.SourceKind != previous.SourceKind) {
		if _, err := s.release(entry); err != nil {
			return Entry{}, err
		}
	}
	created := ""
	saved := false
	defer func() {
		if !saved && created != "" {
			os.RemoveAll(created)
		}
	}()
	switch {
	case entry.Path == "" && original != "":
		return Entry{}, fmt.Errorf("请输入新的插件目录")
	case entry.Path == "":
		if entry.Source == "" {
			return Entry{}, fmt.Errorf("请选择插件目录或填写更新来源")
		}
		entry.Path, err = s.installNew(entry)
		if err != nil {
			return Entry{}, err
		}
		created = entry.Path
	default:
		if !filepath.IsAbs(entry.Path) {
			return Entry{}, fmt.Errorf("请输入插件目录的绝对路径")
		}
		entry.Path = filepath.Clean(entry.Path)
		if original != "" && pathKey(original) != pathKey(entry.Path) {
			for _, candidate := range entries {
				if !candidate.Removed && pathKey(candidate.Path) == pathKey(entry.Path) {
					old, oldErr := readManifest(previous.Path)
					current, currentErr := readManifest(candidate.Path)
					if oldErr != nil || currentErr != nil || old.Name != current.Name || old.Key != current.Key ||
						(candidate.Source != "" && candidate.Source != entry.Source) {
						return Entry{}, fmt.Errorf("新目录已被另一条插件记录使用，请先检查该记录")
					}
					break
				}
			}
			created, err = copyToNewPath(previous.Path, entry.Path)
			if err != nil {
				return Entry{}, err
			}
		} else if _, err = readManifest(entry.Path); err != nil {
			return Entry{}, err
		}
	}

	result := make([]Entry, 0, len(entries)+1)
	for _, candidate := range entries {
		switch {
		case pathKey(candidate.Path) == pathKey(entry.Path):
			continue
		case original != "" && pathKey(candidate.Path) == pathKey(original):
			result = append(result, Entry{Path: candidate.Path, Removed: true})
		default:
			result = append(result, candidate)
		}
	}
	err = s.writeEntries(append(result, entry))
	saved = err == nil
	return entry, err
}

func (s *Store) Remove(folder string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, _, err := s.catalog()
	if err != nil {
		return err
	}
	for i, candidate := range entries {
		if !candidate.Removed && pathKey(candidate.Path) == pathKey(folder) {
			entries[i] = Entry{Path: candidate.Path, Removed: true}
			return s.writeEntries(entries)
		}
	}
	return nil
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
