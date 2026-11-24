package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	goruntime "runtime"
	"slices"
	"strings"
	"sync"

	"github.com/jamiu99/browser-extension-manager/internal/cli"
	"github.com/jamiu99/browser-extension-manager/internal/manager"
	"github.com/jamiu99/browser-extension-manager/internal/selfupdate"
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
	ctx          context.Context
	store        *manager.Store
	browsers     manager.BrowserSettings
	updateMu     sync.Mutex
	updateNotice string
}

type Catalog struct {
	Extensions      []manager.Extension `json:"extensions"`
	Browsers        []manager.Browser   `json:"browsers"`
	Version         string              `json:"version"`
	Commit          string              `json:"commit"`
	BuiltAt         string              `json:"builtAt"`
	AutomaticReload bool                `json:"automaticReload"`
}

func (a *App) Catalog(importInstalled bool) (Catalog, error) {
	items, err := a.store.List(importInstalled)
	return Catalog{items, a.browsers.Available(), version, commit, builtAt, goruntime.GOOS == "windows"}, err
}

func (a *App) Save(folder, source, kind string) (manager.Entry, error) {
	return a.store.Save(folder, source, kind)
}

func (a *App) EditPlugin(original, folder, source, kind string) (manager.Entry, error) {
	return a.store.Edit(original, folder, source, kind)
}

func (a *App) RemovePlugin(folder string) error { return a.store.Remove(folder) }

func (a *App) PluginStorage() (manager.StorageSettings, error) { return a.store.Storage() }

func (a *App) SavePluginStorage(folder string) error { return a.store.SaveStorage(folder) }

func (a *App) PickPluginStorage() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择默认插件目录"})
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

func (a *App) BrowserSettings() manager.BrowserReport { return a.browsers.Report() }

func (a *App) CheckBrowser(id string, config manager.BrowserConfig) (manager.BrowserStatus, error) {
	return a.browsers.Inspect(id, config)
}

func (a *App) SaveBrowser(id string, config manager.BrowserConfig) (manager.BrowserStatus, error) {
	return a.browsers.Save(id, config)
}

func (a *App) PickBrowserPath(kind string) (string, error) {
	if kind == "data" {
		return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择浏览器用户数据目录"})
	}
	options := runtime.OpenDialogOptions{Title: "选择浏览器程序"}
	if goruntime.GOOS == "windows" {
		options.Filters = []runtime.FileFilter{{DisplayName: "应用程序", Pattern: "*.exe"}}
	}
	return runtime.OpenFileDialog(a.ctx, options)
}

func (a *App) BrowserPrompt() string {
	exe, _ := os.Executable()
	return cli.BrowserPrompt(exe)
}

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
	var targets []manager.Browser
	for _, browser := range a.browsers.Available() {
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
	if install {
		if err := a.Copy(selected[0].Path); err != nil {
			return nil, err
		}
	}
	var messages []string
	for _, browser := range targets {
		profiles := []string{browser.Instance}
		if !install {
			profiles = nil
			for _, item := range selected {
				loaded := false
				for _, load := range item.Loads {
					if load.Browser == browser.ID {
						loaded = true
						if !slices.Contains(profiles, load.Profile) {
							profiles = append(profiles, load.Profile)
						}
					}
				}
				if !loaded {
					messages = append(messages, browser.Name+" · "+item.Name+"尚未加载，可以从管理器安装。")
				}
			}
		}
		for _, profile := range profiles {
			name := browser.Name
			if profile != "" {
				name += " · " + profile
			}
			cmd := exec.Command(browser.Exe, append(browser.LaunchOptions(profile), browser.Page)...)
			if err := cmd.Start(); err != nil {
				messages = append(messages, name+" · 打开失败，请在浏览器中输入 "+browser.Page)
				continue
			}
			_ = cmd.Process.Release()
			if install {
				messages = append(messages, name+" · 打开开发者模式，选择「加载已解压的扩展」，粘贴已复制的目录")
			} else {
				messages = append(messages, name+" · 在扩展卡片上点击「重新加载」以生效")
			}
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
	notice, worker, updateErr := selfupdate.HandleArguments(os.Args[1:])
	updateWorkspace := ""
	if worker {
		if updateErr != nil {
			log.Fatal(updateErr)
		}
		return
	}
	if notice != "" {
		updateWorkspace = os.Args[2]
	}
	if notice != "" || updateErr != nil {
		os.Args = os.Args[:1]
	}
	dir, legacy, err := manager.DefaultPaths()
	if err != nil {
		log.Fatal(err)
	}
	browsers := manager.BrowserSettings{Dir: dir}
	app := &App{store: manager.New(dir, legacy, browsers.Available), browsers: browsers, updateNotice: notice}
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
		case name == "browser" || name == "--help" || name == "-h":
			if err := cli.Run(browsers, os.Args[1:], os.Stdout, os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
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
		OnDomReady:       func(_ context.Context) { selfupdate.MarkReady(updateWorkspace) },
		Bind:             []interface{}{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
