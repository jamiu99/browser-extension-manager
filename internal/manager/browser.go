package manager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
)

type Browser struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Page       string `json:"page"`
	Exe        string `json:"-"`
	Profile    string `json:"-"`
	Instance   string `json:"instance,omitempty"`
	UseDataDir bool   `json:"-"`
}

func (b Browser) LaunchOptions(instance string) []string {
	var args []string
	if b.UseDataDir {
		args = append(args, "--user-data-dir="+b.Profile)
	}
	if instance != "" {
		args = append(args, "--profile-directory="+instance)
	}
	return args
}

func profileNames(root string) []string {
	profiles := []string{}
	if root == "" {
		return profiles
	}
	dirs, _ := os.ReadDir(root)
	for _, dir := range dirs {
		if !dir.IsDir() || dir.Name() == "Guest Profile" || dir.Name() == "System Profile" {
			continue
		}
		path := filepath.Join(root, dir.Name())
		if fileExists(filepath.Join(path, "Preferences")) || fileExists(filepath.Join(path, "Secure Preferences")) {
			profiles = append(profiles, dir.Name())
		}
	}
	slices.Sort(profiles)
	return profiles
}

type BrowserLoad struct {
	Browser string
	Profile string
	ID      string
}

type loadedExtension struct {
	BrowserLoad
	Path string
}

func scanBrowsers(browsers []Browser) []loadedExtension {
	var result []loadedExtension
	for _, browser := range browsers {
		for _, name := range profileNames(browser.Profile) {
			if browser.Instance != "" && name != browser.Instance {
				continue
			}
			for _, file := range []string{"Preferences", "Secure Preferences"} {
				var prefs struct {
					Extensions struct {
						Settings map[string]struct {
							Location int
							Path     string
						}
					}
				}
				raw, err := os.ReadFile(filepath.Join(browser.Profile, name, file))
				if err != nil || json.Unmarshal(raw, &prefs) != nil {
					continue
				}
				for id, entry := range prefs.Extensions.Settings {
					if entry.Location != 4 || !filepath.IsAbs(entry.Path) || !fileExists(filepath.Join(entry.Path, "manifest.json")) {
						continue
					}
					result = append(result, loadedExtension{BrowserLoad{browser.ID, name, id}, filepath.Clean(entry.Path)})
				}
			}
		}
	}
	return result
}
