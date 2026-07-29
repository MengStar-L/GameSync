// ============================================================
// main.js —— 启动：窗控 / chrome 导航 / 路由注册 / Bootstrap / 事件
// ============================================================

import { api } from "./api.js";
import { store } from "./store.js";
import { router } from "./router.js";
import { ui, toast } from "./ui.js";

import { mount as mountLibrary } from "./views/library.js";
import { mount as mountGame } from "./views/game.js";
import { mount as mountTags } from "./views/tags.js";
import { mount as mountAccounts } from "./views/accounts.js";
import { mount as mountActivity } from "./views/activity.js";
import { mount as mountSettings } from "./views/settings.js";
import { mount as mountWelcome } from "./views/welcome.js";

/* ---------------- 窗控 ---------------- */

function bindWindowControls() {
  document.getElementById("wc-min")?.addEventListener("click", () => api.window.minimise());
  document.getElementById("wc-max")?.addEventListener("click", () => {
    api.window.toggleMaximise();
    window.setTimeout(syncMaximized, 120);
  });
  document.getElementById("wc-close")?.addEventListener("click", () => api.window.hide());

  function syncMaximized() {
    Promise.resolve(api.window.isMaximised())
      .then((m) => document.body.classList.toggle("is-maximized", Boolean(m)))
      .catch(() => {});
  }
  let t = 0;
  window.addEventListener("resize", () => {
    window.clearTimeout(t);
    t = window.setTimeout(syncMaximized, 120);
  });
  syncMaximized();
}

/* ---------------- chrome：导航页签 + 墨条 ---------------- */

const NAV_PAGES = ["library", "tags", "accounts", "activity", "settings"];

function bindChromeNav() {
  const nav = document.getElementById("chrome-nav");
  const inkBar = document.getElementById("chrome-tab-ink");

  nav.addEventListener("click", (e) => {
    const tab = e.target.closest(".chrome-tab");
    if (tab?.dataset.page) router.navigate(tab.dataset.page);
  });

  const updateInk = (page) => {
    const active = nav.querySelector(`.chrome-tab[data-page="${page}"]`);
    nav.querySelectorAll(".chrome-tab").forEach((t) => t.classList.toggle("active", t === active));
    if (active) {
      inkBar.style.left = `${active.offsetLeft + 6}px`;
      inkBar.style.width = `${active.offsetWidth - 12}px`;
      inkBar.style.opacity = "1";
    } else {
      inkBar.style.opacity = "0";
    }
  };

  router.onChange(({ page }) => {
    // 详情页归属游戏库页签
    updateInk(page === "game" ? "library" : NAV_PAGES.includes(page) ? page : "");
  });
  window.addEventListener("resize", () => updateInk(router.current.page === "game" ? "library" : router.current.page));
}

/* ---------------- chrome：搜索 / 同步全部 / 状态 ---------------- */

function bindChromeTools() {
  const box = document.getElementById("chrome-search");
  const input = document.getElementById("chrome-search-input");
  const clearBtn = document.getElementById("chrome-search-clear");

  input.addEventListener("input", () => {
    box.classList.toggle("has-value", Boolean(input.value));
    store.actions.setSearch(input.value);
    if (input.value && !["library"].includes(router.current.page)) router.navigate("library");
  });
  clearBtn.addEventListener("click", () => {
    input.value = "";
    box.classList.remove("has-value");
    store.actions.setSearch("");
    input.focus();
  });
  window.addEventListener("keydown", (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key === "f") {
      e.preventDefault();
      input.focus();
      input.select();
    }
  });

  const syncBtn = document.getElementById("chrome-sync-all");
  syncBtn.addEventListener("click", () => store.actions.syncAll());

  const net = document.getElementById("chrome-net");
  const netText = document.getElementById("chrome-net-text");
  store.subscribe(() => {
    const { state: netState, message } = store.select.netStatus();
    net.className = `chrome-net ${netState}`;
    net.title = message || "";
    netText.textContent =
      {
        online: "已连接",
        offline: "离线",
        syncing: "同步中",
        checking: "检测中",
        retrying: "重试中",
        pending: "等待中",
        degraded: "部分可用",
        // 后端 catalog:sync_state 词表：queued/syncing/retrying/succeeded
        queued: "排队中",
        succeeded: "已同步",
      }[netState] || "状态未知";
    syncBtn.classList.toggle("busy", store.select.syncingAll());
  });
}

/* ---------------- 启动 ---------------- */

async function boot() {
  bindWindowControls();
  bindChromeNav();
  bindChromeTools();

  const ctx = { store, api, ui, router };
  router.register(
    {
      library: mountLibrary,
      game: mountGame,
      tags: mountTags,
      accounts: mountAccounts,
      activity: mountActivity,
      settings: mountSettings,
      welcome: mountWelcome,
    },
    ctx,
    document.getElementById("view"),
  );

  store.actions.bindBackendEvents();

  if (api.isMock) {
    console.info("[GameSync] mock 模式：浏览器试驾，数据为演示样例");
    // 浏览器控制台调试钩子（仅 mock 模式暴露）
    window.__gs = { store, api, router };
  }

  try {
    const [first] = await Promise.all([
      api.IsFirstLaunch().catch(() => false),
      store.actions.boot(),
    ]);
    router.navigate(first ? "welcome" : "library", {}, { push: false });
  } catch (e) {
    console.error(e);
    toast(`初始化失败：${e?.message || e}`, "err");
    router.navigate("library", {}, { push: false });
  }

  // rAF 在隐藏页签下会被冻结，用定时器兜底保证帷幕一定揭开
  let unveiled = false;
  const unveil = () => {
    if (!unveiled) {
      unveiled = true;
      document.body.classList.remove("booting");
    }
  };
  requestAnimationFrame(() => requestAnimationFrame(unveil));
  window.setTimeout(unveil, 400);
}

boot();
