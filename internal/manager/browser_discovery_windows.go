package manager

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

func detectBrowser(id string) (BrowserConfig, bool) {
	local := os.Getenv("LOCALAPPDATA")
	var product, exe, data string
	switch id {
	case "edge":
		product, exe, data = "Microsoft/Edge", "msedge.exe", "User Data"
	case "chrome":
		product, exe, data = "Google/Chrome", "chrome.exe", "User Data"
	case "chromium":
		product, exe, data = "Chromium", "chrome.exe", "User Data"
	}
	config := BrowserConfig{}
	if filepath.IsAbs(local) {
		config.DataDirectory = filepath.Join(local, filepath.FromSlash(product), data)
	}
	if id != "chromium" {
		for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
			for _, view := range []uint32{registry.WOW64_64KEY, registry.WOW64_32KEY} {
				key, err := registry.OpenKey(root, `Software\Microsoft\Windows\CurrentVersion\App Paths\`+exe, registry.QUERY_VALUE|view)
				if err != nil {
					continue
				}
				v, kind, err := key.GetStringValue("")
				key.Close()
				if err != nil {
					continue
				}
				if kind == registry.EXPAND_SZ {
					v, err = registry.ExpandString(v)
				}
				if err == nil && filepath.IsAbs(v) {
					config.Executable = executable(v)
					if config.Executable != "" {
						return config, false
					}
				}
			}
		}
	}
	for _, root := range []string{local, os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles")} {
		if filepath.IsAbs(root) {
			config.Executable = executable(filepath.Join(root, filepath.FromSlash(product), "Application", exe))
			if config.Executable != "" {
				break
			}
		}
	}
	return config, false
}
