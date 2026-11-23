package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/jamiu99/browser-extension-manager/internal/manager"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed frontend
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

//go:embed docs/update-feed.md
var updateGuide string

var version = "dev"
var commit = ""
var builtAt = ""

type App struct {
	ctx   context.Context
	store *manager.Store
}

type Catalog struct {
	Extensions []manager.Extension `json:"extensions"`
	Browsers   []manager.Browser   `json:"browsers"`
	Version    string              `json:"version"`
	Commit     string              `json:"commit"`
	BuiltAt    string              `json:"builtAt"`
}

func (a *App) Catalog(importInstalled bool) (Catalog, error) {
	items, err := a.store.List(importInstalled)
	return Catalog{items, a.store.Browsers, version, commit, builtAt}, err
}

func (a *App) Save(folder, source, kind string) (manager.Entry, error) {
	return a.store.Save(folder, source, kind)
}

func (a *App) Check(paths []string, apply bool) []manager.Result {
	return a.store.Check(paths, apply)
}

func (a *App) PickFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择包含 manifest.json 的目录"})
}

func (a *App) PickZIP() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择插件 ZIP", Filters: []runtime.FileFilter{{DisplayName: "ZIP", Pattern: "*.zip"}},
	})
}

func (a *App) Copy(value string) error {
	return runtime.ClipboardSetText(a.ctx, value)
}

func (a *App) Guide() string { return updateGuide }

func (a *App) Activate(ids, paths []string, install bool) ([]string, error) {
	items, err := a.store.List(false)
	if err != nil {
		return nil, err
	}
	selected := make([]manager.Extension, 0, len(paths))
	for _, p := range paths {
		for _, item := range items {
			if item.Path == p && item.Version != "" {
				selected = append(selected, item)
				break
			}
		}
	}
	if len(selected) == 0 || (install && len(selected) != 1) {
		return nil, fmt.Errorf("请选择一个可用插件进行安装，或选择需要重新加载的插件")
	}
	if install {
		if err := a.Copy(selected[0].Path); err != nil {
			return nil, err
		}
	}
	var targets []manager.Browser
	for _, browser := range a.store.Browsers {
		for _, id := range ids {
			if browser.ID == id {
				targets = append(targets, browser)
				break
			}
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("请先选择目标浏览器")
	}
	if messages := windowsAutomation(targets, selected, install); len(messages) > 0 {
		return messages, nil
	}
	var messages []string
	for _, browser := range targets {
		cmd := exec.Command(browser.Exe, browser.Page)
		if err := cmd.Start(); err != nil {
			messages = append(messages, browser.Name+" · 打开失败，请在浏览器中输入 "+browser.Page)
			continue
		}
		_ = cmd.Process.Release()
		if install {
			messages = append(messages, browser.Name+" · 打开开发者模式，选择「加载已解压的扩展」，粘贴已复制的目录")
		} else {
			messages = append(messages, browser.Name+" · 在扩展卡片上点击「重新加载」以生效")
		}
	}
	return messages, nil
}

func setSource(store *manager.Store, name, source string) error {
	items, err := store.List(false)
	if err != nil {
		return err
	}
	var matches []manager.Extension
	for _, item := range items {
		if item.Name == name || item.Path == name {
			matches = append(matches, item)
		}
	}
	if len(matches) != 1 {
		return fmt.Errorf("请提供唯一的插件名称或插件绝对目录")
	}
	item := matches[0]
	_, err = store.Save(item.Path, source, "manifest")
	return err
}

func main() {
	dir, legacy, err := manager.DefaultPaths()
	if err != nil {
		log.Fatal(err)
	}
	app := &App{store: manager.New(dir, legacy, manager.Browsers())}
	if len(os.Args) > 1 {
		name := os.Args[1]
		switch {
		case name == "--version":
			fmt.Printf("%s %s %s\n", version, commit, builtAt)
		case name == "--set-source" && len(os.Args) == 4 && strings.TrimSpace(os.Args[3]) != "":
			if err := setSource(app.store, os.Args[2], os.Args[3]); err != nil {
				log.Fatal(err)
			}
			fmt.Println("已登记。回到管理器刷新列表并检查更新。")
		default:
			log.Fatal("用法：extension-manager [--version | --set-source <插件名称或目录> <HTTPS 清单地址>]")
		}
		return
	}
	frontend, err := fs.Sub(assets, "frontend")
	if err != nil {
		log.Fatal(err)
	}
	err = wails.Run(&options.App{
		Title: "浏览器插件管理器", Width: 1110, Height: 720, MinWidth: 660, MinHeight: 520,
		BackgroundColour: &options.RGBA{R: 246, G: 245, B: 248, A: 255},
		AssetServer:      &assetserver.Options{Assets: frontend},
		Linux:            &linux.Options{Icon: appIcon, ProgramName: "extension-manager"},
		OnStartup:        func(ctx context.Context) { app.ctx = ctx },
		Bind:             []interface{}{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
