package manager

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestBrowserSettingsValidatePersistAndReset(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "Custom User Data")
	profile := filepath.Join(data, "Profile 7")
	if err := os.MkdirAll(profile, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "Preferences"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	config := BrowserConfig{exe, data, "Profile 7"}
	settings := BrowserSettings{Dir: filepath.Join(root, "settings")}
	status, err := settings.Inspect("edge", config)
	if err != nil || !status.Ready || !slices.Equal(status.Profiles, []string{"Profile 7"}) {
		t.Fatalf("inspect: %+v, %v", status, err)
	}
	if fileExists(status.SettingsFile) {
		t.Fatal("inspection wrote settings")
	}
	if _, err := settings.Save("edge", config); err != nil {
		t.Fatal(err)
	}
	reloaded, err := settings.Status("edge")
	if err != nil || reloaded.Config != config {
		t.Fatalf("saved settings: %+v, %v", reloaded, err)
	}
	browser := settings.Available()[0]
	want := []string{"--user-data-dir=" + data, "--profile-directory=Profile 7"}
	if !slices.Equal(browser.LaunchOptions(browser.Instance), want) {
		t.Fatalf("launch options: %v", browser.LaunchOptions(browser.Instance))
	}
	invalid := config
	invalid.Profile = "Missing"
	if _, err := settings.Save("edge", invalid); err == nil {
		t.Fatal("invalid profile was saved")
	}
	if actual, _ := settings.Status("edge"); actual.Config != config {
		t.Fatalf("invalid write overwrote settings: %+v", actual.Config)
	}
	if _, err := settings.Save("../wrong", config); err == nil {
		t.Fatal("unknown browser was accepted")
	}
	if _, err := settings.Save("edge", BrowserConfig{}); err != nil {
		t.Fatal(err)
	}
	if fileExists(reloaded.SettingsFile) {
		t.Fatal("reset retained the override")
	}
}

func TestBrowserImportUsesConfiguredProfile(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "profile-data")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Default", "Profile 1"} {
		folder := filepath.Join(root, name+" Extension")
		writePackage(t, folder, name, "1.0")
		if err := writeJSON(filepath.Join(data, name, "Preferences"), map[string]any{
			"extensions": map[string]any{"settings": map[string]any{
				"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": map[string]any{"location": 4, "path": folder},
			}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	settings := BrowserSettings{Dir: filepath.Join(root, "settings")}
	if _, err := settings.Save("edge", BrowserConfig{exe, data, "Profile 1"}); err != nil {
		t.Fatal(err)
	}
	store := New(settings.Dir, "", settings.Available)
	items, err := store.List(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if pathKey(item.Path) == pathKey(filepath.Join(root, "Default Extension")) {
			t.Fatal("import included the unselected profile")
		}
		if pathKey(item.Path) == pathKey(filepath.Join(root, "Profile 1 Extension")) &&
			slices.Contains(item.Loads, BrowserLoad{"edge", "Profile 1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}) {
			return
		}
	}
	t.Fatal("import did not use the saved browser profile")
}
