package manager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewPackageUsesConfiguredDefaultDirectory(t *testing.T) {
	root := t.TempDir()
	store := New(filepath.Join(root, "catalog"), "", nil)
	old := filepath.Join(root, "existing")
	writePackage(t, old, "Existing", "1.0")
	if _, err := store.Save(old, "", ""); err != nil {
		t.Fatal(err)
	}
	storage, err := store.Storage()
	if err != nil || storage.PackageDir != filepath.Join(store.Dir, "packages") {
		t.Fatalf("default directory: %+v %v", storage, err)
	}
	destination := filepath.Join(root, "My extensions")
	if err := store.SaveStorage(destination); err != nil {
		t.Fatal(err)
	}
	writePackage(t, filepath.Join(destination, "new-extension"), "Unrelated", "1.0")
	source := filepath.Join(root, "source.zip")
	zip := makeZip(t, map[string]string{"manifest.json": `{"name":"New Extension","version":"1.0","manifest_version":3}`})
	if err := os.WriteFile(source, zip, 0600); err != nil {
		t.Fatal(err)
	}
	item, err := store.Save("", source, "")
	if err != nil || item.Path != filepath.Join(destination, "new-extension-2") {
		t.Fatalf("package was not installed below the configured directory: %+v, %v", item, err)
	}
	if m, err := readManifest(filepath.Join(destination, "new-extension")); err != nil || m.Name != "Unrelated" {
		t.Fatalf("existing folder was overwritten: %+v %v", m, err)
	}
	if items, err := store.List(false); err != nil || len(items) != 2 {
		t.Fatalf("catalog: %+v %v", items, err)
	}
	if !fileExists(filepath.Join(old, "manifest.json")) {
		t.Fatal("changing default directory moved an existing plugin")
	}
}

func TestEditAndRemoveRespectBrowserLoadedPaths(t *testing.T) {
	root := t.TempDir()
	old, next := filepath.Join(root, "old"), filepath.Join(root, "new")
	writePackage(t, old, "Example", "1.0")
	writePackage(t, next, "Example", "1.1")
	browser := Browser{ID: "edge", Profile: filepath.Join(root, "edge")}
	if err := writeJSON(filepath.Join(browser.Profile, "Default", "Preferences"), map[string]any{
		"extensions": map[string]any{"settings": map[string]any{
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": map[string]any{"location": 4, "path": old},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	store := New(filepath.Join(root, "catalog"), "", func() []Browser { return []Browser{browser} })
	source := "https://example.com/latest.json"
	download := "https://example.com/version.zip"
	if err := store.writeEntries([]Entry{{Path: old, Source: source, SourceKind: "manifest", Download: download}, {Path: next}}); err != nil {
		t.Fatal(err)
	}
	item, err := store.Edit(old, next, source, "manifest")
	if err != nil || item.Path != next || item.Download != download {
		t.Fatalf("edited entry: %+v %v", item, err)
	}
	if items, err := store.List(true); err != nil || len(items) != 1 || items[0].Path != next {
		t.Fatalf("old record came back from discovery: %+v %v", items, err)
	}
	if err := store.Remove(next); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(old); err != nil {
		t.Fatal(err)
	}
	if items, err := store.List(true); err != nil || len(items) != 0 {
		t.Fatalf("removed item reappeared: %+v %v", items, err)
	}
	if !browser.HasLoaded(old) || !fileExists(filepath.Join(next, "manifest.json")) {
		t.Fatal("editing or removing deleted installed files or removed the browser record")
	}
	if _, err := store.Save(old, "", ""); err != nil {
		t.Fatal(err)
	}
	if items, err := store.List(false); err != nil || len(items) != 1 || items[0].Path != old {
		t.Fatalf("explicitly re-added item could not be restored: %+v %v", items, err)
	}
}

func TestEditCopiesToNewPathAndRejectsAnotherPlugin(t *testing.T) {
	root := t.TempDir()
	old, next, unrelated := filepath.Join(root, "old"), filepath.Join(root, "new"), filepath.Join(root, "other")
	writePackage(t, old, "Example", "1.0")
	writePackage(t, unrelated, "Another", "1.0")
	store := New(filepath.Join(root, "catalog"), "", nil)
	if _, err := store.Save(old, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Edit(old, unrelated, "", ""); err == nil {
		t.Fatal("a different extension was accepted")
	}
	if _, err := store.Edit(old, filepath.Join(old, "nested"), "", ""); err == nil {
		t.Fatal("a nested copy destination was accepted")
	}
	if _, err := store.Edit(old, next, "", ""); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{old, next} {
		if m, err := readManifest(path); err != nil || m.Name != "Example" {
			t.Fatalf("plugin missing at %s: %+v %v", path, m, err)
		}
	}
}
