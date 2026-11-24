package manager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCatalogKeepsUnpackedExtensionIdentityPerProfile(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "plugin")
	writePackage(t, folder, "Example", "1.0")
	edge := Browser{ID: "edge", Profile: filepath.Join(root, "edge")}
	chrome := Browser{ID: "chrome", Profile: filepath.Join(root, "chrome")}
	first := BrowserLoad{"edge", "Default", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	second := BrowserLoad{"edge", "Profile 1", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	third := BrowserLoad{"chrome", "Default", "cccccccccccccccccccccccccccccccc"}
	for _, record := range []struct {
		browser Browser
		load    BrowserLoad
	}{{edge, first}, {edge, second}, {chrome, third}} {
		directory := filepath.Join(record.browser.Profile, record.load.Profile)
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		prefs := map[string]any{"extensions": map[string]any{"settings": map[string]any{
			record.load.ID: map[string]any{"location": 4, "path": folder},
		}}}
		data, err := json.Marshal(prefs)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"Preferences", "Secure Preferences"} {
			if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	store := New(filepath.Join(root, "catalog"), "", []Browser{edge, chrome})
	items, err := store.List(false)
	if err != nil || len(items) != 1 {
		t.Fatalf("catalog: %+v, %v", items, err)
	}
	if !slices.Equal(items[0].Browsers, []string{"edge", "chrome"}) ||
		!slices.Equal(items[0].Loads, []BrowserLoad{first, second, third}) {
		t.Fatalf("browser loads: %+v", items[0])
	}
}
