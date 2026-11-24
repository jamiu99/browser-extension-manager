"use strict";

(() => {
  const dialog = $("software-update-dialog");
  const check = $("software-check");
  const install = $("software-install");
  const feedback = $("software-feedback");
  const close = dialog.querySelector(".close");
  let selected = "";
  let busy = false;

  async function checkUpdate() {
    if (busy) return;
    selected = "";
    install.hidden = true;
    busy = true;
    check.disabled = true;
    feedback.textContent = "正在检查 GitHub 发布…";
    try {
      const update = await api().CheckSoftwareUpdate();
      $("software-current").textContent = update.current;
      $("software-latest").textContent = update.latest;
      const messages = {
        current: "你正在使用最新正式版。",
        ahead: "当前版本比 GitHub 的最新正式版更新。",
        development: "当前为未标注版本的开发构建。可在发布页下载正式版。",
        manual: "当前平台暂无可直接安装的更新包，请查看 GitHub 发布。",
      };
      if (update.state === "available") {
        selected = update.latest;
        const mb = (update.size / 1024 / 1024).toFixed(1);
        feedback.textContent = "可以更新到 " + update.latest + "。下载约 " + mb + " MB，安装后应用会重启。";
        install.hidden = false;
      } else {
        feedback.textContent = messages[update.state] || "暂时没有可用的软件更新。";
      }
    } catch (error) {
      feedback.textContent = String(error?.message ?? error);
    } finally {
      busy = false;
      check.disabled = false;
    }
  }

  $("software-update").addEventListener("click", () => {
    dialog.showModal();
    checkUpdate();
  });
  check.addEventListener("click", checkUpdate);
  $("software-releases").addEventListener("click", () => api().OpenSoftwareReleases());
  dialog.addEventListener("cancel", (event) => {
    if (close.disabled) event.preventDefault();
  });
  install.addEventListener("click", async () => {
    if (!selected || busy) return;
    busy = true;
    install.disabled = check.disabled = close.disabled = true;
    feedback.textContent = "正在下载并校验更新。完成后应用会重启…";
    try {
      await api().InstallSoftwareUpdate(selected);
    } catch (error) {
      feedback.textContent = String(error?.message ?? error);
      busy = false;
      install.disabled = check.disabled = close.disabled = false;
    }
  });

  (async () => {
    try {
      const info = await api().SoftwareVersion();
      $("software-current").textContent = info.version;
      if (info.notice) {
        if (!dialog.open) dialog.showModal();
        feedback.textContent = info.notice;
      }
    } catch {
      // The main application remains usable if the update bridge is unavailable.
    }
  })();
})();
