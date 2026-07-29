// ============================================================
// views/activity.js —— 同步动态：统计带 + 垂直时间线（前缀 .act-）
// 模式：mount(root, ctx) → 订阅 store → render() 全量重建 → 返回 cleanup
// ============================================================

export function mount(root, ctx) {
  const { store, ui, router } = ctx;
  const { h, icon, iconEl, fmtTime, fmtBytes } = ui;

  const resolving = new Set(); // 正在「立即处理」的 gameId，防重复点击
  let disposed = false;

  /* ---------------- 状态映射 ---------------- */

  function dotTone(status) {
    if (status === "success") return "success";
    if (status === "conflict" || status === "warning") return "warn";
    if (status === "failed" || status === "error") return "err";
    return "idle";
  }

  function statusBadge(status) {
    switch (status) {
      case "success":
        return h("span", { class: "badge ok" }, "成功");
      case "conflict":
        return h("span", { class: "badge warn" }, "冲突");
      case "warning":
        return h("span", { class: "badge warn" }, "警告");
      case "failed":
      case "error":
        return h("span", { class: "badge err" }, "失败");
      default:
        return h("span", { class: "badge mute" }, status || "未知");
    }
  }

  function accountLabel(accountId) {
    if (!accountId) return "";
    const acc = store.select.accounts().find((a) => a.id === accountId);
    return acc?.name || String(accountId).slice(0, 8);
  }

  /* ---------------- 统计 ---------------- */

  function pendingConflictCount() {
    const ids = new Set();
    for (const a of store.select.activities()) {
      if (a.status === "conflict") ids.add(a.gameId || a.id);
    }
    for (const g of store.select.games()) {
      if (g.lastSync?.status === "conflict") ids.add(g.id);
    }
    return ids.size;
  }

  function renderStats() {
    const games = store.select.games();
    const activities = store.select.activities();
    const accounts = store.select.accounts();

    const okCount = activities.filter((a) => a.status === "success").length;
    const rate = activities.length ? `${Math.round((okCount / activities.length) * 100)}%` : "--";
    const conflicts = pendingConflictCount();
    const usedBytes = accounts.reduce((sum, a) => sum + (Number(a.usedBytes) || 0), 0);

    const stat = (num, label, numCls = "") =>
      h(
        "article",
        { class: "card act-stat" },
        h("div", { class: `act-stat-num${numCls ? ` ${numCls}` : ""}` }, num),
        h("div", { class: "act-stat-label" }, label),
      );

    return h(
      "section",
      { class: "act-stats stagger" },
      stat(String(games.length), "游戏总数"),
      stat(rate, "同步成功率"),
      stat(String(conflicts), "待处理冲突", conflicts > 0 ? "warn" : ""),
      stat(fmtBytes(usedBytes), "云端用量"),
    );
  }

  /* ---------------- 冲突处理 ---------------- */

  async function resolveConflict(gameId) {
    if (resolving.has(gameId)) return;
    resolving.add(gameId);
    render();
    try {
      // 动作内部会弹出冲突选择对话框并处理 toast
      await store.actions.syncGame(gameId);
    } finally {
      resolving.delete(gameId);
      if (!disposed) render();
    }
  }

  /* ---------------- 时间线条目 ---------------- */

  function renderEntry(activity) {
    const game = activity.gameId ? store.select.game(activity.gameId) : null;
    const isConflict = activity.status === "conflict";
    const nameText = activity.gameName || game?.name || "未知游戏";

    const nameEl = game
      ? h(
          "button",
          {
            class: "act-game act-game-link",
            title: `查看「${nameText}」详情`,
            onClick: () => router.navigate("game", { id: game.id }),
          },
          nameText,
        )
      : h("span", { class: "act-game" }, nameText);

    const head = h("div", { class: "act-entry-head" }, nameEl, statusBadge(activity.status));

    if (isConflict && game) {
      const busy = resolving.has(game.id);
      head.append(
        h(
          "button",
          {
            class: `btn btn-sm act-resolve${busy ? " busy" : ""}`,
            disabled: busy,
            onClick: () => resolveConflict(game.id),
          },
          iconEl("refresh"),
          busy ? "处理中…" : "立即处理",
        ),
      );
    }

    const metaParts = [h("span", {}, fmtTime(activity.startedAt))];
    const accName = accountLabel(activity.accountId);
    if (accName) metaParts.push(h("span", {}, accName));
    if (Number(activity.uploaded) > 0) {
      metaParts.push(h("span", { class: "act-flow up" }, `↑ ${fmtBytes(activity.uploaded)}`));
    }
    if (Number(activity.downloaded) > 0) {
      metaParts.push(h("span", { class: "act-flow down" }, `↓ ${fmtBytes(activity.downloaded)}`));
    }

    const meta = h("div", { class: "act-meta" });
    metaParts.forEach((part, i) => {
      if (i > 0) meta.append(h("span", { class: "act-meta-sep" }, "·"));
      meta.append(part);
    });

    return h(
      "li",
      { class: "act-item" },
      h("span", { class: `act-dot ${dotTone(activity.status)}` }),
      h(
        "article",
        { class: `card card-hover act-entry${isConflict ? " conflict" : ""}` },
        head,
        activity.message ? h("p", { class: "act-msg" }, activity.message) : null,
        meta,
      ),
    );
  }

  /* ---------------- 渲染 ---------------- */

  function render() {
    const activities = store.select.activities();
    root.innerHTML = "";

    const page = h("div", { class: "page act-page" });
    page.append(
      h(
        "header",
        { class: "act-head" },
        h(
          "div",
          {},
          h("h1", { class: "act-title" }, "同步动态"),
          h("p", { class: "act-sub" }, "各设备的同步流水与云端使用概览"),
        ),
        h("div", { class: "act-count", html: `${icon("activity")}<span>${activities.length} 条记录</span>` }),
      ),
      renderStats(),
    );

    if (!activities.length) {
      page.append(
        h(
          "div",
          { class: "empty" },
          h("div", { class: "empty-icon", html: icon("activity") }),
          h("div", { class: "empty-title" }, "还没有同步记录"),
          h("div", { class: "empty-text" }, "去游戏库同步一次吧，之后每次同步都会记录在这里。"),
          h(
            "button",
            { class: "btn btn-primary", onClick: () => router.navigate("library") },
            iconEl("gamepad"),
            "前往游戏库",
          ),
        ),
      );
    } else {
      const list = h("ol", { class: "act-list stagger" });
      for (const activity of activities) list.append(renderEntry(activity));
      page.append(h("section", { class: "act-timeline" }, list));
    }

    root.append(page);
  }

  // 渲染签名：本页数据没变时跳过重建（避免同步进度等无关通知引发闪烁）
  const sigOf = () =>
    JSON.stringify([
      store.select.activities(),
      store.select.games().map((g) => [g.id, g.lastSync?.status]),
      store.select.accounts().map((a) => [a.id, a.name, a.usedBytes]),
    ]);

  render();
  let renderedSig = sigOf();
  const off = store.subscribe(() => {
    const sig = sigOf();
    if (sig === renderedSig) return;
    renderedSig = sig;
    render();
  });
  return () => {
    disposed = true;
    off();
  };
}
