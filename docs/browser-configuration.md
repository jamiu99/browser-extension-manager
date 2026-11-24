# 浏览器设置与 AI 配置

管理器在 Windows、macOS 和 Linux 上发现 Edge、Chrome、Chromium，并从选定的用户资料导入以目录加载的扩展。Windows 可以在管理器内重载。macOS 和 Linux 会打开相应用户资料的扩展页，重载在浏览器里确认。

## 在桌面应用配置

1. 打开「浏览器设置」，选择 Edge、Chrome 或 Chromium。输入框里的占位值来自本机自动检测。
2. 浏览器装在自定义位置时，选择程序文件。macOS 也可以选择应用包，例如 `/Applications/Google Chrome.app`。
3. 用户数据目录是 `Default`、`Profile 1` 等资料目录的上一级。选好目录后点「验证路径」，即可看到可选资料。选定资料后，导入和重载均限定到它。资料留空时导入已识别的资料，首次安装使用浏览器默认资料。
4. 点「保存设置」即刻刷新。某个输入留空时使用自动检测。「恢复自动」清除当前浏览器的手动覆盖。「重新检测」可读取命令行写入的新配置。

Windows 查常见安装目录和系统的 App Paths 注册。macOS 查系统和用户的 Applications 目录。Linux 查常见程序名，以及 Chromium Snap 和常见 Flatpak 入口。浏览器组织策略可能指定实际用户数据目录，可以从 `chrome://version` 或 `edge://version` 的资料路径核对。 [Chromium 用户数据目录](https://chromium.googlesource.com/chromium/src/+/main/docs/user_data_dir.md) · [Windows App Paths](https://learn.microsoft.com/en-us/windows/win32/shell/app-registration)

## 给 AI Agent

在应用里点「复制 AI 配置指令」，交给能够访问目标机器文件和终端的 Agent。Agent 先列出检测结果和有效路径，再验证候选设置，得到你的确认后保存。

发布包的 `extension-manager-cli` 与桌面应用共用本机设置。Windows 使用 `extension-manager-cli.exe`。在 PowerShell 中，可用 `& "C:\某个目录\extension-manager-cli.exe" browser list`。从 WSL 管理 Windows 浏览器时，运行 Windows 版 EXE，传入 Windows 路径。从 Linux 版 CLI 看到的是 Linux 的浏览器。

```sh
extension-manager-cli browser list
extension-manager-cli browser check edge --exe "实际程序绝对路径" --data-dir "实际用户数据目录"
extension-manager-cli browser set edge --exe "实际程序绝对路径" --data-dir "实际用户数据目录"
extension-manager-cli browser check edge
extension-manager-cli browser reset edge
```

用 `chrome` 或 `chromium` 可配置其他浏览器。可加 `--profile "Profile 1"` 指定资料。各命令以 JSON 返回检测和配置结果。`check` 按输入预览，省略全部参数时检查已保存的配置。`set` 替换该浏览器的全部覆盖字段。恢复自动检测使用 `reset`。Agent 可先调用 `list` 找到 `settingsFile`、`detected`、`effective` 和 `profiles`。

设置也可手动编辑。文件位于应用配置目录的 `browsers/<id>.json`。每个浏览器单独保存，字段属于当前机器。删除对应文件可恢复自动检测。

```json
{
  "executable": "/usr/bin/google-chrome",
  "dataDirectory": "/home/alice/.config/google-chrome",
  "profile": "Profile 1"
}
```

## 开发

参考 V WebView 示例的层次切分。静态界面在 `frontend/`，配置解析和系统发现位于 `internal/manager/`，桌面通过 Wails 本机绑定调用 Go，CLI 复用同一套 Go 逻辑。Windows、macOS、Linux 的发现实现用 Go 平台文件分开。桌面与 CLI 直接访问应用配置目录，无需启动本地服务。

从源码运行 CLI 时使用 `go run ./cmd/extension-manager-cli browser list`，此入口不依赖图形桌面和 WebView。
