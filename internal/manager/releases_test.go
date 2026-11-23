package manager

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writePackage(t *testing.T, folder, name, version string) {
	t.Helper()
	if err := os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte(fmt.Sprintf(`{"name":%q,"version":%q,"manifest_version":3}`, name, version))
	if err := os.WriteFile(filepath.Join(folder, "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "package.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, body := range files {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCheckAndApplyManifestUpdate(t *testing.T) {
	data := makeZip(t, map[string]string{
		"extension/manifest.json": `{"name":"Example","version":"1.10.0","manifest_version":3}`,
		"extension/background.js": "// build",
	})
	sum := sha256.Sum256(data)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest.json":
			fmt.Fprintf(w, `{"version":"1.10.0","file":"package.zip","sha256":%q}`, hex.EncodeToString(sum[:]))
		case "/package.zip":
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	store := New(filepath.Join(t.TempDir(), "catalog"), "", nil)
	store.Client = server.Client()
	installed := filepath.Join(t.TempDir(), "loaded")
	writePackage(t, installed, "Example", "1.9.0")
	if _, err := store.Save(installed, server.URL+"/latest.json", ""); err != nil {
		t.Fatal(err)
	}
	if result := store.Check([]string{installed}, false)[0]; result.State != "available" || result.Version != "1.10.0" {
		t.Fatalf("check result: %+v", result)
	}
	if result := store.Check([]string{installed}, true)[0]; result.State != "updated" || result.Version != "1.10.0" {
		t.Fatalf("update result: %+v", result)
	}
	if result := store.Check([]string{installed}, true)[0]; result.State != "current" {
		t.Fatalf("repeated update result: %+v", result)
	}
	if !fileExists(filepath.Join(installed, "background.js")) {
		t.Fatal("new payload did not replace the original directory")
	}
}

func TestRejectUnsafePackageWithoutReplacingInstalledFiles(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"traversal": {"../outside": "must not escape", "manifest.json": `{"name":"Example","version":"2.0","manifest_version":3}`},
		"identity":  {"manifest.json": `{"name":"Another extension","version":"2.0","manifest_version":3}`},
		"downgrade": {"manifest.json": `{"name":"Example","version":"0.9","manifest_version":3}`},
	} {
		t.Run(name, func(t *testing.T) {
			store := New(filepath.Join(t.TempDir(), "catalog"), "", nil)
			installed := filepath.Join(t.TempDir(), "loaded")
			writePackage(t, installed, "Example", "1.0")
			zipFile := filepath.Join(t.TempDir(), "package.zip")
			if err := os.WriteFile(zipFile, makeZip(t, files), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Save(installed, zipFile, ""); err != nil {
				t.Fatal(err)
			}
			if result := store.Check([]string{installed}, true)[0]; result.State != "error" {
				t.Fatalf("expected rejection: %+v", result)
			}
			m, err := readManifest(installed)
			if err != nil || m.Version != "1.0" {
				t.Fatalf("existing manifest changed: %+v, %v", m, err)
			}
		})
	}
}

func TestImportLegacyUpdatesWithoutSharingUserData(t *testing.T) {
	legacy := t.TempDir()
	folder := filepath.Join(t.TempDir(), "existing")
	writePackage(t, folder, "Example", "1.0")
	catalog := fmt.Sprintf(`[{"path":%q,"source":""}]`, folder)
	for file, value := range map[string]string{
		"extensions.json":     catalog,
		"ReleaseSources.json": `{"Example":{"latest":"https://example.com/latest.json","download":"https://example.com/package.zip"}}`,
	} {
		if err := os.WriteFile(filepath.Join(legacy, file), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store := New(t.TempDir(), legacy, nil)
	items, err := store.List(true)
	if err != nil || len(items) != 1 || items[0].SourceKind != "manifest" || items[0].Download != "https://example.com/package.zip" {
		t.Fatalf("legacy migration: %+v, %v", items, err)
	}
	items, err = New(store.Dir, "", nil).List(false)
	if err != nil || len(items) != 1 || items[0].Path != folder || items[0].Source == "" {
		t.Fatalf("migrated catalog: %+v, %v", items, err)
	}
}
