// ============================================================
// views/library.js —— 游戏库主页：筛选纸签行 + 封面墙
// 增量渲染：页面骨架只建一次；卡片按 game.id 复用 DOM 节点，
// 内容签名变了才重建单卡，运行状态/忙碌态原地更新；
// 入场动画仅在进入页面时播放一次，后续更新不闪烁。
// 拖拽排序仅在「全部 + 无搜索词」时启用（HTML5 draggable 实时换位）
// ============================================================

export function mount(root, ctx) {
  const { store, api, ui, router } = ctx;
  const { h, icon, iconEl, fmtTime } = ui;

  const busy = new Set(); // 'launch:<id>'，异步动作防重复点击
  let dragId = null; // 拖拽中的游戏 id（期间暂停重渲染，避免 DOM 被重建）
  let dragEl = null; // 拖拽中的卡片元素
  let dragDirty = false; // 拖拽期间被合并掉的渲染请求
  let alive = true; // 卸载后拦截迟到的异步回调

  /* ---------------- 页面骨架（只建一次） ---------------- */

  const filtersBox = h("div", { class: "lib-filters" });
  const headBox = h("div", { class: "lib-head-slot" });
  const contentBox = h("div", { class: "lib-content-slot" });
  const pageEl = h("div", { class: "page lib-page" }, filtersBox, headBox, contentBox);
  root.append(pageEl);

  let gridEl = null; // 持久网格节点（列表非空时存在）
  let firstFill = true; // 首次填充才播交错入场
  let filtersSig = "";
  let headSig = "";
  let emptySig = ""; // 当前空状态标识（"" = 显示网格）

  /* ---------------- 工具 ---------------- */

  const coverRef = (g) => (/^data:/i.test(g.coverPath || "") ? g.coverPath : g.id);

  function scheduleRender() {
    if (!alive) return;
    if (dragId) {
      dragDirty = true;
      return;
    }
    render();
  }

  async function runBusy(key, fn) {
    if (busy.has(key)) return;
    busy.add(key);
    scheduleRender();
    try {
      await fn();
    } finally {
      busy.delete(key);
      scheduleRender();
    }
  }

  function gameMenu(e, g) {
    const fav = store.select.favoriteIds().has(g.id);
    // 菜单会盖在光标下方使卡片丢失 :hover，钉住悬停态避免封面缩放抖动。
    // 注意：必须先开新菜单（内部会关旧菜单并触发其 onClose），再加钉住类，
    // 否则连续右键同一张卡时旧菜单的 onClose 会把刚加的类摘掉。
    const card = e.currentTarget;
    ui.contextMenu(e, [
      {
        label: fav ? "移出常玩" : "加入常玩",
        icon: "heart",
        onClick: () => store.actions.toggleFavorite(g.id),
      },
      {
        label: "游戏详情",
        icon: "eye",
        onClick: () => router.navigate("game", { id: g.id }),
      },
      {
        label: "立即同步",
        icon: "refresh",
        onClick: () => store.actions.syncGame(g.id),
      },
      "divider",
      {
        label: "删除游戏",
        icon: "trash",
        danger: true,
        onClick: () => store.actions.deleteGame(g.id),
      },
    ], { onClose: () => card.classList.remove("menu-pin") });
    card.classList.add("menu-pin");
  }

  const goAdd = () => router.navigate("game", { id: null });

  /* ---------------- 筛选纸签行 ---------------- */

  function updateFilters() {
    const filter = store.select.libraryFilter();
    const pinned = store.select.pinnedTags();
    const sig = JSON.stringify([filter.kind, filter.tag, pinned]);
    if (sig === filtersSig) return;
    filtersSig = sig;

    const chip = (label, active, onClick, iconName) =>
      h(
        "button",
        { class: `chip${active ? " active" : ""}`, onClick },
        iconName ? iconEl(iconName) : null,
        label,
      );

    filtersBox.innerHTML = "";
    filtersBox.append(
      chip("全部", filter.kind === "all", () =>
        store.actions.setLibraryFilter({ kind: "all", tag: "" }),
      ),
      chip("常玩", filter.kind === "fav", () =>
        store.actions.setLibraryFilter({ kind: "fav", tag: "" }),
        "heart",
      ),
    );
    for (const tag of pinned) {
      filtersBox.append(
        chip(tag, filter.kind === "tag" && filter.tag === tag, () =>
          store.actions.setLibraryFilter({ kind: "tag", tag }),
          "tag",
        ),
      );
    }
    filtersBox.append(
      h("span", { class: "lib-filters-gap", "aria-hidden": "true" }),
      h("button", { class: "btn btn-primary", onClick: goAdd }, iconEl("plus"), "添加游戏"),
    );
  }

  /* ---------------- 节区标题 ---------------- */

  function updateSectionHead(filter, q, count, sortable) {
    let name = "全部游戏";
    if (q) name = `搜索「${q}」`;
    else if (filter.kind === "fav") name = "常玩游戏";
    else if (filter.kind === "tag") name = `标签 · ${filter.tag}`;
    const sig = JSON.stringify([name, count, sortable]);
    if (sig === headSig) return;
    headSig = sig;

    headBox.innerHTML = "";
    headBox.append(
      h(
        "div",
        { class: "lib-section-head" },
        h(
          "h2",
          { class: "lib-section-title" },
          name,
          h("span", { class: "lib-section-count" }, ` · ${count}`),
        ),
        sortable && count > 1
          ? h("span", {
              class: "lib-section-hint",
              html: `${icon("grip")}<span>拖拽卡片可调整顺序</span>`,
            })
          : null,
      ),
    );
  }

  /* ---------------- 封面卡片 ---------------- */

  const coverIdentitySig = (g) =>
    JSON.stringify([
      g.coverPath || "",
      g.coverLocalPath || "",
      g.coverCloudAccountId || "",
      g.coverCloudKey || "",
      g.coverUpdatedAt || "",
    ]);

  // 内容签名：变了才整卡重建（重建会重挂封面，不应频繁发生）
  const cardContentSig = (g, fav, sortable) =>
    JSON.stringify([
      g.name,
      coverRef(g),
      coverIdentitySig(g),
      g.lastPlayed || "",
      !g.installPath,
      !g.savePath,
      g.sync?.enabled === false,
      fav,
      sortable,
    ]);

  // 状态签名：变了只原地更新胶囊与快速启动按钮，不动封面
  const cardStatusSig = (rs, quickBusy) =>
    JSON.stringify([rs?.text || "", rs?.tone || "", quickBusy]);

  // 运行状态胶囊：tone = playing / syncing / success / warn
  function statusPill(rs) {
    return h(
      "span",
      { class: `lib-status ${rs.tone || "playing"} sm on-cover lib-card-status` },
      h("span", { class: "lib-status-text" }, rs.text || ""),
    );
  }

  function updateCardStatus(card, g) {
    const rs = store.select.runtimeStatus(g.id);
    const quickBusy = busy.has(`launch:${g.id}`);
    const sig = cardStatusSig(rs, quickBusy);
    if (card.__ssig === sig) return;
    card.__ssig = sig;

    const coverBox = card.querySelector(".lib-card-cover");
    const oldPill = coverBox.querySelector(".lib-card-status");
    if (!rs) {
      oldPill?.remove();
    } else if (oldPill) {
      oldPill.className = `lib-status ${rs.tone || "playing"} sm on-cover lib-card-status`;
      oldPill.querySelector(".lib-status-text").textContent = rs.text || "";
    } else {
      coverBox.append(statusPill(rs));
    }

    const quick = coverBox.querySelector(".lib-quick");
    if (quick) {
      quick.classList.toggle("busy", quickBusy);
      quick.innerHTML = icon(quickBusy ? "refresh" : "play");
    }
  }

  function buildCard(g, fav, sortable) {
    const noInstallPath = !g.installPath;
    const pathsUnset = noInstallPath || !g.savePath;
    const configLabel = !pathsUnset && g.sync?.enabled === false ? "存档同步已禁用" : "";

    const coverBox = h(
      "div",
      { class: "lib-card-cover" },
      ui.coverImg(coverRef(g), "lib-card-img"),
      h("span", {
        class: "lib-card-platform",
        title: g.isSteam ? "Steam 游戏" : "第三方游戏",
        html: icon(g.isSteam ? "steam" : "monitor"),
      }),
    );
    if (configLabel) coverBox.append(h("span", { class: "badge mute lib-card-nopath" }, configLabel));
    if (!noInstallPath) {
      coverBox.append(
        h("button", {
          class: "lib-quick",
          title: "快速启动",
          "aria-label": `启动 ${g.name}`,
          html: icon("play"),
          onClick: (e) => {
            e.stopPropagation();
            runBusy(`launch:${g.id}`, () => store.actions.launchGame(g.id));
          },
        }),
      );
    }

    const card = h(
      "article",
      {
        class: `card card-hover lib-card${pathsUnset ? " lib-unset" : ""}`,
        dataset: { id: g.id },
        draggable: sortable ? "true" : null,
        onClick: () => router.navigate("game", { id: g.id }),
        onContextmenu: (e) => gameMenu(e, g),
      },
      coverBox,
      h(
        "div",
        { class: "lib-card-body" },
        h(
          "div",
          { class: "lib-card-name" },
          h("span", { class: "lib-card-name-text" }, g.name),
          fav ? iconEl("heart", "lib-heart") : null,
        ),
        h("div", { class: "lib-card-sub" }, g.lastPlayed ? fmtTime(g.lastPlayed) : "从未游玩"),
      ),
    );
    card.__csig = cardContentSig(g, fav, sortable);
    card.__coverSig = coverIdentitySig(g);
    card.__ssig = "";

    if (sortable) {
      card.addEventListener("dragstart", (e) => {
        dragId = g.id;
        dragEl = card;
        e.dataTransfer.effectAllowed = "move";
        try {
          e.dataTransfer.setData("text/plain", g.id);
        } catch {
          /* 某些 WebView 下 setData 可能受限，忽略 */
        }
        // 延迟加类，保证拖拽快照仍是原样卡片
        window.setTimeout(() => card.classList.add("lib-dragging"), 0);
      });
      card.addEventListener("dragend", () => finishDrag(card.parentElement));
    }
    return card;
  }

  /* ---------------- 网格增量同步 ---------------- */

  function syncGrid(list, sortable) {
    if (!gridEl) {
      gridEl = h("div", { class: `lib-grid${firstFill ? " stagger" : ""}` });
      bindGridDrag(gridEl);
      if (firstFill) {
        // 入场动画播完即摘掉 stagger，后续新增/换位不再重播
        window.setTimeout(() => gridEl?.classList.remove("stagger"), 800);
      }
    }
    if (gridEl.parentElement !== contentBox) {
      contentBox.innerHTML = "";
      contentBox.append(gridEl);
      emptySig = "";
    }

    const existing = new Map();
    for (const el of gridEl.children) existing.set(el.dataset.id, el);

    const favs = store.select.favoriteIds();
    const desired = [];
    for (const g of list) {
      const fav = favs.has(g.id);
      const csig = cardContentSig(g, fav, sortable);
      let el = existing.get(g.id);
      if (el && el.__csig !== csig) {
        if (el.__coverSig !== coverIdentitySig(g)) api.invalidateCover?.(g.id);
        el = null; // 内容变了：重建该卡
      }
      if (!el) {
        el = buildCard(g, fav, sortable);
        if (!firstFill) el.classList.add("lib-card-enter");
      }
      updateCardStatus(el, g);
      desired.push(el);
    }

    // 按目标顺序就位：只移动错位节点（复用节点不重建、封面不重挂）。
    // FLIP 让非拖拽来源的顺序变化（远端调序合并、排序失败回弹）也平滑滑动
    ui.flipMove(gridEl, () => {
      desired.forEach((el, i) => {
        const cur = gridEl.children[i];
        if (cur !== el) gridEl.insertBefore(el, cur || null);
      });
      while (gridEl.children.length > desired.length) gridEl.lastChild.remove();
    });

    firstFill = false;
  }

  /* ---------------- 拖拽排序（仅 全部 + 无搜索词） ---------------- */

  function bindGridDrag(grid) {
    grid.addEventListener("dragover", (e) => {
      if (!dragId || !dragEl) return;
      e.preventDefault();
      e.dataTransfer.dropEffect = "move";
      const target = e.target instanceof Element ? e.target.closest(".lib-card") : null;
      if (!target || target === dragEl || target.parentElement !== grid) return;
      const kids = [...grid.children];
      // 源在目标之前 → 插到目标之后；反之插到目标之前（实时换位）
      // FLIP：被挤开的卡片从旧位置平滑滑到新位置（拖拽占位卡本身不动画）
      ui.flipMove(
        grid,
        () => {
          if (kids.indexOf(dragEl) < kids.indexOf(target)) {
            grid.insertBefore(dragEl, target.nextSibling);
          } else {
            grid.insertBefore(dragEl, target);
          }
        },
        { exclude: dragEl },
      );
    });
    grid.addEventListener("drop", (e) => e.preventDefault());
  }

  function finishDrag(grid) {
    if (!dragId || !alive) return;
    dragId = null;
    dragEl?.classList.remove("lib-dragging");
    dragEl = null;
    const domIds = grid ? [...grid.children].map((el) => el.dataset.id).filter(Boolean) : [];
    const storeIds = store.select.games().map((g) => g.id);
    const storeSet = new Set(storeIds);

    // 拖拽期间渲染被暂停，远端目录变化会让 DOM 与 store 分叉：
    // 以 DOM 顺序为主序，滤掉远端已删除的 id，再把远端新增的 id 插回结果
    const result = domIds.filter((id) => storeSet.has(id));
    // 分叉 = 拖拽被远端增删打断（纯远端换序不算：DOM 顺序即用户意图，直接胜出）
    const diverged =
      !!grid && (result.length !== domIds.length || result.length !== storeIds.length);
    const resultSet = new Set(result);
    storeIds.forEach((id, i) => {
      if (resultSet.has(id)) return;
      // 远端新增：紧跟它在 store 中前一个仍在结果里的邻居；找不到则排队尾
      let at = result.length;
      for (let j = i - 1; j >= 0; j--) {
        const k = result.indexOf(storeIds[j]);
        if (k !== -1) {
          at = k + 1;
          break;
        }
      }
      result.splice(at, 0, id);
      resultSet.add(id);
    });

    const changed =
      result.length !== storeIds.length || result.some((id, i) => id !== storeIds[i]);
    if (changed) {
      // 成功后快照顺序与结果一致，增量同步为空操作；失败则借同一次渲染回弹
      store.actions.reorderGames(result).then(() => scheduleRender());
    } else if (dragDirty) {
      render();
    }
    if (diverged) ui.toast("列表已被其他设备更新", "info");
    dragDirty = false;
  }

  /* ---------------- 空状态 ---------------- */

  function showEmpty(sig, node) {
    if (emptySig === sig) return;
    emptySig = sig;
    gridEl = null; // 网格被移出文档流，下次列表非空时重建
    firstFill = true;
    contentBox.innerHTML = "";
    contentBox.append(node);
  }

  function emptyAll() {
    return h(
      "div",
      { class: "empty lib-empty-main" },
      h("div", { class: "empty-icon", html: icon("gamepad") }),
      h("div", { class: "empty-title" }, "游戏库还是一张白纸"),
      h(
        "div",
        { class: "empty-text" },
        "添加第一款游戏并配置存档路径，即可在多台设备间云同步进度。",
      ),
      h("button", { class: "btn btn-primary", onClick: goAdd }, iconEl("plus"), "添加游戏"),
    );
  }

  function emptySearch(q) {
    return h(
      "div",
      { class: "empty" },
      h("div", { class: "empty-icon", html: icon("search") }),
      h("div", { class: "empty-title" }, `没有找到「${q}」`),
      h("div", { class: "empty-text" }, "换个关键词试试，或检查拼写是否正确。"),
    );
  }

  function emptyFilter(filter) {
    const isFav = filter.kind === "fav";
    return h(
      "div",
      { class: "empty" },
      h("div", { class: "empty-icon", html: icon(isFav ? "heart" : "tag") }),
      h("div", { class: "empty-title" }, isFav ? "还没有常玩游戏" : "该分类暂无游戏"),
      h(
        "div",
        { class: "empty-text" },
        isFav
          ? "右键任意游戏卡片，选择「加入常玩」即可收藏到这里。"
          : `标签「${filter.tag}」下还没有游戏，可在游戏详情页为游戏添加标签。`,
      ),
    );
  }

  /* ---------------- 主渲染（增量） ---------------- */

  function render() {
    const games = store.select.games();

    // 完全无游戏：隐藏筛选行与节区标题，只留大空状态
    if (!games.length) {
      filtersBox.innerHTML = "";
      headBox.innerHTML = "";
      filtersSig = "";
      headSig = "";
      showEmpty("all-empty", emptyAll());
      return;
    }

    updateFilters();

    const filter = store.select.libraryFilter();
    const q = store.select.search().trim();
    const list = store.select.filteredGames();
    const sortable = filter.kind === "all" && !q;

    updateSectionHead(filter, q, list.length, sortable);

    if (!list.length) {
      showEmpty(
        q ? `search:${q}` : `filter:${filter.kind}:${filter.tag}`,
        q ? emptySearch(q) : emptyFilter(filter),
      );
    } else {
      syncGrid(list, sortable);
    }
  }

  render();
  const off = store.subscribe(scheduleRender);
  return () => {
    alive = false;
    dragId = null;
    dragEl = null;
    off();
  };
}
