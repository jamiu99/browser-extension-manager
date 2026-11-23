package manager

import (
	"archive/zip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const packageLimit = 128 << 20

var zipName = regexp.MustCompile(`(?i)^[a-z0-9_-][a-z0-9_.-]*\.zip$`)

type release struct {
	Version  string `json:"version"`
	File     string `json:"file"`
	Download string `json:"download_url"`
	SHA256   string `json:"sha256"`
}

type Result struct {
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
	State   string `json:"state"`
	Error   string `json:"error,omitempty"`
}

func version(value string) ([4]int, error) {
	var out [4]int
	parts := strings.Split(value, ".")
	if len(parts) > 4 {
		return out, fmt.Errorf("invalid extension version: %q", value)
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil || strconv.FormatUint(n, 10) != p {
			return out, fmt.Errorf("invalid extension version: %q", value)
		}
		out[i] = int(n)
	}
	if out == [4]int{} {
		return out, fmt.Errorf("extension version must be greater than zero")
	}
	return out, nil
}

func compare(left, right string) int {
	x, _ := version(left)
	y, _ := version(right)
	for i := range x {
		if x[i] < y[i] {
			return -1
		}
		if x[i] > y[i] {
			return 1
		}
	}
	return 0
}

func httpsURL(address string) (*url.URL, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, fmt.Errorf("enter an HTTPS address without credentials or a fragment")
	}
	return u, nil
}

func (s *Store) get(address string) (*http.Response, error) {
	if _, err := httpsURL(address); err != nil {
		return nil, err
	}
	client := *s.Client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil {
			return fmt.Errorf("invalid download redirect")
		}
		return nil
	}
	response, err := client.Get(address)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("server returned HTTP %d", response.StatusCode)
	}
	return response, nil
}

func (s *Store) release(entry Entry) (release, error) {
	var result release
	base, err := httpsURL(entry.Source)
	if err != nil {
		return result, err
	}
	response, err := s.get(entry.Source)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	if err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return result, fmt.Errorf("invalid update manifest: %w", err)
	}
	if _, err = version(result.Version); err != nil {
		return result, err
	}
	if entry.Download != "" {
		result.Download = entry.Download
	}
	if result.Download == "" {
		if !zipName.MatchString(result.File) {
			return result, fmt.Errorf("update manifest needs a ZIP filename or download_url")
		}
		result.Download = base.ResolveReference(&url.URL{Path: result.File}).String()
	}
	asset, err := httpsURL(result.Download)
	if err != nil {
		return result, err
	}
	if !strings.EqualFold(base.Host, asset.Host) {
		return result, fmt.Errorf("manifest and ZIP must have the same origin")
	}
	if result.SHA256 != "" {
		if raw, err := hex.DecodeString(result.SHA256); err != nil || len(raw) != 32 {
			return result, fmt.Errorf("invalid SHA-256 in update manifest")
		}
	}
	return result, nil
}

func unzip(asset, folder string) (string, error) {
	archive, err := zip.OpenReader(asset)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	if len(archive.File) > 20000 {
		return "", fmt.Errorf("too many files in ZIP")
	}
	var total uint64
	for _, file := range archive.File {
		if file.UncompressedSize64 > packageLimit-total {
			return "", fmt.Errorf("ZIP exceeds size limit")
		}
		total += file.UncompressedSize64
		name := filepath.FromSlash(file.Name)
		if !filepath.IsLocal(name) || strings.ContainsAny(file.Name, `\:`) {
			return "", fmt.Errorf("invalid path in ZIP")
		}
		path := filepath.Join(folder, name)
		if file.FileInfo().IsDir() {
			if err = os.MkdirAll(path, 0700); err != nil {
				return "", err
			}
			continue
		}
		if !file.Mode().IsRegular() {
			return "", fmt.Errorf("ZIP may only contain files and directories")
		}
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return "", err
		}
		reader, err := file.Open()
		if err != nil {
			return "", err
		}
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			reader.Close()
			return "", err
		}
		_, readErr := io.Copy(out, reader)
		reader.Close()
		closeErr := out.Close()
		if readErr != nil {
			return "", readErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	if fileExists(filepath.Join(folder, "manifest.json")) {
		return folder, nil
	}
	var candidates []string
	err = filepath.WalkDir(folder, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "manifest.json" {
			candidates = append(candidates, filepath.Dir(path))
		}
		return err
	})
	if err != nil {
		return "", err
	}
	if len(candidates) != 1 {
		return "", fmt.Errorf("ZIP must contain one browser extension")
	}
	return candidates[0], nil
}

