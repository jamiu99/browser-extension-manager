package cli

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jamiu99/browser-extension-manager/internal/manager"
)

const Usage = `extension-manager-cli browser list
extension-manager-cli browser check <edge|chrome|chromium> [--exe PATH] [--data-dir PATH] [--profile NAME]
extension-manager-cli browser set <edge|chrome|chromium> [--exe PATH] [--data-dir PATH] [--profile NAME]
extension-manager-cli browser reset <edge|chrome|chromium>

list is read-only. check previews the supplied values, or checks saved settings without flags.
set replaces that browser's overrides. Blank or omitted flags use automatic detection.
All browser commands return JSON. check exits nonzero when the browser is unavailable.`

//go:embed browser-prompt.txt
var browserPrompt string

func BrowserPrompt(exe string) string {
	if exe == "" {
		exe = "extension-manager-cli"
	} else {
		name := "extension-manager-cli"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		companion := filepath.Join(filepath.Dir(exe), name)
		if info, err := os.Stat(companion); err == nil && info.Mode().IsRegular() {
			exe = companion
		}
	}
	quoted := "'" + strings.ReplaceAll(exe, "'", "'\"'\"'") + "'"
	if runtime.GOOS == "windows" {
		quoted = "& '" + strings.ReplaceAll(exe, "'", "''") + "'"
	}
	return strings.ReplaceAll(browserPrompt, "{{command}}", quoted)
}

func Run(settings manager.BrowserSettings, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprintln(stdout, Usage)
		return err
	}
	if args[0] != "browser" || len(args) < 2 {
		return fmt.Errorf("使用 --help 查看浏览器配置命令")
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	action := args[1]
	if action == "list" && len(args) == 2 {
		return encoder.Encode(settings.Report())
	}
	if len(args) < 3 {
		return fmt.Errorf("请指定浏览器 ID，使用 --help 查看用法")
	}
	id := args[2]
	if action == "reset" && len(args) == 3 {
		status, err := settings.Save(id, manager.BrowserConfig{})
		if err != nil {
			return err
		}
		return encoder.Encode(status)
	}
	if action != "check" && action != "set" {
		return fmt.Errorf("未知命令 %q", action)
	}
	var config manager.BrowserConfig
	flags := flag.NewFlagSet(action, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&config.Executable, "exe", "", "absolute browser executable or macOS .app")
	flags.StringVar(&config.DataDirectory, "data-dir", "", "absolute browser user data directory")
	flags.StringVar(&config.Profile, "profile", "", "profile directory name")
	if err := flags.Parse(args[3:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("无法识别参数 %q", flags.Arg(0))
	}
	var status manager.BrowserStatus
	var err error
	switch {
	case action == "set":
		if flags.NFlag() == 0 {
			return fmt.Errorf("请传入要保存的参数，或使用 browser reset %s", id)
		}
		status, err = settings.Save(id, config)
	case flags.NFlag() == 0:
		status, err = settings.Status(id)
	default:
		status, err = settings.Inspect(id, config)
	}
	if err != nil {
		return err
	}
	if err := encoder.Encode(status); err != nil {
		return err
	}
	if action == "check" && !status.Ready {
		return fmt.Errorf("%s 的浏览器路径或用户资料不可用，请检查 JSON 中的诊断信息", status.Name)
	}
	return nil
}
