package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jamiu99/browser-extension-manager/internal/manager"
)

//go:embed scripts/browser-windows.ps1
var automationScript []byte

func windowsAutomation(browsers []manager.Browser, items []manager.Extension, install bool) []string {
	type target struct{ Name, Exe, Address string }
	var targets []target
	for _, b := range browsers {
		targets = append(targets, target{b.Name, b.Exe, b.Page})
	}
	data, err := json.Marshal(struct {
		Targets []target
		Items   []manager.Extension
		Install bool
	}{targets, items, install})
	if err != nil {
		return nil
	}
	folder, err := os.MkdirTemp("", "extension-manager-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(folder)
	script := filepath.Join(folder, "browser.ps1")
	input := filepath.Join(folder, "targets.json")
	if os.WriteFile(script, automationScript, 0600) != nil || os.WriteFile(input, data, 0600) != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	powershell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, powershell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, input)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil
	}
	return strings.FieldsFunc(stdout.String(), func(r rune) bool { return r == '\r' || r == '\n' })
}
