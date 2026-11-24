package manager

import (
	"os"
	"path/filepath"
)

func detectBrowser(id string) (BrowserConfig, bool) {
	home, _ := os.UserHomeDir()
	var bundle, data string
	switch id {
	case "edge":
		bundle, data = "Microsoft Edge", "Microsoft Edge"
	case "chrome":
		bundle, data = "Google Chrome", "Google/Chrome"
	case "chromium":
		bundle, data = "Chromium", "Chromium"
	}
	config := BrowserConfig{}
	if filepath.IsAbs(home) {
		config.DataDirectory = filepath.Join(home, "Library", "Application Support", data)
	}
	for _, root := range []string{"/Applications", filepath.Join(home, "Applications")} {
		if filepath.IsAbs(root) {
			config.Executable = executable(filepath.Join(root, bundle+".app"))
			if config.Executable != "" {
				break
			}
		}
	}
	return config, false
}
