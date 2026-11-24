# 浏览器插件管理器

集中管理 Edge、Chrome 和 Chromium 中通过目录加载的扩展。导入本机插件、管理更新源、原地更新 ZIP，并从桌面应用打开多个浏览器的扩展页。

## 下载

从 [GitHub Releases](https://github.com/jamiu99/browser-extension-manager/releases/latest) 选择适合的平台。先解压，再打开应用。

| 平台 | 下载包 | 运行要求 |
| --- | --- | --- |
| Windows x64 | windows-x64.zip | Windows 10/11 和 WebView2 |
| macOS Apple Silicon / Intel | macos-universal.zip | 系统 WKWebView。应用包未做 Apple 公证，系统可能要求在「隐私与安全性」中确认打开 |
| Linux x64 | linux-x64.tar.gz | GTK 3 和 WebKitGTK 4.1。Debian/Ubuntu 安装 libgtk-3-0（或 libgtk-3-0t64）及 libwebkit2gtk-4.1-0 |

Windows 版会读取先前 Harness 管理器保存的插件目录和更新源。你可以直接在新版中检查更新。

## 使用

1. 点击「导入已安装」，或选择插件目录、ZIP 文件、HTTPS ZIP 或 JSON 更新源。
2. 勾选浏览器后点击「安装到浏览器」。管理器会复制目录并打开扩展页。启用开发者模式，在浏览器中确认加载目录。
3. 点击「检查更新」，再选择插件和浏览器并点击「更新所选」。更新保持原加载目录。Windows 管理器会用临时扩展页在已安装的浏览器配置中重载并核对版本，结果显示在底部。「重载所选」可以单独重新加载当前文件。macOS 和 Linux 打开扩展页，用户在扩展卡片上确认重载。

[浏览器设置与 AI 配置](docs/browser-configuration.md) 介绍自动检测、手动路径和用户资料选择。桌面设置中可复制 AI 配置指令。发布包内的 `extension-manager-cli` 提供 JSON 格式的发现、验证和设置命令。

[更新源格式和 AI 提示词](docs/update-feed.md) 也可以在应用里直接复制。

## 开发与发版

Go 版本见 [.go-version](.go-version)。Wails 版本由 [go.mod](go.mod) 固定。前端使用原生 HTML、CSS 和 JavaScript，Go 负责本机配置、扩展包和浏览器操作。桌面窗口使用各平台的系统 WebView。

- 本地检查运行 `./verify.sh`，使用 Go 和 Node.js。
- 在对应平台运行 `./scripts/package.sh windows/amd64 windows-x64`、`./scripts/package.sh darwin/universal macos-universal` 或 `./scripts/package.sh linux/amd64 linux-x64 webkit2_41`。Linux 构建需 libgtk-3-dev 和 libwebkit2gtk-4.1-dev，macOS 需 Xcode Command Line Tools。
- `./start.sh` 启动已构建的本机应用。
- 合并通过 `verify` 的 PR 后，给 main 的提交打 `v*` 标签触发三平台打包。所有资产齐备后自动发布，附 SHA-256。
