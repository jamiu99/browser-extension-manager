"use strict";

(() => {
  const dialog = $("browser-settings-dialog");
  const browser = $("setting-browser");
  const executable = $("setting-executable");
  const data = $("setting-data");
  const profile = $("setting-profile");
  const feedback = $("setting-feedback");
  let reports = [];
  let busy = false;

  function fields() {
    return { executable: executable.value, dataDirectory: data.value, profile: profile.value };
  }

  function showStatus(status) {
    const label = $("setting-status");
    label.textContent = status.problem ? "需要检查设置" : status.ready ? "可用" : "未找到浏览器";
    label.classList.toggle("attention", Boolean(status.problem));
    const profiles = Array.from(new Set([...status.profiles, status.config.profile].filter(Boolean)));
    profile.innerHTML = '<option value="">自动选择资料</option>' + profiles.map((name) =>
      '<option value="' + esc(name) + '">' + esc(name) + '</option>').join("");
    profile.value = status.config.profile;
  }

  function show(status) {
    executable.value = status.config.executable;
    data.value = status.config.dataDirectory;
    executable.placeholder = status.detected.executable || "输入浏览器程序的绝对路径";
    data.placeholder = status.detected.dataDirectory || "输入用户数据目录的绝对路径";
    showStatus(status);
    feedback.textContent = status.problem || "";
  }

  async function redetect() {
    const id = browser.value;
    const report = await api().BrowserSettings();
    reports = report.browsers;
    browser.innerHTML = reports.map((item) =>
      '<option value="' + esc(item.id) + '">' + esc(item.name) + '</option>').join("");
    if (reports.some((item) => item.id === id)) browser.value = id;
    show(reports.find((item) => item.id === browser.value));
  }

  async function runSettings(action) {
    if (busy) return;
    busy = true;
    const controls = [...dialog.querySelectorAll("button:not(.close), input, select")];
    controls.forEach((control) => { control.disabled = true; });
    feedback.textContent = "";
    try {
      await action();
    } catch (error) {
      feedback.textContent = String(error?.message ?? error);
    } finally {
      controls.forEach((control) => { control.disabled = false; });
      busy = false;
    }
  }

  $("browser-settings").addEventListener("click", () => {
    dialog.showModal();
    runSettings(redetect);
  });
  browser.addEventListener("change", () => show(reports.find((item) => item.id === browser.value)));

  for (const [id, input, kind] of [["pick-executable", executable, "exe"], ["pick-data", data, "data"]]) {
    $(id).addEventListener("click", () => runSettings(async () => {
      const path = await api().PickBrowserPath(kind);
      if (path) input.value = path;
      if (path && kind === "data") profile.value = "";
    }));
  }

  $("browser-redetect").addEventListener("click", () => runSettings(async () => {
    await redetect();
    await load();
    if (!feedback.textContent) feedback.textContent = "已重新检测本机浏览器。";
  }));
  $("browser-check").addEventListener("click", () => runSettings(async () => {
    const status = await api().CheckBrowser(browser.value, fields());
    showStatus(status);
    feedback.textContent = status.problem || (status.ready
      ? "路径有效，可以保存。"
      : "请先选择有效的浏览器程序。");
  }));
  $("copy-browser-prompt").addEventListener("click", () => runSettings(async () => {
    await api().Copy(await api().BrowserPrompt());
    feedback.textContent = "已复制。把指令交给能访问本机的 AI Agent。";
  }));

  async function save(config) {
    const status = await api().SaveBrowser(browser.value, config);
    reports = reports.map((item) => item.id === status.id ? status : item);
    show(status);
    await load();
    feedback.textContent = status.ready ? "已保存并刷新插件列表。" : "已保存设置。找到浏览器程序后可以使用。";
  }

  $("browser-reset").addEventListener("click", () => runSettings(() =>
    save({ executable: "", dataDirectory: "", profile: "" })));
  $("browser-settings-form").addEventListener("submit", (event) => {
    event.preventDefault();
    runSettings(() => save(fields()));
  });
})();
