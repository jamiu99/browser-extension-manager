package manager

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

type StorageSettings struct {
	PackageDir string `json:"packageDir"`
}

func (s *Store) Storage() (StorageSettings, error) {
	settings := StorageSettings{}
	b, err := os.ReadFile(filepath.Join(s.Dir, "storage.json"))
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &settings); err != nil {
			return settings, fmt.Errorf("无法读取默认插件目录设置")
		}
	case !os.IsNotExist(err):
		return settings, err
	}
	if settings.PackageDir == "" {
		settings.PackageDir = filepath.Join(s.Dir, "packages")
	}
	if !filepath.IsAbs(settings.PackageDir) {
		return settings, fmt.Errorf("默认插件目录需要是绝对路径")
	}
	return settings, nil
}

func (s *Store) SaveStorage(directory string) error {
	directory = strings.TrimSpace(directory)
	if directory != "" {
		if !filepath.IsAbs(directory) {
			return fmt.Errorf("请输入默认插件目录的绝对路径")
		}
		directory = filepath.Clean(directory)
		if err := os.MkdirAll(directory, 0700); err != nil {
			return fmt.Errorf("无法使用此目录，请换一个位置")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(filepath.Join(s.Dir, "storage.json"), StorageSettings{PackageDir: directory})
}

func packageName(value string) string {
	name := strings.Trim(strings.Map(func(c rune) rune {
		if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' || c == '-' {
			return unicode.ToLower(c)
		}
		return '-'
	}, value), "-_")
	runes := []rune(name)
	if len(runes) > 48 {
		name = string(runes[:48])
	}
	if name == "" || !filepath.IsLocal(name) {
		return "extension"
	}
	return name
}

func (s *Store) installNew(entry Entry) (string, error) {
	storage, err := s.Storage()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(storage.PackageDir, 0700); err != nil {
		return "", err
	}
	work, err := os.MkdirTemp(storage.PackageDir, ".extension-import-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	entry.Path = filepath.Join(work, "plugin")
	if _, err := s.sync(entry); err != nil {
		return "", err
	}
	m, err := readManifest(entry.Path)
	if err != nil {
		return "", err
	}
	base := packageName(displayName(m, entry.Path))
	for i := 1; i <= 100; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		dest := filepath.Join(storage.PackageDir, name)
		if _, err := os.Lstat(dest); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if err := os.Rename(entry.Path, dest); err != nil {
			if fileExists(dest) {
				continue
			}
			return "", err
		}
		return dest, nil
	}
	return "", fmt.Errorf("默认插件目录中同名文件夹太多，请换一个位置")
}

// copyToNewPath validates an existing target, or copies the plugin into a new
// directory while leaving the original available to any browser using it.
func copyToNewPath(old, next string) (created string, err error) {
	before, beforeErr := readManifest(old)
	if _, statErr := os.Lstat(next); statErr == nil {
		after, err := readManifest(next)
		if err != nil {
			return "", fmt.Errorf("目标目录需要包含有效的插件 manifest.json")
		}
		if beforeErr == nil && (before.Name != after.Name || before.Key != after.Key) {
			return "", fmt.Errorf("目标目录属于另一个插件")
		}
		return "", nil
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	if beforeErr != nil {
		return "", fmt.Errorf("原目录不可用，请选择已包含插件文件的目录")
	}
	relative, err := filepath.Rel(pathKey(old), pathKey(next))
	if err == nil && filepath.IsLocal(relative) {
		return "", fmt.Errorf("请把插件放到原目录之外")
	}
	parent := filepath.Dir(next)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	work, err := os.MkdirTemp(parent, ".extension-copy-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	payload := filepath.Join(work, "plugin")
	if err := os.CopyFS(payload, os.DirFS(old)); err != nil {
		return "", err
	}
	if _, err := os.Lstat(next); !os.IsNotExist(err) {
		return "", fmt.Errorf("目标目录已存在，请重新选择")
	}
	if err := os.Rename(payload, next); err != nil {
		return "", err
	}
	return next, nil
}
