// ============================================================
// views/game.js —— 游戏详情 / 编辑页（整页路由，前缀 .gd-）
// 模式：params.id 有值 = 编辑既有游戏；无值 = 新建（添加游戏）。
// 表单区只在挂载时构建一次（保护输入焦点），store 变化仅重建头部横幅。
// ============================================================

const errMsg = (e) => e?.message || String(e || "未知错误");
const evId = (p) => (!p ? "" : typeof p === "string" ? p : p.gameId || p.id || "");
const dedupe = (arr) => [...new Set((arr || []).filter(Boolean))];

// 从游戏/RAWG 对象读出资料组（字段名与表单 meta 同构，返回全新副本）
const readMeta = (g) => ({
  released: g?.released || "",
  rating: g?.rating ?? 0,
  ratingTop: g?.ratingTop ?? 5,
  metacritic: g?.metacritic ?? 0,
  genres: [...(g?.genres || [])],
  platforms: [...(g?.platforms || [])],
  developers: [...(g?.developers || [])],
  publishers: [...(g?.publishers || [])],
  website: g?.website || "",
  rawgId: g?.rawgId || 0,
  rawgSlug: g?.rawgSlug || "",
  rawgUrl: g?.rawgUrl || "",
  rawgTags: [...(g?.rawgTags || [])],
});

const CONFLICT_OPTIONS = [
  ["manual", "手动选择"],
  ["local", "优先本地"],
  ["remote", "优先云端"],
];

const SYNC_BADGE = {
  success: ["ok", "成功"],
  conflict: ["warn", "冲突"],
  failed: ["err", "失败"],
  unconfigured: ["mute", "当前设备未配置"],
  disabled: ["mute", "已禁用"],
};

