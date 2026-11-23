package manager

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Browser struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Page    string `json:"page"`
	Exe     string `json:"-"`
	Profile string `json:"-"`
}

func Browsers() []Browser {
	home, _ := os.UserHomeDir()
	local := os.Getenv("LOCALAPPDATA")
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	var result []Browser
	add := func(id, name, page, profile string, targets ...string) {
		for _, target := range targets {
			if target == "" {
				continue
			}
			if exe, err := exec.LookPath(target); err == nil {
				result = append(result, Browser{id, name, page, exe, profile})
				return
			}
		}
	}
	switch runtime.GOOS {
	case "windows":
		locations := []string{local, os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles")}
		candidates := func(relative string) []string {
			var out []string
			for _, location := range locations {
				if location != "" {
					out = append(out, filepath.Join(location, relative))
				}
			}
			return out
		}
		add("edge", "Edge", "edge://extensions", filepath.Join(local, "Microsoft", "Edge", "User Data"), candidates(`Microsoft\Edge\Application\msedge.exe`)...)
		add("chrome", "Chrome", "chrome://extensions", filepath.Join(local, "Google", "Chrome", "User Data"), candidates(`Google\Chrome\Application\chrome.exe`)...)
	case "darwin":
		apps := func(name string) []string {
			relative := filepath.Join(name+".app", "Contents", "MacOS", name)
			return []string{filepath.Join("/Applications", relative), filepath.Join(home, "Applications", relative)}
		}
		base := filepath.Join(home, "Library", "Application Support")
		add("edge", "Edge", "edge://extensions", filepath.Join(base, "Microsoft Edge"), apps("Microsoft Edge")...)
		add("chrome", "Chrome", "chrome://extensions", filepath.Join(base, "Google", "Chrome"), apps("Google Chrome")...)
		add("chromium", "Chromium", "chrome://extensions", filepath.Join(base, "Chromium"), apps("Chromium")...)
	default:
		add("edge", "Edge", "edge://extensions", filepath.Join(config, "microsoft-edge"), "microsoft-edge", "microsoft-edge-stable")
		add("chrome", "Chrome", "chrome://extensions", filepath.Join(config, "google-chrome"), "google-chrome", "google-chrome-stable")
		profile := filepath.Join(config, "chromium")
		if _, err := os.Stat(profile); err != nil {
			if snap := filepath.Join(home, "snap", "chromium", "common", "chromium"); fileExists(snap) {
				profile = snap
			}
		}
		add("chromium", "Chromium", "chrome://extensions", profile, "chromium", "chromium-browser")
	}
	return result
}

type loadedExtension struct {
	Path    string
	ID      string
	Browser string
}

func scanBrowsers(browsers []Browser) []loadedExtension {
	var result []loadedExtension
	for _, browser := range browsers {
		profiles, _ := os.ReadDir(browser.Profile)
		for _, profile := range profiles {
			name := profile.Name()
			if !profile.IsDir() || (name != "Default" && !strings.HasPrefix(name, "Profile ")) {
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
					result = append(result, loadedExtension{filepath.Clean(entry.Path), id, browser.ID})
				}
			}
		}
	}
	return result
}
