package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const fixtureEnv = "BROWSER_EXTENSION_MANAGER_UPDATE_TEST"
const fixtureFail = "BROWSER_EXTENSION_MANAGER_UPDATE_TEST_FAIL"
const fixtureOld = "v1.1.0"
const fixtureNew = "v1.2.0"
const fixtureMarker = "EXTENSION-MANAGER-TEST-NEW"
const fixtureBuiltAt = "2025-01-02T03:04:05Z"

var fixtureCommit = strings.Repeat("1", 40)

func TestMain(m *testing.M) {
	if fixture := os.Getenv(fixtureEnv); fixture != "" {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		contents, err := os.ReadFile(exe)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		runningVersion := fixtureOld
		info, err := os.Stat(exe)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		// Use a mode marker on macOS so the fixture's Mach-O signature is preserved.
		if bytes.HasSuffix(contents, []byte(fixtureMarker)) || (runtime.GOOS == "darwin" && info.Mode().Perm() == 0755) {
			runningVersion = fixtureNew
		}
		switch os.Args[1] {
		case "--version":
			fmt.Println(runningVersion, fixtureCommit, fixtureBuiltAt)
		case "--start-fixture-update":
			var archive []byte
			archive, err = os.ReadFile(filepath.Join(fixture, "package"))
			if err == nil {
				s := fixtureService(fixtureNew, assetName(fixtureNew, runtime.GOOS, runtime.GOARCH), string(archive))
				err = s.Start(context.Background(), runningVersion, fixtureNew)
			}
		case finishFlag, resultFlag:
			if os.Args[1] == resultFlag && runningVersion == fixtureNew && os.Getenv(fixtureFail) == "1" {
				os.Exit(1)
			}
			var notice string
			notice, _, err = HandleArguments(os.Args[1:])
			if err == nil && os.Args[1] == resultFlag {
				MarkReady(os.Args[2])
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					if _, err := os.Stat(os.Args[2]); os.IsNotExist(err) {
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
				text := fmt.Sprintf("%d\n%s %s", os.Getpid(), runningVersion, notice)
				err = os.WriteFile(filepath.Join(fixture, "result.txt"), []byte(text), 0600)
			}
		default:
			err = fmt.Errorf("unexpected fixture command")
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	os.Exit(m.Run())
}

func archiveFixture(t *testing.T, files map[string][]byte, zipped bool) []byte {
	t.Helper()
	var contents bytes.Buffer
	if zipped {
		z := zip.NewWriter(&contents)
		for name, data := range files {
			w, err := z.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(data); err != nil {
				t.Fatal(err)
			}
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		gz := gzip.NewWriter(&contents)
		archive := tar.NewWriter(gz)
		for name, data := range files {
			if err := archive.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: 0755}); err != nil {
				t.Fatal(err)
			}
			if _, err := archive.Write(data); err != nil {
				t.Fatal(err)
			}
		}
		if err := archive.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return contents.Bytes()
}

func TestInstallRestartsAndPreservesUserConfig(t *testing.T) {
	t.Run("successful", func(t *testing.T) { testInstall(t, false) })
	t.Run("startup failure", func(t *testing.T) { testInstall(t, true) })
}

func testInstall(t *testing.T, fail bool) {
	if assetName(fixtureNew, runtime.GOOS, runtime.GOARCH) == "" {
		t.Skip("no release archive for this target")
	}
	root := t.TempDir()
	install := filepath.Join(root, "Installed Application")
	if runtime.GOOS == "darwin" {
		install = filepath.Join(install, "Renamed App.app", "Contents", "MacOS")
	}
	if err := os.MkdirAll(install, 0700); err != nil {
		t.Fatal(err)
	}
	name := "Current Launcher"
	if runtime.GOOS == "windows" {
		name += ".exe"
	} else if runtime.GOOS == "darwin" {
		name = "extension-manager"
	}
	target := filepath.Join(install, name)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := copyExecutable(exe, target); err != nil {
		t.Fatal(err)
	}
	layout, err := installLayout(target, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(layout.parent, "extensions.json")
	if err := os.WriteFile(config, []byte("existing settings"), 0600); err != nil {
		t.Fatal(err)
	}

	program, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := json.Marshal(releaseMetadata{fixtureNew, fixtureCommit, fixtureBuiltAt})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "darwin" {
		program = append(program, []byte(fixtureMarker)...)
	}
	files := map[string][]byte{
		filepath.ToSlash(layout.stagedExe): program,
		"release.json":                     metadata,
		layout.cli:                         []byte("new companion"),
	}
	zipped := runtime.GOOS != "linux"
	if err := os.WriteFile(filepath.Join(root, "package"), archiveFixture(t, files, zipped), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(target, "--start-fixture-update")
	detachConsole(cmd)
	cmd.Env = append(os.Environ(), fixtureEnv+"="+root)
	expectedVersion, expectedNotice := fixtureNew, "软件已更新到 "+fixtureNew
	if fail {
		cmd.Env = append(cmd.Env, fixtureFail+"=1")
		expectedVersion, expectedNotice = fixtureOld, "已保留原版本"
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("could not stage fixture update: %s %v", output, err)
	}
	// The update worker waits for the fixture to quit before replacing its files.
	deadline := time.Now().Add(20 * time.Second)
	for {
		b, err := os.ReadFile(filepath.Join(root, "result.txt"))
		if err == nil {
			pidText, result, _ := strings.Cut(string(b), "\n")
			if !strings.HasPrefix(result, expectedVersion+" ") || !strings.Contains(result, expectedNotice) {
				t.Fatalf("app restarted with an unexpected result: %s", result)
			}
			pid, err := strconv.Atoi(pidText)
			if err != nil || pid <= 0 || waitForExit(pid, 5*time.Second) != nil {
				t.Fatalf("fixture process %s did not exit", pidText)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the update worker did not restart the fixture")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if data, err := os.ReadFile(config); err != nil || string(data) != "existing settings" {
		t.Fatalf("existing config was changed: %q, %v", data, err)
	}
	data, err := os.ReadFile(filepath.Join(layout.parent, layout.cli))
	if (fail && !os.IsNotExist(err)) || (!fail && (err != nil || string(data) != "new companion")) {
		t.Fatalf("unexpected command-line tool: %q, %v", data, err)
	}
}

func TestSwapRestoresFilesOnRestartFailure(t *testing.T) {
	stage := t.TempDir()
	target := filepath.Join(stage, "installed")
	source := filepath.Join(stage, "incoming")
	for path, body := range map[string]string{target: "original", source: "new"} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	startupErr := fmt.Errorf("app cannot start")
	err, keep := swap(stage, []replacement{{source, target}}, func() error { return startupErr })
	if !errors.Is(err, startupErr) || keep {
		t.Fatalf("unexpected rollback result: %v, %v", err, keep)
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != "original" {
		t.Fatalf("old app was not restored: %q %v", b, err)
	}
}

func TestSoftwareArchiveRejectsEscapingPath(t *testing.T) {
	for _, zipped := range []bool{true, false} {
		root := t.TempDir()
		file := filepath.Join(root, "package")
		if err := os.WriteFile(file, archiveFixture(t, map[string][]byte{"../outside": []byte("unsafe")}, zipped), 0600); err != nil {
			t.Fatal(err)
		}
		if err := unpack(file, filepath.Join(root, "payload"), zipped); err == nil {
			t.Fatal("archive path outside the payload was accepted")
		}
		if _, err := os.Stat(filepath.Join(root, "outside")); !os.IsNotExist(err) {
			t.Fatalf("archive wrote outside payload: %v", err)
		}
	}
}