func (s *Store) stage(source, hash, folder string) (string, error) {
	asset := source
	if strings.Contains(source, "://") {
		response, err := s.get(source)
		if err != nil {
			return "", err
		}
		defer response.Body.Close()
		asset = filepath.Join(folder, "download.zip")
		file, err := os.OpenFile(asset, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return "", err
		}
		n, readErr := io.Copy(file, io.LimitReader(response.Body, packageLimit+1))
		closeErr := file.Close()
		if readErr != nil {
			return "", readErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if n > packageLimit {
			return "", fmt.Errorf("ZIP exceeds size limit")
		}
	} else if info, err := os.Stat(source); err == nil && info.IsDir() {
		return source, nil
	}
	if hash != "" {
		actual, err := fileDigest(asset)
		if err != nil {
			return "", err
		}
		if !strings.EqualFold(hash, actual) {
			return "", fmt.Errorf("ZIP SHA-256 does not match the update manifest")
		}
	}
	return unzip(asset, filepath.Join(folder, "unpacked"))
}

func (s *Store) sync(entry Entry) (string, error) {
	old, err := readManifest(entry.Path)
	existing := fileExists(entry.Path)
	if existing && err != nil {
		return "", err
	}
	if entry.Source == "" || pathKey(entry.Source) == pathKey(entry.Path) {
		return "local", nil
	}
	source := entry.Source
	var upstream release
	if entry.SourceKind == "manifest" {
		upstream, err = s.release(entry)
		if err != nil {
			return "", err
		}
		if existing && compare(upstream.Version, old.Version) <= 0 {
			return "current", nil
		}
		source = upstream.Download
	}
	parent := filepath.Dir(entry.Path)
	if !strings.Contains(source, "://") && (strings.HasPrefix(pathKey(source), pathKey(entry.Path)+string(os.PathSeparator)) || strings.HasPrefix(pathKey(parent)+string(os.PathSeparator), pathKey(source)+string(os.PathSeparator))) {
		if info, e := os.Stat(source); e == nil && info.IsDir() {
			return "", fmt.Errorf("source and install folders must be separate")
		}
		if strings.HasPrefix(pathKey(source), pathKey(entry.Path)+string(os.PathSeparator)) {
			return "", fmt.Errorf("source must be outside the install folder")
		}
	}
	if err = os.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(parent, ".extension-stage-")
	if err != nil {
		return "", err
	}
	defer func() {
		if stage != "" {
			os.RemoveAll(stage)
		}
	}()
	packagePath, err := s.stage(source, upstream.SHA256, stage)
	if err != nil {
		return "", err
	}
	next, err := readManifest(packagePath)
	if err != nil {
		return "", err
	}
	if upstream.Version != "" && next.Version != upstream.Version {
		return "", fmt.Errorf("ZIP version does not match the update manifest")
	}
	if existing {
		if old.Name != next.Name || old.Key != next.Key {
			return "", fmt.Errorf("ZIP does not match the installed extension")
		}
		if compare(next.Version, old.Version) < 0 {
			return "", fmt.Errorf("refusing to replace an extension with an older version")
		}
		if compare(next.Version, old.Version) == 0 && packagePath != source {
			return "current", nil
		}
	}
	payload := filepath.Join(stage, "payload")
	if packagePath == source {
		if err = os.CopyFS(payload, os.DirFS(packagePath)); err != nil {
			return "", err
		}
	} else if err = os.Rename(packagePath, payload); err != nil {
		return "", err
	}
	previous := filepath.Join(stage, "previous")
	if existing {
		if err = os.Rename(entry.Path, previous); err != nil {
			return "", err
		}
	}
	if err = os.Rename(payload, entry.Path); err != nil {
		if existing {
			if rollback := os.Rename(previous, entry.Path); rollback != nil {
				folder := stage
				stage = ""
				return "", fmt.Errorf("update and rollback failed; previous files are in %s", folder)
			}
		}
		return "", err
	}
	return "updated", nil
}

func (s *Store) Check(paths []string, apply bool) []Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Result, 0, len(paths))
	for _, path := range paths {
		r := Result{Path: path}
		entry, err := s.find(path)
		if err == nil {
			if apply {
				r.State, err = s.sync(entry)
				if err == nil {
					if m, e := readManifest(entry.Path); e == nil {
						r.Version = m.Version
					}
				}
			} else if entry.SourceKind == "manifest" {
				var release release
				release, err = s.release(entry)
				if err == nil {
					var m manifest
					m, err = readManifest(entry.Path)
					if err == nil {
						r.Version = release.Version
						r.State = "current"
						if compare(release.Version, m.Version) > 0 {
							r.State = "available"
						}
					}
				}
			} else {
				r.State = "manual"
			}
		}
		if err != nil {
			r.State = "error"
			r.Error = err.Error()
		}
		result = append(result, r)
	}
	return result
}
