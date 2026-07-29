// ============================================================
// views/tags.js —— 标签纸片墙
// 右键：固定到游戏库 / 查看该标签；拖拽卡片可调整标签顺序。
// 增量渲染：骨架只建一次，卡片按标签名复用节点、签名变了才重建单卡，
// 顺序修正经 FLIP 平滑滑动——调序落盘后不再整页重建（消除闪烁）。
// ============================================================

export function mount(root, ctx) {
  const { store, ui, router } = ctx;
  const { h, icon, iconEl } = ui;

  let dragName = null; // 拖拽中的标签名（期间暂停重渲染）
  let dragEl = null;
  let dragDirty = false;
  let alive = true;
  let firstFill = true; // 仅首次填充播交错入场
  let gridEl = null;

  /* ---------------- 页面骨架（只建一次） ---------------- */

  const countText = h("span", {}, "0 个标签");
  const contentBox = h("div", { class: "tags-content-slot" });
  const page = h(
    "div",
    { class: "page tags-page" },
    h(
      "header",
      { class: "tags-head" },
      h(
        "div",
        {},
        h("h1", { class: "tags-title" }, "标签"),
        h("p", { class: "tags-sub" }, "右键可固定到游戏库筛选行 · 拖拽卡片可调整顺序"),
      ),
      h("div", { class: "tags-count" }, iconEl("tag"), countText),
    ),
    contentBox,
  );
  root.append(page);

  /* ---------------- 右键菜单 ---------------- */

  function tagMenu(e, tag) {
    const cardEl = e.currentTarget;
    // 先开菜单（内部关旧菜单触发其 onClose）再钉住，避免连续右键时钉住类被摘掉
    ui.contextMenu(e, [
      {
        label: tag.pinned ? "取消固定" : "固定到游戏库",
        icon: "pin",
        onClick: () => store.actions.pinTag(tag.name, !tag.pinned),
      },
      {
        label: "查看该标签的游戏",
        icon: "gamepad",
        onClick: () => {
          store.actions.setLibraryFilter({ kind: "tag", tag: tag.name });
          router.navigate("library");
        },
      },
    ], { onClose: () => cardEl.classList.remove("menu-pin") });
    cardEl.classList.add("menu-pin");
  }

  /* ---------------- 卡片 ---------------- */

  const cardSig = (t) => JSON.stringify([t.name, t.count, t.pinned]);

  function buildCard(tag) {
    const card = h(
      "article",
      {
        class: `card card-hover tags-card${tag.pinned ? " pinned" : ""}`,
        dataset: { tag: tag.name },
        draggable: "true",
        onClick: () => {
          store.actions.setLibraryFilter({ kind: "tag", tag: tag.name });
          router.navigate("library");
        },
        onContextmenu: (e) => tagMenu(e, tag),
      },
      h("div", { class: "tags-card-glyph", html: icon("tag") }),
      h(
        "div",
        { class: "tags-card-main" },
        h("div", { class: "tags-card-name" }, tag.name, tag.pinned ? iconEl("pin", "tags-pin-mark") : null),
        h("div", { class: "tags-card-meta" }, `${tag.count} 款游戏`),
      ),
      h("div", { class: "tags-card-num" }, String(tag.count)),
    );
    card.__sig = cardSig(tag);
    card.addEventListener("dragstart", (e) => {
      dragName = tag.name;
      dragEl = card;
      e.dataTransfer.effectAllowed = "move";
      try {
        e.dataTransfer.setData("text/plain", tag.name);
      } catch {
        /* 某些 WebView 下 setData 可能受限，忽略 */
      }
      window.setTimeout(() => card.classList.add("tags-dragging"), 0);
    });
    card.addEventListener("dragend", () => finishDrag(card.parentElement));
    return card;
  }

  /* ---------------- 网格增量同步 ---------------- */

  function syncGrid(tags) {
    if (!gridEl) {
      gridEl = h("div", { class: `tags-grid${firstFill ? " stagger" : ""}` });
      bindGridDrag(gridEl);
      if (firstFill) {
        window.setTimeout(() => gridEl?.classList.remove("stagger"), 800);
      }
    }
    if (gridEl.parentElement !== contentBox) {
      contentBox.innerHTML = "";
      contentBox.append(gridEl);
    }

    const existing = new Map();
    for (const el of gridEl.children) existing.set(el.dataset.tag, el);

    const desired = [];
    for (const tag of tags) {
      let el = existing.get(tag.name);
      if (el && el.__sig !== cardSig(tag)) el = null; // 数量/固定态变了：重建该卡
      if (!el) {
        el = buildCard(tag);
        if (!firstFill) el.classList.add("tags-card-enter");
      }
      desired.push(el);
    }

    // 顺序就位走 FLIP：拖拽落盘后 DOM 已是目标顺序（零操作零闪烁），
    // 远端调序/回弹则平滑滑动归位
    ui.flipMove(gridEl, () => {
      desired.forEach((el, i) => {
        const cur = gridEl.children[i];
        if (cur !== el) gridEl.insertBefore(el, cur || null);
      });
      while (gridEl.children.length > desired.length) gridEl.lastChild.remove();
    });

    firstFill = false;
  }

  /* ---------------- 拖拽排序 ---------------- */

  function bindGridDrag(grid) {
    grid.addEventListener("dragover", (e) => {
      if (!dragName || !dragEl) return;
      e.preventDefault();
      e.dataTransfer.dropEffect = "move";
      const target = e.target instanceof Element ? e.target.closest(".tags-card") : null;
      if (!target || target === dragEl || target.parentElement !== grid) return;
      const kids = [...grid.children];
      // FLIP：被挤开的标签卡平滑滑到新位置（拖拽占位卡本身不动画）
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
    if (!dragName || !alive) return;
    dragName = null;
    dragEl?.classList.remove("tags-dragging");
    dragEl = null;
    const names = grid ? [...grid.children].map((el) => el.dataset.tag).filter(Boolean) : [];
    const before = store.select.tagSummaries().map((t) => t.name);
    const changed =
      names.length === before.length && names.some((n, i) => n !== before[i]);
    if (changed) {
      // 成功后快照顺序与 DOM 一致（增量同步零操作）；失败则借这次渲染平滑回弹
      store.actions.reorderTags(names).then(() => {
        if (alive && !dragName) render();
      });
    } else if (dragDirty) {
      render();
    }
    dragDirty = false;
  }

  /* ---------------- 渲染（增量） ---------------- */

  function render() {
    const tags = store.select.tagSummaries();
    countText.textContent = `${tags.length} 个标签`;

    if (!tags.length) {
      gridEl = null;
      firstFill = true;
      contentBox.innerHTML = "";
      contentBox.append(
        h(
          "div",
          { class: "empty" },
          h("div", { class: "empty-icon", html: icon("tag") }),
          h("div", { class: "empty-title" }, "还没有任何标签"),
          h("div", { class: "empty-text" }, "在游戏详情页为游戏添加标签后，这里会自动汇总。"),
        ),
      );
      return;
    }

    syncGrid(tags);
  }

  render();
  // 渲染签名：标签汇总没变时跳过；拖拽期间暂停（增量渲染本身已很轻，签名再省一层）
  let renderedSig = JSON.stringify(store.select.tagSummaries());
  const off = store.subscribe(() => {
    if (!alive) return;
    const sig = JSON.stringify(store.select.tagSummaries());
    if (sig === renderedSig) return;
    renderedSig = sig;
    if (dragName) {
      dragDirty = true;
      return;
    }
    render();
  });
  return () => {
    alive = false;
    dragName = null;
    dragEl = null;
    off();
  };
}
