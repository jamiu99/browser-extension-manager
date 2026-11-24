"use strict";

const $ = (id) => document.getElementById(id);
const api = () => window.go.main.App;
const state = {
  items: [], browsers: [], checked: new Set(), targets: new Set(),
  focused: "", results: new Map(), busy: false, guide: "", version: "",
  automaticReload: false,
};

const labels = {
  available: "有更新", current: "已是最新", updated: "已更新", manual: "手动维护",
  local: "手动维护", error: "检查失败",
};

function esc(value) {
  return String(value ?? "").replace(/[&<>"']/g, (char) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char]);
}

function names(ids) {
  return ids.map((id) => state.browsers.find((browser) => browser.id === id)?.name ?? id).join("、");
}

function schedule(message) {
  $("activity").textContent = message;
}

function selected() {
  const wanted = state.checked.size ? state.checked : new Set([state.focused]);
  return state.items.filter((item) => wanted.has(item.path) && item.version);
}

function focused() {
  return state.items.find((item) => item.path === state.focused);
}

function draw() {
  const items = state.items;
  $("count").textContent = items.length + " 个插件" + (state.version ? " · " + state.version : "");
  document.querySelector(".library").setAttribute("aria-busy", state.busy);
  const term = $("search").value.trim().toLowerCase();
  const rows = items.filter((item) => (item.name + " " + names(item.browsers)).toLowerCase().includes(term));
  $("entries").innerHTML = rows.length ? rows.map((item) => {
    const result = state.results.get(item.path);
    const status = result?.state ?? (item.source ? "pending" : "manual");
    const label = labels[status] ?? (item.sourceKind === "manifest" ? "待检查" : "可手动更新");
    const icon = item.icon
      ? '<img class="logo" src="' + esc(item.icon) + '" alt="">'
      : '<span class="logo" aria-hidden="true">' + esc(Array.from(item.name)[0]) + "</span>";
    return '<div class="item' + (item.path === state.focused ? " active" : "") + '" tabindex="0" data-path="' +
      esc(item.path) + '"><div class="identity"><input type="checkbox" aria-label="选择 ' + esc(item.name) + '"' +
      (state.checked.has(item.path) ? " checked" : "") + ">" + icon + "<div><strong>" + esc(item.name) +
      "</strong><small>" + (item.version ? "版本 " + esc(item.version) : "目录不可用") +
      '</small></div></div><small class="loaded">' + esc(names(item.browsers) || "待加载") +
      '</small><span class="badge ' + esc(status) + '" title="' + esc(result?.error || "") + '">' + label + "</span></div>";
  }).join("") : '<div class="empty">' + (term ? "没有匹配的插件。" : "点击「添加插件」选取目录或更新源。") + "</div>";
  drawDetail();
  const controls = ["check", "import", "all", "add", "browser-settings", "software-update"];
  controls.forEach((id) => { $(id).disabled = state.busy; });
  $("check").disabled ||= !items.some((item) => item.sourceKind === "manifest");
  $("all").disabled ||= !items.some((item) => item.source && item.version);
}

function drawDetail() {
  const item = focused();
  if (!item) {
    $("detail").innerHTML = '<p class="empty">选择一个插件查看安装目录和更新来源。</p>';
    return;
  }
  const result = state.results.get(item.path);
  const browserOptions = state.browsers.map((browser) =>
    '<label class="choice"><input type="checkbox" data-browser="' + esc(browser.id) + '"' +
    (state.targets.has(browser.id) ? " checked" : "") + ">" +
    esc(browser.instance ? browser.name + " · " + browser.instance : browser.name) + "</label>").join("");
  const operation = (id, label, kind, disabled = false) =>
    '<button id="' + id + '" class="' + kind + '"' + (state.busy || disabled ? " disabled" : "") + ">" + label + "</button>";
  const picks = selected();
  $("detail").innerHTML = "<h2>" + esc(item.name) + "</h2><p>" +
    esc([item.version ? "版本 " + item.version : "目录不可用", names(item.browsers)].filter(Boolean).join(" · ")) + "</p>" +
    (result?.version && result.state !== "updated" ? "<p>发布版本 " + esc(result.version) + "</p>" : "") +
    "<dl><dt>更新来源</dt><dd>" + esc(item.source || "尚未设置") + "</dd>" +
    "<dt>安装目录</dt><dd>" + esc(item.path) + "</dd></dl><h3>目标浏览器</h3>" +
    '<div class="browsers">' + (browserOptions || '<p>请在「浏览器设置」中确认浏览器路径。</p>') + "</div>" +
    '<div class="actions">' +
    operation("update", "更新所选" + (picks.length > 1 ? "（" + picks.length + "）" : ""), "primary", !picks.some((pick) => pick.source)) +
    operation("install", "安装到浏览器", "", !item.version || state.targets.size === 0) + "<div>" +
    operation("reload", "重载所选", "quiet", !picks.length || state.targets.size === 0) +
    operation("edit-source", "修改来源", "quiet", !item.version) +
    operation("copy-path", "复制目录", "quiet") + "</div></div>";
}

async function load(importInstalled = false) {
  const catalog = await api().Catalog(importInstalled);
  const firstLoad = state.browsers.length === 0;
  state.items = catalog.extensions ?? [];
  state.browsers = catalog.browsers ?? [];
  for (const target of state.targets) {
    if (!state.browsers.some((browser) => browser.id === target)) state.targets.delete(target);
  }
  state.version = catalog.version;
  state.automaticReload = catalog.automaticReload;
  if (firstLoad && state.browsers.length) {
    state.targets.add(state.browsers[0].id);
  }
  if (!focused()) {
    state.focused = state.items[0]?.path ?? "";
  }
  draw();
}

async function run(message, action) {
  if (state.busy) return;
  state.busy = true;
  schedule(message);
  draw();
  try {
    await action();
  } catch (error) {
    schedule(String(error?.message ?? error));
  } finally {
    state.busy = false;
    draw();
  }
}

async function check(items, apply) {
  return run(apply ? "正在更新插件文件…" : "正在检查更新…", async () => {
    const results = await api().Check(items.map((item) => item.path), apply);
    results.forEach((result) => state.results.set(result.path, result));
    const summary = results.map((result) => {
      const item = items.find((candidate) => candidate.path === result.path);
      return item.name + " · " + (result.error || labels[result.state]) + (result.version ? " " + result.version : "");
    });
    if (apply) {
      await load();
      const ready = results.filter((item) => item.state === "updated" ||
        (state.automaticReload && item.state === "current")).map((item) => item.path);
      if (ready.length && state.targets.size) {
        schedule(summary.concat("正在让所选浏览器重新加载插件…").join("\n"));
        const messages = await api().Activate([...state.targets], ready, false);
        summary.push(...messages);
      }
    }
    schedule(summary.join("\n") || "当前没有可检查的更新源。");
  });
}

function edit(item) {
  $("edit-title").textContent = item ? "修改来源" : "添加插件";
  $("path").readOnly = Boolean(item);
  $("folder").hidden = Boolean(item);
  $("path").value = item?.path ?? "";
  $("source").value = item?.source ?? "";
  $("manifest").checked = item?.sourceKind === "manifest";
  $("edit-error").textContent = "";
  $("edit").showModal();
  $(item ? "source" : "path").focus();
}

function focusRow(event) {
  const row = event.target.closest("[data-path]");
  if (!row) return;
  state.focused = row.dataset.path;
  if (event.target.type === "checkbox") {
    if (event.target.checked) state.checked.add(state.focused);
    else state.checked.delete(state.focused);
  }
  draw();
  const updated = Array.from($("entries").children).find((node) => node.dataset.path === state.focused);
  if (event.target.type === "checkbox") updated?.querySelector("input").focus();
  else updated?.focus();
}

$("entries").addEventListener("click", focusRow);
$("entries").addEventListener("keydown", (event) => {
  if (event.target.matches(".item") && (event.key === "Enter" || event.key === " ")) {
    event.preventDefault();
    focusRow(event);
  }
});
$("search").addEventListener("input", draw);
$("add").addEventListener("click", () => edit(null));
$("import").addEventListener("click", () => run("正在扫描浏览器…", async () => {
  await load(true);
  schedule("已刷新浏览器加载记录。");
}));
$("check").addEventListener("click", () => check(state.items.filter((item) => item.sourceKind === "manifest"), false));
$("all").addEventListener("click", () => check(state.items.filter((item) => item.source && item.version), true));
$("detail").addEventListener("change", (event) => {
  const id = event.target.dataset.browser;
  if (!id) return;
  if (event.target.checked) state.targets.add(id);
  else state.targets.delete(id);
  drawDetail();
  $("detail").querySelector('[data-browser="' + id + '"]')?.focus();
});
$("detail").addEventListener("click", (event) => {
  const item = focused();
  const id = event.target.id;
  if (id === "edit-source") edit(item);
  if (id === "update") check(selected().filter((entry) => entry.source), true);
  if (id === "copy-path") run("正在复制目录…", async () => {
    await api().Copy(item.path);
    schedule("已复制插件目录。");
  });
  if (id === "install" || id === "reload") {
    const install = id === "install";
    run(install ? "正在打开浏览器扩展页…" : "正在让所选浏览器重新加载插件…", async () => {
      const paths = install ? [item.path] : selected().map((entry) => entry.path);
      const messages = await api().Activate([...state.targets], paths, install);
      schedule(messages.join("\n"));
    });
  }
});

for (const button of document.querySelectorAll("[data-close]")) {
  button.addEventListener("click", () => $(button.dataset.close).close());
}
async function pick(field, action) {
  try {
    const value = await action();
    if (value) $(field).value = value;
    if (value && field === "source") $("manifest").checked = false;
  } catch (error) {
    $("edit-error").textContent = String(error?.message ?? error);
  }
}
$("folder").addEventListener("click", () => pick("path", () => api().PickFolder()));
$("zip").addEventListener("click", () => pick("source", () => api().PickZIP()));
$("source-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  if (state.busy) return;
  state.busy = true;
  draw();
  $("save").disabled = true;
  $("edit-error").textContent = "正在验证插件和更新源…";
  try {
    const item = await api().Save($("path").value, $("source").value, $("manifest").checked ? "manifest" : "");
    state.results.delete(item.path);
    state.focused = item.path;
    await load();
    $("edit").close();
    schedule("已保存插件。");
  } catch (error) {
    $("edit-error").textContent = String(error?.message ?? error);
  } finally {
    $("save").disabled = false;
    state.busy = false;
    draw();
  }
});

function showGuide(markdown) {
  const code = String.fromCharCode(96).repeat(3);
  const json = markdown.split(code + "json\n")[1]?.split(code)[0]?.trim() ?? "";
  const prompt = markdown.split(code + "text\n")[1]?.split(code)[0]?.trim() ?? "";
  $("guide-content").innerHTML = "<p>填入稳定的 HTTPS 清单地址。每次发版先上传 ZIP，再更新清单。</p>" +
    "<h3>版本清单示例</h3><pre>" + esc(json) + "</pre><h3>给 AI 的提示词</h3><pre>" + esc(prompt) + "</pre>";
  return prompt;
}

$("guide").addEventListener("click", async () => {
  try {
    state.guide ||= await api().Guide();
    state.prompt = showGuide(state.guide);
    $("guide-dialog").showModal();
  } catch (error) {
    schedule(String(error?.message ?? error));
  }
});
for (const [id, key] of [["copy-guide", "guide"], ["copy-prompt", "prompt"]]) {
  $(id).addEventListener("click", async () => {
    try {
      await api().Copy(state[key]);
      $("guide-dialog").close();
      schedule("已复制到剪贴板。");
    } catch (error) {
      schedule(String(error?.message ?? error));
    }
  });
}

(async () => {
  await run("正在读取插件…", async () => {
    await load();
    schedule("已读取本机插件。");
  });
  const feeds = state.items.filter((item) => item.sourceKind === "manifest");
  if (feeds.length) await check(feeds, false);
})();
