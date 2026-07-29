// ============================================================
// ui.js —— DOM 工具 / 图标 / toast / 对话框 / 抽屉 / 右键菜单 /
//           封面解析 / 格式化。视图禁止自造同类轮子。
// ============================================================

import { api } from "./api.js";

/* ---------------- DOM 构建 ---------------- */

export function h(tag, attrs = {}, ...children) {
  const el = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs || {})) {
    if (value == null || value === false) continue;
    if (key === "class") el.className = value;
    else if (key === "html") el.innerHTML = value;
    else if (key === "dataset") Object.assign(el.dataset, value);
    else if (key === "style" && typeof value === "object") Object.assign(el.style, value);
    else if (key.startsWith("on") && typeof value === "function") {
      el.addEventListener(key.slice(2).toLowerCase(), value);
    } else if (value === true) el.setAttribute(key, "");
    else el.setAttribute(key, value);
  }
  for (const child of children.flat(Infinity)) {
    if (child == null || child === false) continue;
    el.append(child.nodeType ? child : document.createTextNode(String(child)));
  }
  return el;
}

export function esc(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

/* ---------------- 图标（lucide 风格 24×24 描边） ---------------- */

const ICONS = {
  gamepad: '<line x1="6" x2="10" y1="12" y2="12"/><line x1="8" x2="8" y1="10" y2="14"/><line x1="15" x2="15.01" y1="13" y2="13"/><line x1="18" x2="18.01" y1="11" y2="11"/><rect width="20" height="12" x="2" y="6" rx="6"/>',
  heart: '<path d="M19 14c1.49-1.46 3-3.21 3-5.5A5.5 5.5 0 0 0 16.5 3c-1.76 0-3 .5-4.5 2-1.5-1.5-2.74-2-4.5-2A5.5 5.5 0 0 0 2 8.5c0 2.3 1.5 4.05 3 5.5l7 7Z"/>',
  tag: '<path d="M12.586 2.586A2 2 0 0 0 11.172 2H4a2 2 0 0 0-2 2v7.172a2 2 0 0 0 .586 1.414l8.704 8.704a2.426 2.426 0 0 0 3.42 0l6.58-6.58a2.426 2.426 0 0 0 0-3.42z"/><circle cx="7.5" cy="7.5" r=".5" fill="currentColor"/>',
  cloud: '<path d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"/>',
  activity: '<polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/>',
  sliders: '<line x1="21" x2="14" y1="4" y2="4"/><line x1="10" x2="3" y1="4" y2="4"/><line x1="21" x2="12" y1="12" y2="12"/><line x1="8" x2="3" y1="12" y2="12"/><line x1="21" x2="16" y1="20" y2="20"/><line x1="12" x2="3" y1="20" y2="20"/><line x1="14" x2="14" y1="2" y2="6"/><line x1="8" x2="8" y1="10" y2="14"/><line x1="16" x2="16" y1="18" y2="22"/>',
  search: '<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>',
  refresh: '<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/>',
  play: '<polygon points="6 3 20 12 6 21 6 3"/>',
  plus: '<path d="M5 12h14"/><path d="M12 5v14"/>',
  x: '<path d="M18 6 6 18"/><path d="m6 6 12 12"/>',
  check: '<path d="M20 6 9 17l-5-5"/>',
  chevronLeft: '<path d="m15 18-6-6 6-6"/>',
  chevronRight: '<path d="m9 18 6-6-6-6"/>',
  chevronDown: '<path d="m6 9 6 6 6-6"/>',
  trash: '<path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/><line x1="10" x2="10" y1="11" y2="17"/><line x1="14" x2="14" y1="11" y2="17"/>',
  pencil: '<path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"/><path d="m15 5 4 4"/>',
  pin: '<path d="M12 17v5"/><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V6h1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4h1z"/>',
  archive: '<rect width="20" height="5" x="2" y="3" rx="1"/><path d="M4 8v11a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8"/><path d="M10 12h4"/>',
  download: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" x2="12" y1="15" y2="3"/>',
  upload: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" x2="12" y1="3" y2="15"/>',
  cloudUpload: '<path d="M12 13v8"/><path d="M4 14.899A7 7 0 1 1 15.71 8h1.79a4.5 4.5 0 0 1 2.5 8.242"/><path d="m8 17 4-4 4 4"/>',
  folder: '<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>',
  folderOpen: '<path d="m6 14 1.5-2.9A2 2 0 0 1 9.24 10H20a2 2 0 0 1 1.94 2.5l-1.54 6a2 2 0 0 1-1.95 1.5H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h3.9a2 2 0 0 1 1.69.9l.81 1.2a2 2 0 0 0 1.67.9H18a2 2 0 0 1 2 2v2"/>',
  image: '<rect width="18" height="18" x="3" y="3" rx="2" ry="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21"/>',
  info: '<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>',
  alert: '<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><path d="M12 9v4"/><path d="M12 17h.01"/>',
  clock: '<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>',
  calendar: '<path d="M8 2v4"/><path d="M16 2v4"/><rect width="18" height="18" x="3" y="4" rx="2"/><path d="M3 10h18"/>',
  star: '<polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/>',
  sparkles: '<path d="M9.937 15.5A2 2 0 0 0 8.5 14.063l-6.135-1.582a.5.5 0 0 1 0-.962L8.5 9.936A2 2 0 0 0 9.937 8.5l1.582-6.135a.5.5 0 0 1 .963 0L14.063 8.5A2 2 0 0 0 15.5 9.937l6.135 1.581a.5.5 0 0 1 0 .964L15.5 14.063a2 2 0 0 0-1.437 1.437l-1.582 6.135a.5.5 0 0 1-.963 0z"/>',
  eye: '<path d="M2.062 12.348a1 1 0 0 1 0-.696 10.75 10.75 0 0 1 19.876 0 1 1 0 0 1 0 .696 10.75 10.75 0 0 1-19.876 0"/><circle cx="12" cy="12" r="3"/>',
  eyeOff: '<path d="M10.733 5.076a10.744 10.744 0 0 1 11.205 6.575 1 1 0 0 1 0 .696 10.747 10.747 0 0 1-1.444 2.49"/><path d="M14.084 14.158a3 3 0 0 1-4.242-4.242"/><path d="M17.479 17.499a10.75 10.75 0 0 1-15.417-5.151 1 1 0 0 1 0-.696 10.75 10.75 0 0 1 4.446-5.143"/><path d="m2 2 20 20"/>',
  externalLink: '<path d="M15 3h6v6"/><path d="M10 14 21 3"/><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>',
  hardDrive: '<line x1="22" x2="2" y1="12" y2="12"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/><line x1="6" x2="6.01" y1="16" y2="16"/><line x1="10" x2="10.01" y1="16" y2="16"/>',
  database: '<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5V19A9 3 0 0 0 21 19V5"/><path d="M3 12A9 3 0 0 0 21 12"/>',
  key: '<path d="m15.5 7.5 2.3 2.3a1 1 0 0 0 1.4 0l2.1-2.1a1 1 0 0 0 0-1.4L19 4"/><path d="m21 2-9.6 9.6"/><circle cx="7.5" cy="15.5" r="5.5"/>',
  shield: '<path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"/>',
  rocket: '<path d="M4.5 16.5c-1.5 1.26-2 5-2 5s3.74-.5 5-2c.71-.84.7-2.13-.09-2.91a2.18 2.18 0 0 0-2.91-.09z"/><path d="m12 15-3-3a22 22 0 0 1 2-3.95A12.88 12.88 0 0 1 22 2c0 2.72-.78 7.5-6 11a22.35 22.35 0 0 1-4 2z"/><path d="M9 12H4s.55-3.03 2-4c1.62-1.08 5 0 5 0"/><path d="M12 15v5s3.03-.55 4-2c1.08-1.62 0-5 0-5"/>',
  moreH: '<circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/><circle cx="5" cy="12" r="1"/>',
  grip: '<circle cx="9" cy="12" r="1"/><circle cx="9" cy="5" r="1"/><circle cx="9" cy="19" r="1"/><circle cx="15" cy="12" r="1"/><circle cx="15" cy="5" r="1"/><circle cx="15" cy="19" r="1"/>',
  login: '<path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4"/><polyline points="10 17 15 12 10 7"/><line x1="15" x2="3" y1="12" y2="12"/>',
  copy: '<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>',
  steam: '<circle cx="12" cy="12" r="10"/><circle cx="15.8" cy="8.6" r="2.2"/><circle cx="8.6" cy="15.6" r="1.9"/><path d="m10.2 14.2 3.6-4.2"/>',
  monitor: '<rect width="20" height="14" x="2" y="3" rx="2"/><line x1="8" x2="16" y1="21" y2="21"/><line x1="12" x2="12" y1="17" y2="21"/>',
  file: '<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/>',
  history: '<path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/><path d="M12 7v5l4 2"/>',
};

export function icon(name, cls = "") {
  const body = ICONS[name] || ICONS.info;
  return `<svg class="${cls}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${body}</svg>`;
}

export function iconEl(name, cls = "") {
  const tpl = document.createElement("template");
  tpl.innerHTML = icon(name, cls);
  return tpl.content.firstChild;
}

/* ---------------- 格式化 ---------------- */

export function fmtTime(iso) {
  if (!iso) return "从未";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "从未";
  const now = new Date();
  const diffMs = now - date;
  const min = Math.floor(diffMs / 60000);
  if (min < 1) return "刚刚";
  if (min < 60) return `${min} 分钟前`;
  const sameDay = date.toDateString() === now.toDateString();
  const hh = String(date.getHours()).padStart(2, "0");
  const mm = String(date.getMinutes()).padStart(2, "0");
  if (sameDay) return `今天 ${hh}:${mm}`;
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (date.toDateString() === yesterday.toDateString()) return `昨天 ${hh}:${mm}`;
  const y = date.getFullYear();
  const mo = String(date.getMonth() + 1).padStart(2, "0");
  const d = String(date.getDate()).padStart(2, "0");
  return y === now.getFullYear() ? `${mo}-${d} ${hh}:${mm}` : `${y}-${mo}-${d}`;
}

export function fmtDuration(minutes) {
  const min = Math.max(Number(minutes) || 0, 0);
  if (min < 1) return "不足 1 分钟";
  if (min < 60) return `${Math.floor(min)} 分钟`;
  const hours = min / 60;
  if (hours < 100) return `${hours.toFixed(1)} 小时`;
  return `${Math.round(hours)} 小时`;
}

export function fmtBytes(n) {
  const num = Number(n) || 0;
  if (num <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(Math.floor(Math.log2(num) / 10), units.length - 1);
  const v = num / 2 ** (10 * i);
  return `${v >= 100 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

/* ---------------- toast ---------------- */

export function toast(message, tone = "ok") {
  const rootEl = document.getElementById("layer-toast");
  if (!rootEl) return;
  // 相同文案还在屏上时不重复堆叠
  const text = String(message ?? "");
  for (const existing of rootEl.children) {
    if (existing.dataset.msg === text && !existing.classList.contains("leaving")) return;
  }
  while (rootEl.children.length >= 3) rootEl.firstChild.remove();
  const item = h(
    "div",
    { class: `toast-item ${tone}`, dataset: { msg: text } },
    h("span", { class: "toast-dot" }),
    h("span", {}, text),
  );
  rootEl.append(item);
  window.setTimeout(() => {
    item.classList.add("leaving");
    window.setTimeout(() => item.remove(), 260);
  }, 3200);
}

/* ---------------- overlay 基座（dialog / drawer 共用） ---------------- */

// 打开中的 overlay 栈：Escape 只作用于栈顶，避免一次按键连关全部叠层
const overlayStack = [];

function openOverlay({ mode = "dialog", width, title, render, onClose }) {
  const layer = document.getElementById("layer-overlay");
  const overlay = h("div", { class: `overlay${mode === "drawer" ? " drawer-mode" : ""}` });
  const panel = h("div", { class: mode === "drawer" ? "drawer" : "dialog" });
  if (width) panel.style.width = typeof width === "number" ? `${width}px` : width;

  let closed = false;
  const close = (result) => {
    if (closed) return;
    closed = true;
    const depth = overlayStack.indexOf(overlay);
    if (depth !== -1) overlayStack.splice(depth, 1);
    overlay.classList.remove("open");
    document.removeEventListener("keydown", onKey);
    window.setTimeout(() => overlay.remove(), 260);
    onClose?.(result);
  };
  const onKey = (e) => {
    if (e.key !== "Escape") return;
    // 仅栈顶响应；截断后续 document 级监听，防止同一次按键穿透到底层 overlay
    if (overlayStack[overlayStack.length - 1] !== overlay) return;
    e.stopImmediatePropagation();
    close();
  };

  const head = h(
    "div",
    { class: "dialog-head" },
    h("h2", { class: "dialog-title" }, title || ""),
    h("button", { class: "dialog-close", "aria-label": "关闭", onClick: () => close(), html: icon("x") }),
  );
  const body = h("div", { class: "dialog-body" });
  panel.append(head, body);
  overlay.append(panel);
  overlay.addEventListener("mousedown", (e) => {
    if (e.target === overlay) close();
  });
  document.addEventListener("keydown", onKey);
  overlayStack.push(overlay);
  layer.append(overlay);

  render?.(body, close, panel);
  requestAnimationFrame(() => requestAnimationFrame(() => overlay.classList.add("open")));
  return { close, body, panel };
}

export function dialog(opts) {
  return openOverlay({ ...opts, mode: "dialog" });
}

export function drawer(opts) {
  return openOverlay({ ...opts, mode: "drawer", width: opts.width || 520 });
}

export function confirm({ message, confirmText = "确定", cancelText = "取消", tone = "warn" } = {}) {
  return new Promise((resolve) => {
    let settled = false;
    const settle = (v, close) => {
      if (!settled) {
        settled = true;
        resolve(v);
      }
      close?.();
    };
    openOverlay({
      mode: "dialog",
      width: 400,
      title: "",
      onClose: () => settle(false),
      render(body, close, panel) {
        panel.querySelector(".dialog-head").style.display = "none";
        body.style.padding = "26px 26px 22px";
        body.append(
          h("div", {
            class: `confirm-glyph ${tone}`,
            html: icon(tone === "danger" ? "trash" : "alert"),
          }),
          h("div", { class: "confirm-text" }, String(message ?? "确认操作？")),
          h(
            "div",
            { class: "confirm-btns" },
            h("button", { class: "btn", onClick: () => settle(false, close) }, cancelText),
            h(
              "button",
              {
                class: tone === "danger" ? "btn btn-danger" : "btn btn-primary",
                onClick: () => settle(true, close),
              },
              confirmText,
            ),
          ),
        );
      },
    });
  });
}

/* 三选对话框（冲突处理：保留本地 / 保留云端 / 取消）→ 'local'|'remote'|null
   opts.gameName：标题带上游戏名，syncAll 连续弹框时用户才能分清对象；单参旧调用兼容 */
export function conflictDialog(message, opts = {}) {
  const gameName = opts?.gameName;
  return new Promise((resolve) => {
    let settled = false;
    const settle = (v, close) => {
      if (!settled) {
        settled = true;
        resolve(v);
      }
      close?.();
    };
    openOverlay({
      mode: "dialog",
      width: 460,
      title: gameName ? `「${gameName}」存在同步冲突` : "检测到同步冲突",
      onClose: () => settle(null),
      render(body, close) {
        body.append(
          h("p", { class: "conflict-msg" }, message || "本地与云端都发生了修改，请选择保留哪一侧。"),
          h(
            "div",
            { class: "conflict-btns" },
            h("button", { class: "btn btn-ghost", onClick: () => settle(null, close) }, "取消"),
            h(
              "div",
              { class: "conflict-btns-main" },
              h("button", { class: "btn", onClick: () => settle("remote", close) }, [iconEl("cloud"), "保留云端"]),
              h("button", { class: "btn btn-primary", onClick: () => settle("local", close) }, [iconEl("hardDrive"), "保留本地"]),
            ),
          ),
        );
      },
    });
  });
}

/* ---------------- 右键菜单 ---------------- */

let activeMenuCleanup = null;

/**
 * 右键菜单。opts.onClose 在菜单以任何方式关闭时回调一次
 * （用于恢复被菜单钉住的悬停态等）。
 */
export function contextMenu(event, items, opts = {}) {
  event.preventDefault();
  closeContextMenu();
  const layer = document.getElementById("layer-menu");
  const menu = h("div", { class: "menu", role: "menu" });
  for (const item of items) {
    if (item === "divider") {
      menu.append(h("div", { class: "menu-divider" }));
      continue;
    }
    menu.append(
      h(
        "button",
        {
          class: `menu-item${item.danger ? " danger" : ""}`,
          role: "menuitem",
          onClick: () => {
            closeContextMenu();
            item.onClick?.();
          },
        },
        item.icon ? iconEl(item.icon) : null,
        item.label,
      ),
    );
  }
  layer.append(menu);
  const { innerWidth, innerHeight } = window;
  const rect = menu.getBoundingClientRect();
  const x = Math.min(event.clientX, innerWidth - rect.width - 8);
  const y = Math.min(event.clientY, innerHeight - rect.height - 8);
  menu.style.left = `${Math.max(8, x)}px`;
  menu.style.top = `${Math.max(8, y)}px`;

  const dismiss = (e) => {
    // 菜单内部的按下不能关闭菜单：mousedown 就移除菜单会让菜单项的 click 永远无法触发
    if (e?.type === "mousedown" && menu.contains(e.target)) return;
    closeContextMenu();
  };
  const onKey = (e) => {
    if (e.key === "Escape") closeContextMenu();
  };
  window.setTimeout(() => {
    document.addEventListener("mousedown", dismiss, { capture: true });
    document.addEventListener("keydown", onKey);
    window.addEventListener("blur", dismiss);
  }, 0);
  activeMenuCleanup = () => {
    menu.remove();
    document.removeEventListener("mousedown", dismiss, { capture: true });
    document.removeEventListener("keydown", onKey);
    window.removeEventListener("blur", dismiss);
    try {
      opts.onClose?.();
    } catch (e) {
      console.error(e);
    }
  };
}

export function closeContextMenu() {
  activeMenuCleanup?.();
  activeMenuCleanup = null;
}

/* ---------------- 封面 ---------------- */

const FALLBACK_HTML = icon("gamepad");

/**
 * 封面 <img> 懒解析：ref 为 http/data 直链或 game.id。
 * 失败/为空时替换为纸纹占位块（保留同尺寸）。
 */
export function coverImg(ref, cls = "") {
  const wrap = h("div", { class: `cover ${cls}` });
  if (!ref) {
    wrap.classList.add("cover-fallback");
    wrap.innerHTML = FALLBACK_HTML;
    return wrap;
  }
  const img = h("img", { alt: "", draggable: "false", loading: "lazy" });
  wrap.append(img);
  api
    .resolveCover(ref)
    .then((src) => {
      if (!src) throw new Error("empty cover");
      img.src = src;
      img.addEventListener("load", () => wrap.classList.add("cover-loaded"), { once: true });
      img.addEventListener(
        "error",
        () => {
          wrap.classList.add("cover-fallback");
          wrap.innerHTML = FALLBACK_HTML;
        },
        { once: true },
      );
    })
    .catch(() => {
      wrap.classList.add("cover-fallback");
      wrap.innerHTML = FALLBACK_HTML;
    });
  return wrap;
}

export function skeleton(cls = "") {
  return h("div", { class: `skel ${cls}` });
}

/* ---------------- FLIP 位移动画 ---------------- */

/**
 * 对容器执行一次会移动子节点的 DOM 变更（mutate 回调），
 * 并让被移动的子节点从旧位置平滑滑到新位置（First-Last-Invert-Play）。
 * - exclude：不参与动画的节点（如拖拽占位卡，位置变化是用户显式指定的）
 * - 变更前读取的是节点当前视觉位置（含在途动画的 transform），
 *   连续快速换位时动画从当前位置无缝接续，不会跳回
 * - 动画中的节点带 .flip-moving（pointer-events:none），
 *   避免滑动经过光标下方时反复触发 dragover 造成来回抖动
 */
export function flipMove(container, mutate, { exclude = null, duration = 220 } = {}) {
  const before = new Map();
  for (const el of container.children) {
    if (el === exclude) continue;
    before.set(el, el.getBoundingClientRect());
  }

  mutate();

  for (const el of container.children) {
    if (el === exclude) continue;
    const prev = before.get(el);
    if (!prev) continue; // 新插入的节点走各自的入场动画
    // 清掉在途动画后测量纯布局位置（同帧内完成，不会闪烁）
    el.style.transition = "none";
    el.style.transform = "";
    const next = el.getBoundingClientRect();
    const dx = prev.left - next.left;
    const dy = prev.top - next.top;
    if (Math.abs(dx) < 0.5 && Math.abs(dy) < 0.5) {
      el.style.transition = "";
      continue;
    }
    el.style.transform = `translate(${dx}px, ${dy}px)`;
    el.classList.add("flip-moving");
    void el.offsetHeight; // 提交起始态
    el.style.transition = `transform ${duration}ms cubic-bezier(0.22, 1, 0.36, 1)`;
    el.style.transform = "";
    const done = () => {
      el.style.transition = "";
      el.classList.remove("flip-moving");
      el.removeEventListener("transitionend", done);
    };
    el.addEventListener("transitionend", done);
    window.setTimeout(done, duration + 80); // transitionend 可能因中断丢失，兜底清理
  }
}

export const ui = {
  h,
  esc,
  icon,
  iconEl,
  fmtTime,
  fmtDuration,
  fmtBytes,
  toast,
  dialog,
  drawer,
  confirm,
  conflictDialog,
  contextMenu,
  closeContextMenu,
  coverImg,
  skeleton,
  flipMove,
};
