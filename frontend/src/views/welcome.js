// ============================================================
// views/welcome.js —— 首次启动欢迎页（整页路由，前缀 .wel-）
// 居中舞台：品牌墨块 + Fraunces 标题 + 选择卡 + 底部隐私小字
// 两级步骤（同页切换）：三选卡 →「配置云端存储」→ 选择存储方式
// （Cloudflare R2 / WebDAV 两张 provider 大卡，带返回）。
// 本页不展示 store 数据，DOM 只在挂载时构建一次；
// 恢复流程中避免因 boot() 触发的快照通知重建 DOM 而丢失 busy 态。
// ============================================================

export function mount(root, ctx) {
  const { store, api, ui, router } = ctx;
  const { h, icon, iconEl } = ui;

  let restoring = false;
  let alive = true;

  /* ---------- 选择卡工厂 ---------- */

  function makeCard({ iconName, name, desc, hint, onClick }) {
    const glyph = h("div", { class: "wel-card-glyph", html: icon(iconName) });
    const card = h(
      "button",
      { class: "card wel-card", type: "button", onClick },
      glyph,
      h("div", { class: "wel-card-name" }, name),
      h("p", { class: "wel-card-desc" }, desc),
      h(
        "div",
        { class: "wel-card-hint" },
        h("span", {}, hint),
        h("span", { class: "wel-card-arrow", html: icon("chevronRight") }),
      ),
    );
    return { card, glyph };
  }

  /* ---------- 从备份恢复 ---------- */

  const restore = makeCard({
    iconName: "upload",
    name: "从备份恢复",
    desc: "导入配置备份文件，恢复全部游戏与账号设置",
    hint: "选择备份文件",
    onClick: handleRestore,
  });

  async function handleRestore() {
    if (restoring) return;
    restoring = true;
    restore.card.classList.add("busy");
    restore.glyph.innerHTML = icon("refresh");
    try {
      await api.ImportAppBackup();
      await store.actions.boot();
      ui.toast("恢复完成", "ok");
      router.navigate("library");
    } catch (e) {
      ui.toast(`恢复未完成：${e?.message || e || "操作已取消"}`, "warn");
      restoring = false;
      if (!alive) return;
      restore.card.classList.remove("busy");
      restore.glyph.innerHTML = icon("upload");
    }
  }

  /* ---------- 配置云端存储 / 先逛逛 ---------- */

  const configure = makeCard({
    iconName: "cloud",
    name: "配置云端存储",
    desc: "接入 Cloudflare 或 WebDAV，把存档同步到你自己的云端",
    hint: "选择存储方式",
    onClick: () => {
      if (restoring) return;
      showStep("provider");
    },
  });

  const browse = makeCard({
    iconName: "login",
    name: "先逛逛",
    desc: "直接进入游戏库，稍后随时可配置",
    hint: "进入游戏库",
    onClick: () => {
      if (restoring) return;
      router.navigate("library");
    },
  });

  /* ---------- 子步骤：选择存储方式 ---------- */

  const cfCard = makeCard({
    iconName: "cloud",
    name: "Cloudflare R2",
    desc: "免费额度大 · 需要一点配置",
    hint: "使用 Cloudflare",
    onClick: () => router.navigate("accounts", { openNew: true, provider: "cloudflare" }),
  });

  const davCard = makeCard({
    iconName: "hardDrive",
    name: "WebDAV",
    desc: "Nextcloud/坚果云/NAS · 填地址账号密码即用",
    hint: "使用 WebDAV",
    onClick: () => router.navigate("accounts", { openNew: true, provider: "webdav" }),
  });

  const stepMain = h(
    "div",
    { class: "wel-step" },
    h("div", { class: "wel-cards stagger" }, restore.card, configure.card, browse.card),
  );

  const stepProvider = h(
    "div",
    { class: "wel-step wel-step-provider off" },
    h(
      "div",
      { class: "wel-step-bar" },
      h(
        "button",
        { class: "btn btn-ghost btn-sm wel-back", type: "button", onClick: () => showStep("main") },
        iconEl("chevronLeft"),
        "返回",
      ),
    ),
    h("h2", { class: "wel-step-title" }, "选择存储方式"),
    h("p", { class: "wel-step-sub" }, "两种方式的数据都只属于你，之后也可以在「云账号」页调整。"),
    h("div", { class: "wel-cards wel-cards-2 stagger" }, cfCard.card, davCard.card),
  );

  // 同页切换：只改显隐，恢复卡的 busy 态与已建 DOM 全部保留；
  // display none→有 会重放子元素的 page-in 入场动画，天然带过渡
  function showStep(name) {
    const provider = name === "provider";
    stepMain.classList.toggle("off", provider);
    stepProvider.classList.toggle("off", !provider);
  }

  /* ---------- 组装页面 ---------- */

  const page = h(
    "div",
    { class: "page wel-page" },
    h("div", { class: "wel-wash", "aria-hidden": "true" }),
    h(
      "div",
      { class: "wel-stage" },
      h("div", { class: "wel-logo", "aria-hidden": "true", html: icon("gamepad") }),
      h("h1", { class: "wel-title" }, "欢迎使用 ", h("span", { class: "wel-brand" }, "GameSync")),
      h("p", { class: "wel-sub" }, "把每一份存档，稳稳放进你自己的云端。"),
      stepMain,
      stepProvider,
    ),
    h(
      "p",
      { class: "wel-foot" },
      h("span", { class: "wel-foot-icon", html: icon("shield") }),
      h("span", {}, "数据仅存本机与你自己的云端存储，不经第三方服务器。"),
    ),
  );

  root.append(page);

  return () => {
    alive = false;
  };
}
