package manager

import (
	"os"
	"path/filepath"
	"strings"
)

func detectBrowser(id string) (BrowserConfig, bool) {
	home, _ := os.UserHomeDir()
	config := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(config) && filepath.IsAbs(home) {
		config = filepath.Join(home, ".config")
	}
	if id != "edge" {
		if chromeConfig := os.Getenv("CHROME_CONFIG_HOME"); filepath.IsAbs(chromeConfig) {
			config = chromeConfig
		}
	}
	var folder, flatpakID string
	var names []string
	switch id {
	case "edge":
		folder, flatpakID = "microsoft-edge", "com.microsoft.Edge"
		names = []string{"microsoft-edge", "microsoft-edge-stable"}
	case "chrome":
		folder, flatpakID = "google-chrome", "com.google.Chrome"
		names = []string{"google-chrome", "google-chrome-stable"}
	case "chromium":
		folder, flatpakID = "chromium", "org.chromium.Chromium"
		names = []string{"chromium", "chromium-browser"}
	}
	browser := BrowserConfig{}
	if filepath.IsAbs(config) {
		browser.DataDirectory = filepath.Join(config, folder)
	}
	for _, name := range names {
		for _, target := range []string{name, filepath.Join("/usr/bin", name), filepath.Join("/snap/bin", name)} {
			browser.Executable = executable(target)
			if browser.Executable != "" {
				break
			}
		}
		if browser.Executable != "" {
			break
		}
	}
	if filepath.IsAbs(home) {
		if browser.Executable == "" {
			for _, root := range []string{filepath.Join(home, ".local", "share"), "/var/lib"} {
				if file := executable(filepath.Join(root, "flatpak", "exports", "bin", flatpakID)); file != "" {
					browser.Executable = file
					browser.DataDirectory = filepath.Join(home, ".var", "app", flatpakID, "config", folder)
					return browser, false
				}
			}
		}
		snap := filepath.Join(home, "snap", "chromium", "common", "chromium")
		if id == "chromium" && (strings.HasPrefix(browser.Executable, "/snap/bin/") ||
			(len(profileNames(browser.DataDirectory)) == 0 && len(profileNames(snap)) > 0)) {
			browser.DataDirectory = snap
		}
	}
	if override := os.Getenv("CHROME_USER_DATA_DIR"); id != "edge" && filepath.IsAbs(override) {
		browser.DataDirectory = override
		return browser, true
	}
	return browser, false
}
