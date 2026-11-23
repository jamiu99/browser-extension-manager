# 浏览器插件更新源

用稳定的 HTTPS 地址提供 UTF-8 JSON 版本清单。推荐命名为 latest.json，并设置 Cache-Control: no-cache 或 no-store。

```json
{
  "version": "1.2.3",
  "file": "my-extension-1.2.3.zip"
}
```

- version 和 ZIP 内 manifest.json 的 version 一致。使用 1 至 4 段数字，每段 0 至 65535，无前导零，且至少一段大于零。
- file 是同目录 ZIP 文件名，可用字母、数字、点、连字符和下划线。也可用 download_url 提供同源的绝对 HTTPS 下载地址。
- sha256 可选，用 64 个十六进制字符填写 ZIP 的 SHA-256。
- ZIP 放置可直接加载的扩展文件，建议让 manifest.json 位于根目录。保留原扩展名称和已有的 key。发布时先上传版本化 ZIP，再替换清单。

为已加载的插件点「修改来源」，填写地址保存。以 .json 结尾的地址会自动识别，其他清单地址勾选「来源是 JSON 版本清单」。新插件可在「添加插件」中直接填写地址。已有条目也可以通过命令行保存来源。

```bash
extension-manager --set-source '插件名称或绝对目录' 'https://example.com/extensions/latest.json'
```

## 给 AI 的提示词

```text
请检查这个浏览器插件的 manifest.json、构建产物和发版文档，按 https://github.com/jamiu99/browser-extension-manager/blob/main/docs/update-feed.md 接入浏览器插件管理器。优先复用项目已有的清单和 ZIP。需要新建时，准备稳定 HTTPS 地址 latest.json 和带版本号的 ZIP。清单填写 version 和同目录 ZIP 的文件名 file，建议附上 sha256。确保包内 manifest.json 版本一致，保留扩展名称和已有的 key。发布按项目现有流程和授权执行。

清单上线后，告诉我实际地址。如果插件已在列表中且我授权登记，用 extension-manager --set-source '插件名称或绝对目录' '实际的 HTTPS 清单地址' 保存。让我在管理器里检查并确认是否应用更新。
```
