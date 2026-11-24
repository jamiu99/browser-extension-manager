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
	type extension struct{ Name, ID, Version string }
	type target struct {
		Name, Exe, Address, Profile string
		Extensions                  []extension
	}
	var targets []target
	var messages []string
	operations := 0
	for _, b := range browsers {
		if install {
			targets = append(targets, target{Name: b.Name, Exe: b.Exe, Address: b.Page})
			continue
		}
		profiles := make(map[string]int)
		for _, item := range items {
			loaded := false
			for _, load := range item.Loads {
				if load.Browser != b.ID {
					continue
				}
				loaded = true
				index, exists := profiles[load.Profile]
				if !exists {
					index = len(targets)
					profiles[load.Profile] = index
					targets = append(targets, target{Name: b.Name, Exe: b.Exe, Address: b.Page, Profile: load.Profile})
				}
				targets[index].Extensions = append(targets[index].Extensions, extension{item.Name, load.ID, item.Version})
				operations++
			}
			if !loaded {
				messages = append(messages, b.Name+" · "+item.Name+"尚未加载，可以从管理器安装。")
			}
		}
	}
	if len(targets) == 0 {
		return messages
	}
	failure := func() []string {
		if install {
			return nil
		}
		return append(messages, "浏览器重载未完成，请在管理器里点「重载所选」重试。")
	}
	data, err := json.Marshal(struct {
		Targets []target
		Install bool
	}{targets, install})
	if err != nil {
		return failure()
	}
	folder, err := os.MkdirTemp("", "extension-manager-")
	if err != nil {
		return failure()
	}
	defer os.RemoveAll(folder)
	script := filepath.Join(folder, "browser.ps1")
	input := filepath.Join(folder, "targets.json")
	if os.WriteFile(script, automationScript, 0600) != nil || os.WriteFile(input, data, 0600) != nil {
		return failure()
	}
	timeout := time.Duration(20+len(targets)*20+operations*12) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	powershell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, powershell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, input)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err = cmd.Run()
	messages = append(messages, strings.FieldsFunc(stdout.String(), func(r rune) bool { return r == '\r' || r == '\n' })...)
	if err != nil || stdout.Len() == 0 {
		return failure()
	}
	return messages
}
