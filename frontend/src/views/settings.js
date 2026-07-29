// ============================================================
// views/settings.js —— 设置页（前缀 .set-）
// 左锚点目录 + 右分区卡片：本地路径 / 设备信息 / 软件更新 /
// 同步偏好 / 架构说明 / 备份与恢复。
// 页面只在挂载时整体构建一次（保护表单焦点）；
// store 变化时仅局部刷新只读 kv（数据目录/设备/云端目录状态）。
// ============================================================

const SECTIONS = [
  { key: "paths", label: "本地路径", icon: "folder" },
  { key: "device", label: "设备信息", icon: "monitor" },
  { key: "update", label: "软件更新", icon: "rocket" },
  { key: "prefs", label: "同步偏好", icon: "sliders" },
  { key: "arch", label: "架构说明", icon: "database" },
  { key: "backup", label: "备份与恢复", icon: "archive" },
];

export function isUpdateAvailable(result) {
  return result?.status === "available";
}

export function mount(root, ctx) {
  const { store, api, ui } = ctx;
  const { h, icon, iconEl, fmtTime, toast, skeleton } = ui;

  let disposed = false;
  const errMsg = (e) => e?.message || String(e || "未知错误");

  /* 异步按钮防重复：busy 期间禁点，结束后恢复 */
  async function withBusy(btn, fn) {
    if (btn.classList.contains("busy")) return;
    btn.classList.add("busy");
    btn.disabled = true;
    try {
      await fn();
    } finally {
      btn.classList.remove("busy");
      btn.disabled = false;
    }
  }

  function kvRow(label, valueNode, mono = false) {
    const v = h("span", { class: `kv-v${mono ? " mono" : ""}` });
    if (valueNode != null) v.append(valueNode.nodeType ? valueNode : document.createTextNode(String(valueNode)));
    return { row: h("div", { class: "kv" }, h("span", { class: "kv-k" }, label), v), value: v };
  }

  function section(key, title, ...content) {
    return h(
      "section",
      { class: "set-sec", id: `set-sec-${key}` },
      h("h2", { class: "set-sec-title" }, title),
      h("div", { class: "card set-card" }, ...content),
    );
  }

  /* ---------------- 1. 本地路径 ---------------- */

  const dataDirKv = kvRow("数据目录", store.select.dataDir() || "—", true);

  const openDirBtn = h(
    "button",
    {
      class: "btn",
      onClick: () => {
        const dir = store.select.dataDir();
        if (!dir) {
          toast("数据目录未知", "warn");
          return;
        }
        Promise.resolve(api.OpenPath(dir)).catch((e) => toast(`打开失败：${errMsg(e)}`, "err"));
      },
    },
    iconEl("folderOpen"),
    "打开数据目录",
  );

  const secPaths = section(
    "paths",
    "本地路径",
    h("div", { class: "set-kvs" }, dataDirKv.row),
    h("div", { class: "set-actions" }, openDirBtn),
  );

  /* ---------------- 2. 设备信息 ---------------- */

  const devNameKv = kvRow("设备名", "—");
  const devPlatKv = kvRow("平台", "—");
  const devIdKv = kvRow("设备 ID", "—", true);
  const devStartKv = kvRow("最近启动", "—");

  const secDevice = section(
    "device",
    "设备信息",
    h("div", { class: "set-kvs" }, devNameKv.row, devPlatKv.row, devIdKv.row, devStartKv.row),
  );

  /* ---------------- 3. 软件更新 ---------------- */

  const verKv = kvRow("当前版本", skeleton("set-skel-line"));
  const chanKv = kvRow("更新渠道", skeleton("set-skel-line"));
  const buildKv = kvRow("构建时间", skeleton("set-skel-line"));

  const updateResult = h("div", { class: "set-update-result" });

  function renderUpdateAvailable(res) {
    updateResult.innerHTML = "";
    const dlBtn = h(
      "button",
      {
        class: "btn btn-primary",
        onClick: () =>
          withBusy(dlBtn, async () => {
            try {
              const dl = await api.DownloadUpdate({ version: res.latestVersion, asset: res.asset });
              const yes = await ui.confirm({
                message: "下载完成，立即重启安装？",
                confirmText: "重启安装",
                cancelText: "稍后再说",
              });
              if (yes) await api.ApplyUpdateAndRestart(dl);
            } catch (e) {
              toast(`更新失败：${errMsg(e)}`, "err");
            }
          }),
      },
      iconEl("download"),
      "下载并安装",
    );
    const notes = String(res.notes || "")
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean);
    updateResult.append(
      h(
        "div",
        { class: "set-update-avail" },
        h(
          "div",
          { class: "set-update-avail-head" },
          h("span", { class: "set-update-glyph", html: icon("sparkles") }),
          h(
            "div",
            { class: "set-update-avail-main" },
            h("div", { class: "set-update-ver" }, `发现新版本 ${res.latestVersion || ""}`),
            res.publishedAt ? h("div", { class: "set-update-date" }, `发布于 ${fmtTime(res.publishedAt)}`) : null,
          ),
        ),
        notes.length
          ? h("div", { class: "set-update-notes" }, notes.map((line) => h("div", { class: "set-update-note" }, line)))
          : null,
        h("div", { class: "set-update-dl" }, dlBtn),
      ),
    );
  }

  const checkBtn = h(
    "button",
    {
      class: "btn",
      onClick: () =>
        withBusy(checkBtn, async () => {
          try {
            const res = await api.CheckForUpdates();
            if (disposed) return;
            if (isUpdateAvailable(res)) {
              renderUpdateAvailable(res);
            } else {
              updateResult.innerHTML = "";
              toast(res?.message || "当前已是最新版本", "info");
            }
          } catch (e) {
            toast(`检查更新失败：${errMsg(e)}`, "err");
          }
        }),
    },
    iconEl("refresh"),
    "检查更新",
  );

  const secUpdate = section(
    "update",
    "软件更新",
    h("div", { class: "set-kvs" }, verKv.row, chanKv.row, buildKv.row),
    h("div", { class: "set-actions" }, checkBtn),
    updateResult,
  );

  Promise.resolve(api.GetAppInfo())
    .then((info) => {
      if (disposed) return;
      verKv.value.replaceChildren(document.createTextNode(info?.version || "未知"));
      chanKv.value.replaceChildren(document.createTextNode(info?.updateChannel || "stable"));
      const t = fmtTime(info?.buildDate);
      buildKv.value.replaceChildren(document.createTextNode(t === "从未" && info?.buildDate ? info.buildDate : t));
    })
    .catch(() => {
      if (disposed) return;
      verKv.value.replaceChildren(document.createTextNode("未知"));
      chanKv.value.replaceChildren(document.createTextNode("未知"));
      buildKv.value.replaceChildren(document.createTextNode("未知"));
    });

  /* ---------------- 4. 同步偏好（只建一次，防焦点丢失） ---------------- */

  const prefsInit = store.select.preferences();

  const autoSyncChk = h("input", { type: "checkbox", checked: Boolean(prefsInit.autoSyncOnLaunch) });

  function selectField(label, options, current, help) {
    const sel = h(
      "select",
      { class: "input" },
      options.map(([value, text]) => h("option", { value, selected: value === current }, text)),
    );
    const field = h(
      "div",
      { class: "field" },
      h("span", { class: "field-label" }, label),
      sel,
      help ? h("span", { class: "field-help" }, help) : null,
    );
    return { field, sel };
  }

  function secretField(label, value, help) {
    const input = h("input", {
      class: "input",
      type: "password",
      value: value || "",
      placeholder: "未设置",
      autocomplete: "off",
      spellcheck: "false",
    });
    const eyeBtn = h("button", {
      class: "set-eye",
      type: "button",
      "aria-label": "显示或隐藏密钥",
      html: icon("eye"),
      onClick: () => {
        const show = input.type === "password";
        input.type = show ? "text" : "password";
        eyeBtn.innerHTML = icon(show ? "eyeOff" : "eye");
      },
    });
    const field = h(
      "div",
      { class: "field" },
      h("span", { class: "field-label" }, label),
      h("div", { class: "set-secret" }, input, eyeBtn),
      help ? h("span", { class: "field-help" }, help) : null,
    );
    return { field, input };
  }

  const modeSelect = selectField(
    "启动同步模式",
    [
      ["smart", "智能判断"],
      ["local-first", "优先本地"],
      ["cloud-first", "优先云端"],
    ],
    prefsInit.startupSyncMode || "smart",
  );

  const policySelect = selectField(
    "冲突策略",
    [
      ["manual", "手动处理"],
      ["local", "优先本地"],
      ["remote", "优先云端"],
    ],
    prefsInit.conflictPolicy || "manual",
  );

  const backgroundSyncSelect = selectField(
    "后台检查间隔",
    [
      ["0", "关闭"],
      ["30", "30 秒"],
      ["60", "60 秒"],
      ["300", "5 分钟"],
    ],
    String(prefsInit.backgroundSyncIntervalSeconds ?? 60),
  );

  const rawgField = secretField("RAWG API Key", prefsInit.rawgApiKey, "用于在详情页搜索游戏资料与封面");
  const sgdbField = secretField("SteamGridDB API Key", prefsInit.steamGridDbApiKey, "用于拉取更高质量的游戏封面");

  const savePrefsBtn = h(
    "button",
    {
      class: "btn btn-primary",
      onClick: () =>
        withBusy(savePrefsBtn, async () => {
          try {
            await store.actions.savePreferences({
              autoSyncOnLaunch: autoSyncChk.checked,
              startupSyncMode: modeSelect.sel.value,
              conflictPolicy: policySelect.sel.value,
              backgroundSyncIntervalSeconds: Number(backgroundSyncSelect.sel.value),
              rawgApiKey: rawgField.input.value.trim(),
              steamGridDbApiKey: sgdbField.input.value.trim(),
            });
            toast("偏好已保存", "ok");
          } catch (e) {
            toast(`保存失败：${errMsg(e)}`, "err");
          }
        }),
    },
    iconEl("check"),
    "保存偏好",
  );

  const secPrefs = section(
    "prefs",
    "同步偏好",
    h(
      "label",
      { class: "check set-form-check" },
      autoSyncChk,
      h("span", {}, "启动游戏前自动同步存档"),
    ),
    h("div", { class: "set-form-grid" }, modeSelect.field, policySelect.field, backgroundSyncSelect.field),
    rawgField.field,
    sgdbField.field,
    h("div", { class: "set-actions" }, savePrefsBtn),
  );

  /* ---------------- 5. 架构说明 ---------------- */

  function archRow(iconName, name, desc) {
    return h(
      "div",
      { class: "set-arch-row" },
      h("span", { class: "set-arch-glyph", html: icon(iconName) }),
      h(
        "div",
        { class: "set-arch-main" },
        h("div", { class: "set-arch-name" }, name),
        h("div", { class: "set-arch-desc" }, desc),
      ),
    );
  }

  const catalogKv = kvRow("云端目录", "—");
  const catalogSyncKv = kvRow("目录最近同步", "—");

  const secArch = section(
    "arch",
    "架构说明",
    archRow("database", "主账号 D1 · 目录索引中心", "游戏目录、账号与偏好的索引数据集中存放在主账号的 D1 数据库，多设备由此对齐。"),
    archRow("hardDrive", "R2 · 存档对象存储", "存档备份以对象形式写入 R2 存储桶；副账号提供额外的 R2 存储池。"),
    archRow("folder", "WebDAV 模式", "主账号选择 WebDAV 时，目录索引与存档对象都存放在 DAV 服务器的指定目录（catalog / manifests / objects），Nextcloud、坚果云、NAS 皆可接入。"),
    archRow("cloud", "多账号分摊额度", "配置多个 Cloudflare 账号即可分摊各自的免费额度，存档越多越推荐。"),
    h("div", { class: "set-kvs set-arch-kvs" }, catalogKv.row, catalogSyncKv.row),
  );

  /* ---------------- 6. 备份与恢复 ---------------- */

  const exportBtn = h(
    "button",
    {
      class: "btn",
      onClick: () =>
        withBusy(exportBtn, async () => {
          try {
            await api.ExportAppBackup();
            toast("已导出配置备份", "ok");
          } catch (e) {
            toast(`导出失败：${errMsg(e)}`, "err");
          }
        }),
    },
    iconEl("upload"),
    "导出配置备份",
  );

  const importBtn = h(
    "button",
    {
      class: "btn",
      onClick: async () => {
        if (importBtn.classList.contains("busy")) return;
        const yes = await ui.confirm({
          message: "从备份恢复将覆盖当前的账号、游戏目录与偏好设置。\n确定继续吗？",
          confirmText: "继续恢复",
          cancelText: "取消",
          tone: "warn",
        });
        if (!yes) return;
        await withBusy(importBtn, async () => {
          try {
            await api.ImportAppBackup();
            await store.actions.boot();
            toast("恢复完成", "ok");
          } catch (e) {
            toast(`恢复失败：${errMsg(e)}`, "err");
          }
        });
      },
    },
    iconEl("history"),
    "从备份恢复",
  );

  const secBackup = section(
    "backup",
    "备份与恢复",
    h(
      "p",
      { class: "set-note" },
      "配置备份包含云账号、游戏目录与偏好设置（不含存档文件本体），可用于迁移到新设备或灾难恢复。",
    ),
    h("div", { class: "set-actions" }, exportBtn, importBtn),
  );

  /* ---------------- 目录导航 + 组装 ---------------- */

  const navItems = SECTIONS.map((s, i) =>
    h(
      "button",
      {
        class: `set-nav-item${i === 0 ? " active" : ""}`,
        onClick: (e) => {
          const btn = e.currentTarget;
          btn.parentElement.querySelectorAll(".set-nav-item").forEach((el) => el.classList.remove("active"));
          btn.classList.add("active");
          // #view 是滚动容器（非 window），scrollIntoView 会滚动最近的可滚祖先
          root.querySelector(`#set-sec-${s.key}`)?.scrollIntoView({ behavior: "smooth", block: "start" });
        },
      },
      iconEl(s.icon),
      s.label,
    ),
  );

  const page = h(
    "div",
    { class: "page set-page" },
    h(
      "header",
      { class: "set-head" },
      h("h1", { class: "set-title" }, "设置"),
      h("p", { class: "set-sub" }, "本地环境、软件更新与同步偏好"),
    ),
    h(
      "div",
      { class: "set-layout" },
      h("nav", { class: "set-nav" }, navItems),
      h("main", { class: "set-main stagger" }, secPaths, secDevice, secUpdate, secPrefs, secArch, secBackup),
    ),
  );

  root.innerHTML = "";
  root.append(page);

  /* ---------------- 动态 kv 局部刷新（不触碰表单区） ---------------- */

  function refreshDynamic() {
    dataDirKv.value.textContent = store.select.dataDir() || "—";

    const dev = store.select.device();
    devNameKv.value.textContent = dev.name || "—";
    devPlatKv.value.textContent = dev.platform || "—";
    devIdKv.value.textContent = dev.id || "—";
    devStartKv.value.textContent = fmtTime(dev.lastStartedAt);

    const rec = store.select.recoveryStatus();
    catalogKv.value.replaceChildren(
      rec.remoteCatalogAvailable
        ? h("span", { class: "badge ok" }, "云端目录可用")
        : h("span", { class: "badge warn" }, "云端目录不可用"),
    );
    catalogSyncKv.value.textContent = fmtTime(store.select.catalogSync().lastSuccessAt);
  }

  refreshDynamic();
  const off = store.subscribe(refreshDynamic);

  return () => {
    disposed = true;
    off();
  };
}