export function mount(root, ctx) {
  const { store, api, ui, router, params } = ctx;
  const { h, icon, iconEl, fmtTime, fmtDuration, fmtBytes } = ui;

  const gameId = params?.id || "";
  const isNew = !gameId;

  /* ---------------- 找不到游戏：空状态 ---------------- */

  function renderNotFound() {
    root.innerHTML = "";
    root.append(
      h(
        "div",
        { class: "page gd-page" },
        h(
          "div",
          { class: "empty" },
          h("div", { class: "empty-icon", html: icon("gamepad") }),
          h("div", { class: "empty-title" }, "游戏不存在"),
          h("div", { class: "empty-text" }, "该游戏可能已被删除，或链接已失效。"),
          h("button", { class: "btn btn-primary", onClick: () => router.back() }, iconEl("chevronLeft"), "返回"),
        ),
      ),
    );
  }

  if (!isNew && !store.select.game(gameId)) {
    renderNotFound();
    // 快照晚到（如恢复流程）时游戏可能出现，重挂载本页即可
    const off = store.subscribe(() => {
      if (store.select.game(gameId)) router.navigate("game", { id: gameId }, { push: false });
    });
    return () => off();
  }

  /* ---------------- 表单状态（唯一真源，随输入事件更新） ---------------- */

  const src = isNew ? null : store.select.game(gameId);
  const form = {
    name: src?.name || "",
    description: src?.description || "",
    isSteam: src ? Boolean(src.isSteam) : false,
    installPath: src?.installPath || "",
    savePath: src?.savePath || "",
    storageAccountId: src?.storageAccountId || "",
    conflictStrategy: src?.sync?.conflictStrategy || "manual",
    coverPath: src?.coverPath || "",
    tags: [...(src?.tags || [])],
    meta: readMeta(src),
  };
  if (isNew) {
    const firstEnabled = store.select.accounts().find((a) => a.enabled);
    if (firstEnabled) form.storageAccountId = firstEnabled.id;
  }

  // 脏字段集合：只记录用户实际操作过的字段名（键与 saveForm 的 writers 一一对应），
  // 保存时以最新快照为底、仅覆盖脏字段，避免陈旧表单整包回滚他机编辑
  const dirty = new Set();

  let tab = "info";
  let descEditing = false;
  let notFound = false;
  let disposed = false;
  let backups = null; // null = 尚未加载
  let backupsMsg = "";
  let backupsLoading = false;

  /* ---------------- 通用小工具 ---------------- */

  function withBusy(btn, fn) {
    return async () => {
      if (btn.disabled || btn.classList.contains("busy")) return;
      btn.disabled = true;
      btn.classList.add("busy");
      try {
        await fn();
      } finally {
        btn.disabled = false;
        btn.classList.remove("busy");
      }
    };
  }

  function coverRef() {
    const savedCoverPath = !isNew ? store.select.game(gameId)?.coverPath || "" : "";
    if (savedCoverPath && form.coverPath === savedCoverPath && !/^data:/i.test(form.coverPath)) return gameId;
    if (/^(https?:|data:)/i.test(form.coverPath || "")) return form.coverPath;
    if (savedCoverPath) return gameId;
    return "";
  }

  /* ---------------- 页面骨架（只建一次） ---------------- */

  const crumbName = h("span", { class: "gd-crumb-cur" }, isNew ? "添加游戏" : src?.name || "游戏详情");
  const topbar = h(
    "div",
    { class: "gd-topbar" },
    h("button", { class: "btn btn-ghost gd-back", onClick: () => router.back() }, iconEl("chevronLeft"), "返回"),
    h(
      "nav",
      { class: "gd-crumbs" },
      h("span", { class: "gd-crumb-link", onClick: () => router.navigate("library") }, "游戏库"),
      h("span", { class: "gd-crumb-sep" }, "/"),
      crumbName,
    ),
  );

  const hero = h("header", { class: "gd-hero" });

  const segInfoBtn = h("button", { class: "seg-item active", onClick: () => setTab("info") }, "资料");
  const segBackupBtn = h("button", { class: "seg-item", onClick: () => setTab("backups") }, "存档备份");
  const segRow = h("div", { class: "gd-seg-row" }, h("div", { class: "seg" }, segInfoBtn, segBackupBtn));

  const infoPanel = h("section", { class: "gd-panel" });
  const backupsPanel = h("section", { class: "gd-panel" });
  backupsPanel.style.display = "none";

  const page = h("div", { class: "page gd-page" }, topbar, hero, isNew ? null : segRow, infoPanel, backupsPanel);
  root.append(page);

  function setTab(next) {
    tab = next;
    segInfoBtn.classList.toggle("active", tab === "info");
    segBackupBtn.classList.toggle("active", tab === "backups");
    infoPanel.style.display = tab === "info" ? "" : "none";
    backupsPanel.style.display = tab === "backups" ? "" : "none";
    if (tab === "backups" && backups === null && !backupsLoading) loadBackups();
  }

  /* ---------------- 头部横幅（随 store 重建） ---------------- */

  function coverMenu(e) {
    ui.contextMenu(e, [
      { label: "本地图片…", icon: "image", onClick: pickLocalCover },
      { label: "SteamGridDB 搜索…", icon: "search", onClick: openSgdbDialog },
    ]);
  }

  async function pickLocalCover() {
    try {
      const path = await api.PickFile("选择封面图片");
      if (!path) return;
      form.coverPath = path;
      dirty.add("coverPath");
      renderHeader();
      ui.toast("已选择本地封面，保存后生效", "info");
    } catch (e) {
      ui.toast(`选择图片失败：${errMsg(e)}`, "err");
    }
  }

  function statCell(label, value, sub) {
    return h(
      "div",
      { class: "gd-stat" },
      h("div", { class: "gd-stat-k" }, label),
      h("div", { class: "gd-stat-v" }, value),
      sub ? h("div", { class: "gd-stat-sub" }, sub) : null,
    );
  }

  function syncStatCell(ls, game) {
    const status = game
      ? game.sync?.enabled === false
        ? "disabled"
        : !game.savePath
          ? "unconfigured"
          : ls?.status
      : ls?.status;
    const [tone, label] = status ? SYNC_BADGE[status] || ["mute", status] : ["mute", "从未"];
    return h(
      "div",
      { class: "gd-stat" },
      h("div", { class: "gd-stat-k" }, "上次同步"),
      h("div", { class: "gd-stat-v gd-stat-badge" }, h("span", { class: `badge ${tone}`, title: ls?.message || "" }, label)),
      ls?.syncedAt ? h("div", { class: "gd-stat-sub" }, fmtTime(ls.syncedAt)) : null,
    );
  }

  function buildBadges() {
    const m = form.meta;
    const badges = h("div", { class: "gd-badges" });
    badges.append(
      h(
        "span",
        { class: `badge ${form.isSteam ? "info" : "mute"}` },
        iconEl(form.isSteam ? "steam" : "monitor"),
        form.isSteam ? "Steam" : "第三方",
      ),
    );
    if (m.released) badges.append(h("span", { class: "badge mute" }, iconEl("calendar"), m.released));
    if ((m.metacritic || 0) >= 1) badges.append(h("span", { class: "badge ok" }, `MC ${m.metacritic}`));
    if ((m.rating || 0) >= 0.1) {
      badges.append(h("span", { class: "badge warn" }, iconEl("star"), `${Number(m.rating).toFixed(1)} / ${m.ratingTop || 5}`));
    }
    return badges;
  }

  function buildActions(live, syncing) {
    const actions = h("div", { class: "gd-actions" });
    const launchBtn = h(
      "button",
      {
        class: "btn btn-primary",
        disabled: !live.installPath || syncing,
        title: live.installPath ? "" : "未配置启动文件，无法启动",
        onClick: (e) => {
          e.currentTarget.disabled = true;
          store.actions.launchGame(gameId);
        },
      },
      iconEl("play"),
      "启动",
    );
    const syncBtn = h(
      "button",
      {
        class: `btn${syncing ? " busy" : ""}`,
        disabled: syncing,
        onClick: async (e) => {
          const btn = e.currentTarget;
          btn.disabled = true;
          btn.classList.add("busy");
          try {
            await store.actions.syncGame(gameId);
          } finally {
            btn.disabled = false;
            btn.classList.remove("busy");
          }
        },
      },
      iconEl("refresh"),
      "同步",
    );
    const fav = store.select.favoriteIds().has(gameId);
    const favBtn = h(
      "button",
      { class: `btn gd-fav${fav ? " active" : ""}`, onClick: () => store.actions.toggleFavorite(gameId) },
      iconEl("heart"),
      fav ? "已常玩" : "常玩",
    );
    const delBtn = h(
      "button",
      {
        class: "btn btn-danger",
        onClick: async () => {
          await store.actions.deleteGame(gameId);
          if (store.state.pendingDeletes?.has?.(gameId) || !store.select.game(gameId)) router.back();
        },
      },
      iconEl("trash"),
      "删除",
    );
    actions.append(launchBtn, syncBtn, favBtn, delBtn);
    return actions;
  }

  function renderHeader() {
    if (notFound) return;
    const live = isNew ? null : store.select.game(gameId);
    hero.innerHTML = "";

    const coverBox = h(
      "div",
      { class: "gd-cover-wrap", title: "更换封面", onClick: coverMenu, onContextmenu: coverMenu },
      ui.coverImg(coverRef(), "gd-cover"),
      h("div", { class: "gd-cover-edit" }, iconEl("image"), "更换封面"),
    );

    const main = h("div", { class: "gd-hero-main" });
    const rt = isNew ? null : store.select.runtimeStatus(gameId);
    if (rt) main.append(h("span", { class: `gd-status ${rt.tone}`, title: rt.detail || rt.text || "" }, h("span", { class: "gd-status-dot" }), rt.text));

    main.append(h("h1", { class: "gd-title" }, isNew ? "添加游戏" : live?.name || "未命名游戏"));
    main.append(buildBadges());
    main.append(
      h(
        "div",
        { class: "gd-stats card" },
        statCell("总时长", (live?.playTime || 0) >= 1 ? fmtDuration(live.playTime) : "—"),
        statCell("上次游玩", fmtTime(live?.lastPlayed)),
        statCell("备份数", String(live?.backupCount ?? 0)),
        syncStatCell(live?.lastSync, live),
      ),
    );
    if (!isNew && live) main.append(buildActions(live, rt?.tone === "syncing"));

    hero.append(coverBox, main);
    crumbName.textContent = isNew ? "添加游戏" : live?.name || "游戏详情";
  }

  /* ---------------- 资料表单（只建一次，保护焦点） ---------------- */

  let nameInput;
  let installInput;
  let saveInput;
  let tagInput;
  let descBox;
  let tagListEl;
  let tagSuggEl;
  let thirdLbl;
  let steamLbl;
  let platformSwitch;
  let accountSel;
  let conflictSel;

  function fieldLabel(text, required) {
    return h("span", { class: "field-label" }, text, required ? h("span", { class: "gd-req" }, " *") : null);
  }

  function renderDesc() {
    descBox.innerHTML = "";
    descBox.append(
      h(
        "div",
        { class: "gd-field-head" },
        fieldLabel("简介"),
        h(
          "button",
          {
            class: "btn btn-ghost btn-sm",
            onClick: () => {
              descEditing = !descEditing;
              renderDesc();
            },
          },
          iconEl(descEditing ? "check" : "pencil"),
          descEditing ? "完成" : "编辑",
        ),
      ),
    );
    if (descEditing) {
      const area = h(
        "textarea",
        {
          class: "input",
          rows: "6",
          placeholder: "写点什么…",
          onInput: (e) => {
            form.description = e.target.value;
            dirty.add("description");
          },
        },
        form.description,
      );
      descBox.append(area);
      area.focus();
    } else if (form.description.trim()) {
      descBox.append(h("p", { class: "gd-desc-read" }, form.description));
    } else {
      descBox.append(h("p", { class: "gd-desc-read placeholder" }, "暂无简介。点击「编辑」撰写，或用「RAWG 资料」自动填充。"));
    }
  }

  function addTag(raw) {
    const name = String(raw || "").trim();
    if (!name) return;
    if (form.tags.includes(name)) {
      ui.toast(`标签「${name}」已存在`, "info");
      return;
    }
    form.tags.push(name);
    dirty.add("tags");
    renderTags();
  }

  function renderTags() {
    tagListEl.innerHTML = "";
    if (!form.tags.length) {
      tagListEl.append(h("span", { class: "gd-tag-none" }, "暂无标签"));
    } else {
      for (const t of form.tags) {
        tagListEl.append(
          h(
            "span",
            { class: "chip gd-tag" },
            t,
            h(
              "button",
              {
                class: "gd-tag-x",
                title: "移除标签",
                onClick: () => {
                  form.tags = form.tags.filter((x) => x !== t);
                  dirty.add("tags");
                  renderTags();
                },
              },
              iconEl("x"),
            ),
          ),
        );
      }
    }
    tagSuggEl.innerHTML = "";
    const sugg = dedupe(form.meta.rawgTags).filter((t) => !form.tags.includes(t));
    if (sugg.length) {
      tagSuggEl.append(h("span", { class: "gd-tag-sugg-label" }, "RAWG 推荐："));
      for (const t of sugg) {
        tagSuggEl.append(h("button", { class: "chip gd-tag-add", onClick: () => addTag(t) }, iconEl("plus"), t));
      }
    }
  }

  function pathField(label, required, input, pick) {
    const browseBtn = h("button", { class: "btn" }, iconEl("folderOpen"), "浏览");
    browseBtn.addEventListener(
      "click",
      withBusy(browseBtn, async () => {
        try {
          const path = await pick();
          if (!path) return;
          input.value = path;
          input.dispatchEvent(new Event("input"));
        } catch (e) {
          ui.toast(`选择路径失败：${errMsg(e)}`, "err");
        }
      }),
    );
    return h("div", { class: "field" }, fieldLabel(label, required), h("div", { class: "input-row" }, input, browseBtn));
  }

  function buildAccountSelect() {
    const sel = h("select", {
      class: "input",
      onChange: (e) => {
        form.storageAccountId = e.target.value;
        dirty.add("storageAccountId");
      },
    });
    accountSel = sel;
    const all = store.select.accounts();
    const enabled = all.filter((a) => a.enabled);
    sel.append(h("option", { value: "" }, "未选择账号"));
    for (const a of enabled) sel.append(h("option", { value: a.id }, a.name || a.id));
    if (form.storageAccountId && !enabled.some((a) => a.id === form.storageAccountId)) {
      const raw = all.find((a) => a.id === form.storageAccountId);
      sel.append(h("option", { value: form.storageAccountId }, `${raw?.name || form.storageAccountId}（已停用）`));
    }
    sel.value = form.storageAccountId;
    return sel;
  }

  function updatePlatformLabels() {
    thirdLbl.classList.toggle("active", !form.isSteam);
    steamLbl.classList.toggle("active", form.isSteam);
  }

  async function saveForm() {
    if (!isNew && !dirty.size) {
      ui.toast("没有修改", "info");
      return;
    }
    const name = form.name.trim();
    if (!name) {
      ui.toast("请填写游戏名称", "warn");
      nameInput.focus();
      return;
    }
    // 脏字段提交：以保存时刻的最新快照为底，只覆盖用户改过的字段；
    // 新建时无 base，全部字段视为脏
    const base = (!isNew && store.select.game(gameId)) || {};
    const payload = {
      ...base,
      id: isNew ? "" : gameId,
      playTime: base.playTime || 0,
      sync: {
        enabled: base.sync?.enabled ?? true,
        includePatterns: base.sync?.includePatterns || [],
        excludePatterns: base.sync?.excludePatterns || [],
        conflictStrategy: base.sync?.conflictStrategy || "manual",
      },
    };
    const writers = {
      name: () => (payload.name = name),
      description: () => (payload.description = form.description),
      isSteam: () => (payload.isSteam = form.isSteam),
      installPath: () => (payload.installPath = form.installPath.trim()),
      savePath: () => (payload.savePath = form.savePath.trim()),
      coverPath: () => (payload.coverPath = form.coverPath),
      storageAccountId: () => (payload.storageAccountId = form.storageAccountId),
      conflictStrategy: () => (payload.sync.conflictStrategy = form.conflictStrategy),
      tags: () => (payload.tags = [...form.tags]),
      meta: () => Object.assign(payload, readMeta(form.meta)),
    };
    for (const field of isNew ? Object.keys(writers) : dirty) writers[field]?.();
    payload.backupStorageAccountId = base.backupStorageAccountId || payload.storageAccountId || "";
    try {
      await store.actions.saveGame(payload);
      dirty.clear();
      if (!isNew) api.invalidateCover?.(gameId);
      ui.toast(`已保存「${payload.name || name}」`, "ok");
      if (isNew) router.back();
    } catch (e) {
      ui.toast(`保存失败：${errMsg(e)}`, "err");
    }
  }

  function buildInfoPanel() {
    nameInput = h("input", {
      class: "input",
      value: form.name,
      placeholder: "游戏名称",
      onInput: (e) => {
        form.name = e.target.value;
        dirty.add("name");
      },
    });
    const rawgBtn = h("button", { class: "btn", onClick: openRawgDialog }, iconEl("sparkles"), "RAWG 资料");

    descBox = h("div", { class: "field gd-desc" });

    platformSwitch = h("input", {
      type: "checkbox",
      checked: form.isSteam,
      onChange: (e) => {
        form.isSteam = e.target.checked;
        dirty.add("isSteam");
        updatePlatformLabels();
        renderHeader();
      },
    });
    thirdLbl = h("span", { class: "gd-switch-side" }, "第三方");
    steamLbl = h("span", { class: "gd-switch-side" }, "Steam");

    installInput = h("input", {
      class: "input",
      value: form.installPath,
      placeholder: "游戏可执行文件路径",
      onInput: (e) => {
        form.installPath = e.target.value;
        dirty.add("installPath");
      },
    });
    saveInput = h("input", {
      class: "input",
      value: form.savePath,
      placeholder: "存档所在目录",
      onInput: (e) => {
        form.savePath = e.target.value;
        dirty.add("savePath");
      },
    });

    conflictSel = h("select", {
      class: "input",
      onChange: (e) => {
        form.conflictStrategy = e.target.value;
        dirty.add("conflictStrategy");
      },
    });
    for (const [v, label] of CONFLICT_OPTIONS) conflictSel.append(h("option", { value: v }, label));
    conflictSel.value = form.conflictStrategy;

    tagInput = h("input", {
      class: "input",
      placeholder: "输入标签，回车添加",
      onKeydown: (e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          addTag(tagInput.value);
          tagInput.value = "";
        }
      },
    });
    const tagAddBtn = h(
      "button",
      {
        class: "btn",
        onClick: () => {
          addTag(tagInput.value);
          tagInput.value = "";
          tagInput.focus();
        },
      },
      iconEl("plus"),
      "添加",
    );
    tagListEl = h("div", { class: "gd-tag-list" });
    tagSuggEl = h("div", { class: "gd-tag-sugg" });

    const saveBtn = h("button", { class: "btn btn-primary gd-save" }, iconEl("check"), "保存");
    saveBtn.addEventListener("click", withBusy(saveBtn, () => saveForm()));

    const card = h(
      "div",
      { class: "card gd-form-card" },
      h("div", { class: "field" }, fieldLabel("名称", true), h("div", { class: "input-row" }, nameInput, rawgBtn)),
      descBox,
      h(
        "div",
        { class: "field" },
        fieldLabel("平台"),
        h(
          "div",
          { class: "gd-switch-row" },
          thirdLbl,
          h("label", { class: "switch" }, platformSwitch),
          steamLbl,
        ),
      ),
      h(
        "div",
        { class: "gd-grid-2" },
        pathField("启动文件", false, installInput, () => api.PickFile("选择启动文件")),
        pathField("存档目录", false, saveInput, () => api.PickFolder("选择存档目录")),
      ),
      h(
        "div",
        { class: "gd-grid-2" },
        h("div", { class: "field" }, fieldLabel("存档账号"), buildAccountSelect()),
        h("div", { class: "field" }, fieldLabel("冲突策略"), conflictSel),
      ),
      h(
        "div",
        { class: "field" },
        fieldLabel("标签"),
        tagListEl,
        h("div", { class: "input-row gd-tag-input-row" }, tagInput, tagAddBtn),
        tagSuggEl,
      ),
      h("div", { class: "gd-form-foot" }, saveBtn),
    );
    infoPanel.append(card);
    renderDesc();
    renderTags();
    updatePlatformLabels();
  }

  /* ---------------- RAWG 对话框 ---------------- */

  function applyRawg(d, coverUrl) {
    form.name = d.name || form.name;
    form.description = d.description ?? form.description;
    if (coverUrl || d.coverPath) {
      form.coverPath = coverUrl || d.coverPath;
      dirty.add("coverPath");
    }
    form.meta = readMeta(d);
    // RAWG 写入的字段全部记脏，保存时才随 payload 提交
    dirty.add("name").add("description").add("meta");
    nameInput.value = form.name;
    descEditing = false;
    renderDesc();
    renderTags();
    renderHeader();
    ui.toast("已应用 RAWG 资料，保存后生效", "ok");
  }

  function dlgHint(text, name = "info") {
    return h("div", { class: "gd-dlg-hint" }, iconEl(name), text);
  }

  function dlgSkeleton(stage, n, cls) {
    stage.innerHTML = "";
    const wrap = h("div", { class: "gd-dlg-skel" });
    for (let i = 0; i < n; i += 1) wrap.append(ui.skeleton(cls));
    stage.append(wrap);
  }

  function coverPickGrid(urls, onPick) {
    const grid = h("div", { class: "gd-cover-grid stagger" });
    for (const u of urls) {
      grid.append(h("div", { class: "gd-cover-pick", onClick: () => onPick(u) }, ui.coverImg(u, "gd-cover-pick-img")));
    }
    return grid;
  }

  function openRawgDialog() {
    ui.dialog({
      title: "RAWG 资料检索",
      width: 720,
      render(body, close) {
        const input = h("input", {
          class: "input",
          placeholder: "输入游戏名称搜索 RAWG…",
          value: form.name.trim(),
          onKeydown: (e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              doSearch();
            }
          },
        });
        const searchBtn = h("button", { class: "btn btn-primary", onClick: () => doSearch() }, iconEl("search"), "搜索");
        const stage = h("div", { class: "gd-dlg-stage" });
        body.append(h("div", { class: "input-row gd-dlg-search" }, input, searchBtn), stage);

        async function doSearch() {
          const q = input.value.trim();
          if (!q) {
            input.focus();
            return;
          }
          dlgSkeleton(stage, 4, "gd-skel-result");
          let list;
          try {
            list = await api.SearchRAWGGames(q);
          } catch (e) {
            ui.toast(`RAWG 搜索失败：${errMsg(e)}`, "err");
            stage.innerHTML = "";
            stage.append(dlgHint(`搜索失败：${errMsg(e)}`, "alert"));
            return;
          }
          renderResults(list || []);
        }

        function renderResults(list) {
          stage.innerHTML = "";
          if (!list.length) {
            stage.append(dlgHint("没有找到匹配的游戏，换个关键词试试。", "search"));
            return;
          }
          const box = h("div", { class: "gd-rawg-list stagger" });
          for (const r of list) {
            box.append(
              h(
                "div",
                { class: "gd-rawg-row", onClick: () => pickResult(r) },
                ui.coverImg(r.coverPath, "gd-rawg-thumb"),
                h(
                  "div",
                  { class: "gd-rawg-main" },
                  h("div", { class: "gd-rawg-name" }, r.name),
                  h(
                    "div",
                    { class: "gd-rawg-meta" },
                    r.released ? h("span", {}, r.released) : null,
                    (r.rating || 0) >= 0.1 ? h("span", {}, `★ ${Number(r.rating).toFixed(1)}`) : null,
                  ),
                ),
                (r.metacritic || 0) >= 1 ? h("span", { class: "badge ok" }, `MC ${r.metacritic}`) : null,
                iconEl("chevronRight", "gd-rawg-go"),
              ),
            );
          }
          stage.append(box);
        }

        async function pickResult(r) {
          dlgSkeleton(stage, 3, "gd-skel-detail");
          let d;
          try {
            d = await api.GetRAWGGame(r.id);
          } catch (e) {
            ui.toast(`拉取资料失败：${errMsg(e)}`, "err");
            stage.innerHTML = "";
            stage.append(dlgHint(`拉取资料失败：${errMsg(e)}`, "alert"));
            return;
          }
          const covers = dedupe(d.coverOptions);
          if (covers.length > 1) {
            stage.innerHTML = "";
            stage.append(
              h(
                "div",
                { class: "gd-dlg-subhead" },
                h("span", {}, `「${d.name}」有 ${covers.length} 张候选封面，点选一张应用：`),
                h(
                  "button",
                  {
                    class: "btn btn-ghost btn-sm",
                    onClick: () => {
                      applyRawg(d, d.coverPath);
                      close();
                    },
                  },
                  "跳过，使用默认封面",
                ),
              ),
              coverPickGrid(covers, (u) => {
                applyRawg(d, u);
                close();
              }),
            );
          } else {
            applyRawg(d, covers[0] || d.coverPath);
            close();
          }
        }

        if (input.value) doSearch();
        else window.setTimeout(() => input.focus(), 80);
      },
    });
  }

  /* ---------------- SteamGridDB 对话框 ---------------- */

  function openSgdbDialog() {
    ui.dialog({
      title: "SteamGridDB 封面搜索",
      width: 860,
      render(body, close) {
        const input = h("input", {
          class: "input",
          placeholder: "输入游戏名称搜索封面…",
          value: form.name.trim(),
          onKeydown: (e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              doSearch();
            }
          },
        });
        const searchBtn = h("button", { class: "btn btn-primary", onClick: () => doSearch() }, iconEl("search"), "搜索");
        const stage = h("div", { class: "gd-dlg-stage" });
        body.append(h("div", { class: "input-row gd-dlg-search" }, input, searchBtn), stage);

        const applyCover = (u) => {
          form.coverPath = u;
          dirty.add("coverPath");
          renderHeader();
          ui.toast("封面已更新，保存后生效", "ok");
          close();
        };

        async function doSearch() {
          const q = input.value.trim();
          if (!q) {
            input.focus();
            return;
          }
          dlgSkeleton(stage, 2, "gd-skel-detail");
          let list;
          try {
            list = await api.SearchSteamGridDBGames(q);
          } catch (e) {
            ui.toast(`SteamGridDB 搜索失败：${errMsg(e)}`, "err");
            stage.innerHTML = "";
            stage.append(dlgHint(`搜索失败：${errMsg(e)}`, "alert"));
            return;
          }
          stage.innerHTML = "";
          if (!list?.length) {
            stage.append(dlgHint("没有找到匹配的封面，换个关键词试试。", "search"));
            return;
          }
          for (const r of list) {
            const covers = dedupe([r.coverPath, ...(r.coverOptions || [])]);
            stage.append(
              h(
                "section",
                { class: "gd-sgdb-item" },
                h(
                  "div",
                  { class: "gd-sgdb-head" },
                  h("span", { class: "gd-sgdb-name" }, r.name),
                  r.verified ? h("span", { class: "badge info" }, iconEl("shield"), "已认证") : null,
                ),
                coverPickGrid(covers, applyCover),
              ),
            );
          }
        }

        if (input.value) doSearch();
        else window.setTimeout(() => input.focus(), 80);
      },
    });
  }

  /* ---------------- 存档备份页签 ---------------- */

  const bkNameInput = h("input", { class: "input", placeholder: "备份名称（可选）" });
  const bkCreateBtn = h("button", { class: "btn btn-primary" }, iconEl("archive"), "创建新备份");
  const bkBanner = h("div", { class: "gd-bk-banner" });
  bkBanner.style.display = "none";
  const bkList = h("div", { class: "gd-bk-list-box" });

  bkCreateBtn.addEventListener(
    "click",
    withBusy(bkCreateBtn, async () => {
      const live = store.select.game(gameId);
      try {
        const rec = await api.CreateGameBackup(gameId, live?.backupStorageAccountId || "", bkNameInput.value.trim());
        if (backups === null) backups = [];
        if (rec) backups.unshift(rec);
        bkNameInput.value = "";
        renderBackups();
        ui.toast("备份已创建", "ok");
      } catch (e) {
        ui.toast(`创建备份失败：${errMsg(e)}`, "err");
      }
    }),
  );

  async function loadBackups() {
    if (notFound || isNew) return;
    backupsLoading = true;
    if (backups === null) {
      bkList.innerHTML = "";
      const wrap = h("div", { class: "gd-dlg-skel" });
      for (let i = 0; i < 3; i += 1) wrap.append(ui.skeleton("gd-bk-skel"));
      bkList.append(wrap);
    }
    try {
      const res = await api.GetGameBackups(gameId);
      backups = res?.backups || [];
      backupsMsg = res?.partial ? res?.message || "部分账号的备份列表拉取失败，结果可能不完整" : "";
    } catch (e) {
      backups = backups || [];
      backupsMsg = "";
      ui.toast(`获取备份列表失败：${errMsg(e)}`, "err");
    }
    backupsLoading = false;
    renderBackups();
  }

  function backupRow(b) {
    const label = b.name || b.filename;
    const restoreBtn = h("button", { class: "btn btn-sm" }, iconEl("history"), "恢复");
    restoreBtn.addEventListener(
      "click",
      withBusy(restoreBtn, async () => {
        const yes = await ui.confirm({
          message: `确定将「${label}」恢复到本地存档目录吗？\n当前本地存档将被覆盖。`,
          confirmText: "恢复",
        });
        if (!yes) return;
        try {
		  await api.RestoreGameBackup(gameId, b.id);
          ui.toast("已恢复到本地", "ok");
        } catch (e) {
          ui.toast(`恢复失败：${errMsg(e)}`, "err");
        }
      }),
    );
    const delBtn = h("button", { class: "btn btn-sm btn-danger" }, iconEl("trash"), "删除");
    delBtn.addEventListener(
      "click",
      withBusy(delBtn, async () => {
        const yes = await ui.confirm({
          message: `确定删除备份「${label}」吗？\n本地与云端副本将一并删除，此操作不可恢复。`,
          confirmText: "删除",
          tone: "danger",
        });
        if (!yes) return;
        try {
		  await api.DeleteGameBackup(gameId, b.id);
		  backups = (backups || []).filter((x) => x.id !== b.id);
          renderBackups();
          ui.toast("备份已删除", "ok");
        } catch (e) {
          ui.toast(`删除备份失败：${errMsg(e)}`, "err");
        }
      }),
    );
    return h(
      "div",
      { class: "gd-bk-row" },
      h(
        "span",
        { class: `badge ${b.type === "manual" ? "ok" : "info"}` },
        b.type === "manual" ? "手动" : "自动",
      ),
      h(
        "div",
        { class: "gd-bk-main" },
        h("div", { class: "gd-bk-name" }, label),
        b.name ? h("div", { class: "gd-bk-file" }, b.filename) : null,
      ),
      h("span", { class: "gd-bk-size" }, fmtBytes(b.size)),
      h("span", { class: "gd-bk-time" }, fmtTime(b.createdAt)),
      h(
        "span",
        { class: "gd-bk-ex" },
        iconEl("hardDrive", b.localExists ? "" : "off"),
        iconEl("cloud", b.cloudExists ? "" : "off"),
      ),
      h("div", { class: "gd-bk-actions" }, restoreBtn, delBtn),
    );
  }

  function renderBackups() {
    if (backupsMsg) {
      bkBanner.innerHTML = "";
      bkBanner.append(iconEl("alert"), backupsMsg);
      bkBanner.style.display = "";
    } else {
      bkBanner.style.display = "none";
    }
    bkList.innerHTML = "";
    const list = backups || [];
    if (!list.length) {
      bkList.append(
        h(
          "div",
          { class: "empty gd-bk-empty" },
          h("div", { class: "empty-icon", html: icon("archive") }),
          h("div", { class: "empty-title" }, "还没有存档备份"),
          h("div", { class: "empty-text" }, "点击上方「创建新备份」，把当前本地存档打包留存一份。"),
        ),
      );
      return;
    }
    const card = h("div", { class: "card gd-bk-list stagger" });
    for (const b of list) card.append(backupRow(b));
    bkList.append(card);
  }

  if (!isNew) {
    backupsPanel.append(
      h("div", { class: "gd-bk-create" }, bkNameInput, bkCreateBtn),
      bkBanner,
      bkList,
    );
  }

  /* ---------------- 挂载 / 订阅 / 清理 ---------------- */

  buildInfoPanel();
  renderHeader();

  // 头部渲染签名：本游戏数据/状态没变时跳过重建（避免无关通知反复重挂封面）
  const headerSig = () =>
    JSON.stringify([
      store.select.game(gameId) || null,
      store.select.runtimeStatus(gameId) || null,
      store.select.favoriteIds().has(gameId),
    ]);
  let renderedHeaderSig = isNew ? "" : headerSig();

  // state:updated 后把未被用户改过的字段跟随最新快照刷新显示。
  // 保存正确性不依赖这里（saveForm 以保存时刻最新 base 为底），显示刷新尽力而为：
  // 聚焦中的控件与展开中的简介编辑器跳过，避免打断输入。
  function refreshCleanFields(live) {
    if (!live) return;
    const focused = document.activeElement;
    if (!dirty.has("name") && focused !== nameInput) {
      form.name = live.name || "";
      nameInput.value = form.name;
    }
    if (!dirty.has("description") && !descEditing && form.description !== (live.description || "")) {
      form.description = live.description || "";
      renderDesc();
    }
    if (!dirty.has("isSteam")) {
      form.isSteam = Boolean(live.isSteam);
      platformSwitch.checked = form.isSteam;
      updatePlatformLabels();
    }
    if (!dirty.has("installPath") && focused !== installInput) {
      form.installPath = live.installPath || "";
      installInput.value = form.installPath;
    }
    if (!dirty.has("savePath") && focused !== saveInput) {
      form.savePath = live.savePath || "";
      saveInput.value = form.savePath;
    }
    if (!dirty.has("coverPath")) form.coverPath = live.coverPath || "";
    if (!dirty.has("storageAccountId") && focused !== accountSel) {
      form.storageAccountId = live.storageAccountId || "";
      // 选项不存在（如账号列表变化）时不强设，避免 select 值被清空
      if ([...accountSel.options].some((o) => o.value === form.storageAccountId)) {
        accountSel.value = form.storageAccountId;
      }
    }
    if (!dirty.has("conflictStrategy") && focused !== conflictSel) {
      form.conflictStrategy = live.sync?.conflictStrategy || "manual";
      conflictSel.value = form.conflictStrategy;
    }
    let tagsStale = false;
    if (!dirty.has("tags")) {
      const next = [...(live.tags || [])];
      if (JSON.stringify(next) !== JSON.stringify(form.tags)) {
        form.tags = next;
        tagsStale = true;
      }
    }
    if (!dirty.has("meta")) {
      const next = readMeta(live);
      if (JSON.stringify(next) !== JSON.stringify(form.meta)) {
        form.meta = next;
        tagsStale = true; // RAWG 推荐标签随 meta 变化
      }
    }
    if (tagsStale) renderTags();
  }

  const offStore = store.subscribe(() => {
    if (!isNew && !store.select.game(gameId)) {
      if (notFound) return;
      notFound = true;
      // 远端删除无法阻止：有未保存修改时先提示，用户确认后再清空页面
      const clear = () => {
        if (!disposed && notFound) renderNotFound();
      };
      if (dirty.size) {
        ui.confirm({
          message: "游戏已在其他设备被删除，放弃未保存的修改？",
          confirmText: "知道了",
        }).then(clear, clear);
      } else {
        clear();
      }
      return;
    }
    if (notFound) {
      // 游戏重新出现（如他机恢复）：重挂载本页，对齐挂载早期路径行为
      router.navigate("game", { id: gameId }, { push: false });
      return;
    }
    if (isNew) return; // 新建页头部为静态，无需随快照刷新
    const sig = headerSig();
    if (sig === renderedHeaderSig) return;
    renderedHeaderSig = sig;
    refreshCleanFields(store.select.game(gameId));
    renderHeader();
  });

  const offBackupEvt = api.onEvent("game:backup_success", (p) => {
    if (notFound || isNew) return;
    if (evId(p) === gameId && backups !== null && !backupsLoading) loadBackups();
  });

  return () => {
    disposed = true;
    offStore();
    offBackupEvt?.();
  };
}
