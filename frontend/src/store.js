// ============================================================
// store.js —— 状态容器 + 订阅 + 业务动作。
// 视图只消费 select/actions，不直接触达 api（弹窗流程除外均在此处理）。
// ============================================================

import { api } from "./api.js";
import { toast, confirm, conflictDialog } from "./ui.js";
import { deriveSyncStatus } from "./sync-state.js";

const state = {
  snapshot: null, // DashboardSnapshot
  runtimeStatus: {}, // { [gameId]: { text, tone: 'playing'|'syncing'|'success'|'warn' } }
  netStatus: { state: "checking", message: "检测中" },
  syncTasks: { catalog: { status: "checking", message: "检测中" }, covers: {}, saves: {} },
  search: "",
  libraryFilter: { kind: "all", tag: "" }, // 'all' | 'fav' | 'tag'
  pendingDeletes: new Set(), // 乐观隐藏中的游戏
  syncingAll: false,
  booted: false,
};

/* ---------------- 订阅（微任务合并） ---------------- */

const subscribers = new Set();
let notifyQueued = false;

function notify() {
  if (notifyQueued) return;
  notifyQueued = true;
  queueMicrotask(() => {
    notifyQueued = false;
    subscribers.forEach((fn) => {
      try {
        fn(state);
      } catch (e) {
        console.error(e);
      }
    });
  });
}

function subscribe(fn) {
  subscribers.add(fn);
  return () => subscribers.delete(fn);
}

/* ---------------- 内部工具 ---------------- */

const appState = () => state.snapshot?.state || {};
const prefs = () => appState().preferences || {};

function applySnapshot(snap) {
  if (!snap) return;
  state.snapshot = snap;
  notify();
}

function applyAppState(appStateNext) {
  if (!state.snapshot || !appStateNext) return;
  state.snapshot = { ...state.snapshot, state: appStateNext };
  notify();
}

function setRuntimeStatus(gameId, status) {
  if (!gameId) return;
  const prev = state.runtimeStatus[gameId];
  // 相同状态不重复通知，避免高频事件反复触发视图刷新
  if (
    status &&
    prev &&
    prev.text === status.text &&
    prev.tone === status.tone &&
    prev.source === status.source &&
    prev.detail === status.detail
  ) return;
  if (!status && !prev) return;
  if (status) state.runtimeStatus[gameId] = status;
  else delete state.runtimeStatus[gameId];
  notify();
}

function setNet(netState, message) {
  const prev = state.netStatus;
  const msg = message || "";
  if (prev.state === netState && prev.message === msg) return;
  state.netStatus = { state: netState, message: msg };
  notify();
}

function recomputeSyncStatus() {
  const next = deriveSyncStatus(state.syncTasks);
  setNet(next.state, next.message);
}

function setCatalogSyncStatus(status, message) {
  state.syncTasks.catalog = { status: status || "checking", message: message || "" };
  recomputeSyncStatus();
}

function setSaveSyncStatus(gameId, status, message = "") {
  if (!gameId) return;
  if (status) state.syncTasks.saves[gameId] = { status, message };
  else delete state.syncTasks.saves[gameId];
  recomputeSyncStatus();
}

function setCoverSyncStatus(payload) {
  const gameId = eventGameId(payload);
  if (!gameId) return;
  const status = payload?.status || "skipped";
  const message = payload?.message || "";
  if (status === "skipped") {
    delete state.syncTasks.covers[gameId];
    if (state.runtimeStatus[gameId]?.source === "cover") setRuntimeStatus(gameId, null);
    recomputeSyncStatus();
    return;
  }

  state.syncTasks.covers[gameId] = { status, message };
  if (status === "syncing") {
    setRuntimeStatus(gameId, { text: message || "正在同步游戏封面", tone: "syncing", source: "cover" });
  } else if (status === "pending") {
    setRuntimeStatus(gameId, { text: "封面等待重试", detail: message, tone: "warn", source: "cover" });
  } else if (status === "succeeded") {
    setRuntimeStatus(gameId, { text: message || "封面同步完成", tone: "success", source: "cover" });
    window.setTimeout(() => {
      if (state.syncTasks.covers[gameId]?.status === "succeeded") delete state.syncTasks.covers[gameId];
      if (state.runtimeStatus[gameId]?.source === "cover" && state.runtimeStatus[gameId]?.tone === "success") {
        setRuntimeStatus(gameId, null);
      }
      recomputeSyncStatus();
    }, 3000);
  }
  recomputeSyncStatus();
}

function hasPendingCover(gameId) {
  return state.syncTasks.covers[gameId]?.status === "pending";
}

function restorePendingCoverStatus(gameId) {
  if (!hasPendingCover(gameId)) return false;
  setRuntimeStatus(gameId, {
    text: "封面等待重试",
    detail: state.syncTasks.covers[gameId]?.message || "",
    tone: "warn",
    source: "cover",
  });
  recomputeSyncStatus();
  return true;
}

function refreshCatalogStatusFromSnapshot() {
  const catalog = appState().catalogSync || {};
  if (catalog.lastError) setCatalogSyncStatus("offline", catalog.lastError);
  else if (!catalog.dirty && catalog.lastSuccessAt) setCatalogSyncStatus("succeeded", "云端目录已同步");
}

function eventGameId(payload) {
  if (!payload || typeof payload === "string") return payload || "";
  return payload.gameId || payload.id || "";
}

// catalog:sync_failed 报错节流：message → 上次弹 toast 的时间戳
const catalogFailToastAt = new Map();

// 偏好写入串行链：payload 必须在前一次 applySnapshot 落地后再组装，
// 否则同机连点会基于同一份旧 prefs 整包提交、互相回滚
let prefsChain = Promise.resolve();
function queuePrefsWrite(task) {
  const run = prefsChain.then(task);
  prefsChain = run.catch(() => {});
  return run;
}

const errMsg = (e) => e?.message || String(e || "未知错误");
const SAVE_RESULT_STATUSES = new Set(["success", "conflict", "failed", "unconfigured", "disabled"]);

/* ---------------- 选择器 ---------------- */

const select = {
  booted: () => state.booted,
  games: () => (appState().games || []).filter((g) => !state.pendingDeletes.has(g.id)),
  game: (id) => (appState().games || []).find((g) => g.id === id),
  accounts: () => appState().accounts || [],
  activities: () => appState().activities || [],
  preferences: prefs,
  device: () => appState().device || {},
  recoveryStatus: () => appState().recoveryStatus || {},
  catalogSync: () => appState().catalogSync || {},
  dataDir: () => state.snapshot?.dataDir || "",
  favoriteIds: () => new Set(prefs().favoriteGames || []),
  runtimeStatus: (id) => state.runtimeStatus[id],
  netStatus: () => state.netStatus,
  search: () => state.search,
  libraryFilter: () => state.libraryFilter,
  syncingAll: () => state.syncingAll,

  filteredGames() {
    const q = state.search.trim().toLowerCase();
    const { kind, tag } = state.libraryFilter;
    const favs = select.favoriteIds();
    return select.games().filter((g) => {
      if (kind === "fav" && !favs.has(g.id)) return false;
      if (kind === "tag" && !(g.tags || []).includes(tag)) return false;
      if (q && !`${g.name}`.toLowerCase().includes(q)) return false;
      return true;
    });
  },

  heroGame() {
    const games = select.games();
    if (!games.length) return null;
    const played = games.filter((g) => g.lastPlayed);
    if (!played.length) return games[0];
    return played.reduce((a, b) => (new Date(a.lastPlayed) > new Date(b.lastPlayed) ? a : b));
  },

  tagSummaries() {
    const counts = new Map();
    for (const g of select.games()) {
      for (const t of g.tags || []) counts.set(t, (counts.get(t) || 0) + 1);
    }
    const order = prefs().tagOrder || [];
    const pinned = new Set(prefs().pinnedTags || []);
    return [...counts.entries()]
      .map(([name, count]) => ({ name, count, pinned: pinned.has(name) }))
      .sort((a, b) => {
        const ia = order.indexOf(a.name);
        const ib = order.indexOf(b.name);
        if (ia !== -1 || ib !== -1) return (ia === -1 ? 1e9 : ia) - (ib === -1 ? 1e9 : ib);
        return b.count - a.count || a.name.localeCompare(b.name, "zh");
      });
  },

  pinnedTags() {
    const existing = new Set(select.tagSummaries().map((t) => t.name));
    return (prefs().pinnedTags || []).filter((t) => existing.has(t));
  },
};

/* ---------------- 动作 ---------------- */

const actions = {
  async boot() {
    const snap = await api.Bootstrap();
    state.booted = true;
    applySnapshot(snap);
    const catalog = snap?.state?.catalogSync || {};
    if (catalog.lastError) setCatalogSyncStatus("offline", catalog.lastError);
    else if (catalog.lastSuccessAt) setCatalogSyncStatus("succeeded", "云端目录已同步");
    else if (catalog.dirty) setCatalogSyncStatus("pending", "等待连接云端");
  },

  setSearch(q) {
    state.search = q;
    notify();
  },

  setLibraryFilter(filter) {
    state.libraryFilter = filter;
    notify();
  },

  /* ----- 同步 ----- */

  // 返回本次目标游戏状态，批量同步用它更新冲突处理后的结果。
  async syncGame(gameId, conflictChoice = "") {
    const game = select.game(gameId);
    if (!game) return "skipped";
    setNet("syncing", `正在同步 ${game.name}…`);
    setRuntimeStatus(gameId, { text: "同步中", tone: "syncing" });
    setSaveSyncStatus(gameId, "syncing", `正在同步 ${game.name}`);
    try {
      applySnapshot(await api.RunSync({ gameId, conflictChoice }));
      refreshCatalogStatusFromSnapshot();
      const updated = select.game(gameId);
      if (!updated) {
        setRuntimeStatus(gameId, null);
        setSaveSyncStatus(gameId, null);
        return "skipped";
      }
      const syncStatus =
        updated.sync?.enabled === false
          ? "disabled"
          : !updated.savePath
            ? "unconfigured"
            : updated.lastSync?.status || "success";
      if (syncStatus === "conflict") {
        setRuntimeStatus(gameId, { text: "存在冲突", tone: "warn" });
        setNet("degraded", "检测到同步冲突，等待处理");
        setSaveSyncStatus(gameId, "conflict", updated.lastSync?.message || "检测到同步冲突，等待处理");
        if (!conflictChoice) {
          const choice = await conflictDialog(updated.lastSync?.message, { gameName: game.name });
          if (choice) return actions.syncGame(gameId, choice);
        }
        // 用户取消或后端未能解决冲突：清理挂起状态，避免卡片常驻异常状态。
        if (!restorePendingCoverStatus(gameId)) setRuntimeStatus(gameId, null);
        return "conflict";
      }
      if (syncStatus === "unconfigured" || syncStatus === "disabled") {
        setSaveSyncStatus(gameId, null);
        if (!restorePendingCoverStatus(gameId)) setRuntimeStatus(gameId, null);
        toast(syncStatus === "unconfigured" ? "当前设备未配置存档目录" : "该游戏已禁用存档同步", "info");
        return syncStatus;
      }
      if (syncStatus === "failed") {
        if (!restorePendingCoverStatus(gameId)) setRuntimeStatus(gameId, null);
        setNet("degraded", updated?.lastSync?.message || "同步失败");
        setSaveSyncStatus(gameId, "failed", updated?.lastSync?.message || "同步失败");
        toast(updated?.lastSync?.message || "同步失败", "err");
        return "failed";
      }
      setSaveSyncStatus(gameId, "succeeded", "同步完成");
      if (!restorePendingCoverStatus(gameId)) setRuntimeStatus(gameId, { text: "同步完成", tone: "success" });
      window.setTimeout(() => {
        if (state.syncTasks.saves[gameId]?.status === "succeeded") setSaveSyncStatus(gameId, null);
        if (!hasPendingCover(gameId) && state.runtimeStatus[gameId]?.tone === "success") setRuntimeStatus(gameId, null);
      }, 3000);
      return "success";
    } catch (e) {
      if (!restorePendingCoverStatus(gameId)) setRuntimeStatus(gameId, null);
      setCatalogSyncStatus("offline", errMsg(e));
      setSaveSyncStatus(gameId, "failed", errMsg(e));
      toast(`同步失败：${errMsg(e)}`, "err");
      return "failed";
    }
  },

  async syncAll() {
    if (state.syncingAll) return { busy: true, results: [], incomplete: [] };
    state.syncingAll = true;
    setNet("syncing", "正在同步游戏库…");
    for (const game of select.games()) setSaveSyncStatus(game.id, "syncing", `正在同步 ${game.name}`);
    notify();
    let batch = null;
    let results = [];
    const clearedConflictIds = new Set();
    try {
      batch = await api.RunSyncAll();
      if (batch?.snapshot) applySnapshot(batch.snapshot);
      setCatalogSyncStatus(
        batch?.catalog?.status === "failed" ? "offline" : "succeeded",
        batch?.catalog?.message || (batch?.catalog?.status === "failed" ? "游戏库目录同步失败" : "云端目录已同步"),
      );
      for (const cover of Array.isArray(batch?.covers) ? batch.covers : []) {
        setCoverSyncStatus({
          ...cover,
          status: cover?.status === "uploaded" ? "succeeded" : cover?.status,
        });
      }
      results = (Array.isArray(batch?.saves) ? batch.saves : []).map((result) => {
        const game = select.game(result?.gameId);
        const status = SAVE_RESULT_STATUSES.has(result?.status) ? result.status : "failed";
        return {
          ...result,
          gameId: result?.gameId || "",
          gameName: result?.gameName || game?.name || result?.gameId || "未知游戏",
          status,
          message: result?.message || "",
          uploaded: Number(result?.uploaded) || 0,
          downloaded: Number(result?.downloaded) || 0,
          conflicts: Number(result?.conflicts) || 0,
        };
      });

      for (const result of results.filter((item) => item.status === "conflict")) {
        if (!select.game(result.gameId)) continue;
        // 后端已完成全库流水线；这里只复用目标游戏分支处理用户选择。
        // eslint-disable-next-line no-await-in-loop
        const status = await actions.syncGame(result.gameId);
        result.status = SAVE_RESULT_STATUSES.has(status) ? status : "failed";
        if (result.status === "conflict") clearedConflictIds.add(result.gameId);
        const latest = select.game(result.gameId)?.lastSync;
        if (latest?.status === result.status) {
          result.message = latest.message || result.message;
          result.uploaded = Number(latest.uploaded) || 0;
          result.downloaded = Number(latest.downloaded) || 0;
          result.conflicts = Number(latest.conflicts) || 0;
        }
      }
    } catch (e) {
      const failed = {
        gameId: "",
        gameName: "全部同步",
        status: "failed",
        message: errMsg(e),
        uploaded: 0,
        downloaded: 0,
        conflicts: 0,
      };
      results = [failed];
      for (const gameId of Object.keys(state.syncTasks.saves)) setSaveSyncStatus(gameId, null);
      setCatalogSyncStatus("offline", failed.message);
      toast(`同步失败：${failed.message}`, "err");
    } finally {
      state.syncingAll = false;
      notify();
    }

    for (const result of results) {
      if (!result.gameId) continue;
      if (result.status === "success") {
        setSaveSyncStatus(result.gameId, "succeeded", result.message || "同步完成");
        if (!restorePendingCoverStatus(result.gameId)) {
          setRuntimeStatus(result.gameId, { text: "同步完成", tone: "success" });
        }
        window.setTimeout(() => {
          if (state.syncTasks.saves[result.gameId]?.status === "succeeded") setSaveSyncStatus(result.gameId, null);
          if (!hasPendingCover(result.gameId) && state.runtimeStatus[result.gameId]?.tone === "success") {
            setRuntimeStatus(result.gameId, null);
          }
        }, 3000);
      } else if (result.status === "conflict") {
        setSaveSyncStatus(result.gameId, "conflict", result.message || "存在冲突");
        if (!clearedConflictIds.has(result.gameId)) {
          if (!restorePendingCoverStatus(result.gameId)) setRuntimeStatus(result.gameId, { text: "存在冲突", tone: "warn" });
        }
      } else if (result.status === "failed") {
        setSaveSyncStatus(result.gameId, "failed", result.message || "同步失败");
        if (!restorePendingCoverStatus(result.gameId)) setRuntimeStatus(result.gameId, { text: "同步失败", tone: "warn" });
      } else {
        setSaveSyncStatus(result.gameId, null);
        if (!restorePendingCoverStatus(result.gameId)) setRuntimeStatus(result.gameId, null);
      }
    }

    const successCount = results.filter((result) => result.status === "success").length;
    const incomplete = results.filter((result) => result.status === "failed" || result.status === "conflict");
    if (batch?.catalog?.status === "failed") {
      incomplete.push({
        gameId: "",
        gameName: "游戏库目录",
        status: "failed",
        message: batch.catalog.message || "游戏库目录同步失败",
      });
    }
    const pendingCovers = (Array.isArray(batch?.covers) ? batch.covers : []).filter(
      (cover) => cover?.status === "pending",
    );
    for (const cover of pendingCovers) {
      incomplete.push({
        gameId: cover.gameId || "",
        gameName: cover.gameName || select.game(cover.gameId)?.name || cover.gameId || "游戏封面",
        status: "pending",
        message: cover.message || "封面等待重试",
      });
    }
    const unconfiguredCount = results.filter((result) => result.status === "unconfigured").length;
    const disabledCount = results.filter((result) => result.status === "disabled").length;
    if (batch) {
      const parts = [`成功 ${successCount}`];
      if (unconfiguredCount) parts.push(`未配置 ${unconfiguredCount}`);
      if (disabledCount) parts.push(`已禁用 ${disabledCount}`);
      if (batch.catalog?.status === "failed") parts.push("目录失败");
      if (pendingCovers.length) parts.push(`封面待重试 ${pendingCovers.length}`);
      const incompleteSaveCount = results.filter(
        (result) => result.status === "failed" || result.status === "conflict",
      ).length;
      if (incompleteSaveCount) parts.push(`存档未完成 ${incompleteSaveCount}`);
      setNet(incomplete.length ? "degraded" : "online", incomplete.length ? "部分游戏未完成同步" : "同步完成");
      toast(`全部同步完成（${parts.join("，")}）`, incomplete.length ? "warn" : "ok");
    }
    return {
      busy: false,
      results,
      incomplete,
      catalog: batch?.catalog,
      covers: batch?.covers,
      stats: batch?.stats,
    };
  },

  /* ----- 启动 ----- */

  async launchGame(gameId, conflictChoice = "") {
    const game = select.game(gameId);
    if (!game) return;
    if (!game.installPath) {
      toast("未配置启动文件，请先在详情页设置", "warn");
      return;
    }
    setNet("syncing", `正在检查 ${game.name} 的启动前存档…`);
    setRuntimeStatus(gameId, { text: "启动准备中", tone: "syncing" });
    try {
      const result = await api.PrepareGameLaunch(gameId, conflictChoice);
      if (result?.snapshot) applySnapshot(result.snapshot);
      if (result?.status === "needs_choice") {
        setRuntimeStatus(gameId, { text: "存在冲突", tone: "warn" });
        const choice = await conflictDialog(result.message || "检测到启动前存档冲突，请选择保留哪一侧。", { gameName: game.name });
        if (choice) return actions.launchGame(gameId, choice);
        setRuntimeStatus(gameId, null);
        setNet("online", "");
        return;
      }
      if (result?.status === "failed") throw new Error(result?.message || "启动前同步失败");
      setNet("online", result?.message || "启动前存档检查完成");
      await api.LaunchAndMonitorGame(gameId);
    } catch (e) {
      const go = await confirm({
        message: `${errMsg(e)}\n继续启动将跳过本次启动前同步，是否继续？`,
        confirmText: "继续启动",
        cancelText: "取消",
      });
      if (go) {
        try {
          await api.LaunchAndMonitorGame(gameId);
          // 跳过预同步成功启动后清掉早前的"检查中"状态，避免顶栏卡"同步中"
          setNet("online", "");
          return;
        } catch (e2) {
          toast(`启动失败：${errMsg(e2)}`, "err");
        }
      }
      setRuntimeStatus(gameId, null);
      setNet("offline", errMsg(e));
    }
  },

  /* ----- 收藏 / 删除 / 保存 / 排序 ----- */

  async toggleFavorite(gameId) {
    return queuePrefsWrite(async () => {
      const favs = [...(prefs().favoriteGames || [])];
      const idx = favs.indexOf(gameId);
      if (idx >= 0) favs.splice(idx, 1);
      else favs.push(gameId);
      try {
        applySnapshot(await api.SavePreferences({ ...prefs(), favoriteGames: favs }));
        toast(idx >= 0 ? "已移出常玩" : "已加入常玩", "ok");
      } catch (e) {
        toast(errMsg(e), "err");
      }
    });
  },

  async deleteGame(gameId) {
    const game = select.game(gameId);
    if (!game) return;
    const yes = await confirm({
      message: `确定要删除「${game.name}」吗？\n云端存档备份将一并清理，此操作不可恢复。`,
      confirmText: "删除",
      tone: "danger",
    });
    if (!yes) return;
    state.pendingDeletes.add(gameId);
    notify();
    try {
      await api.RequestDeleteGame(gameId);
      toast("已加入删除队列", "info");
    } catch (e) {
      state.pendingDeletes.delete(gameId);
      notify();
      toast(`删除失败：${errMsg(e)}`, "err");
    }
  },

  async saveGame(game) {
    applySnapshot(await api.SaveGame(game));
  },

  async reorderGames(ids) {
    try {
      applySnapshot(await api.ReorderGames(ids));
    } catch (e) {
      toast(errMsg(e), "err");
    }
  },

  /* ----- 标签 ----- */

  async pinTag(name, on) {
    return queuePrefsWrite(async () => {
      const pinned = new Set(prefs().pinnedTags || []);
      if (on) pinned.add(name);
      else pinned.delete(name);
      try {
        applySnapshot(await api.SavePreferences({ ...prefs(), pinnedTags: [...pinned] }));
        toast(on ? `已固定「${name}」到库页筛选` : `已取消固定「${name}」`, "ok");
      } catch (e) {
        toast(errMsg(e), "err");
      }
    });
  },

  async reorderTags(names) {
    return queuePrefsWrite(async () => {
      try {
        applySnapshot(await api.UpdateTagOrder(names));
      } catch (e) {
        toast(errMsg(e), "err");
      }
    });
  },

  /* ----- 账号 ----- */

  async saveAccount(account) {
    applySnapshot(await api.SaveAccount(account));
  },

  async verifyAccount(id) {
    applySnapshot(await api.VerifyAccount(id));
  },

  async deleteAccount(id) {
    const acc = select.accounts().find((a) => a.id === id);
    const yes = await confirm({
      message: `确定要删除账号「${acc?.name || id}」吗？\n使用该账号的游戏将无法继续同步。`,
      confirmText: "删除",
      tone: "danger",
    });
    if (!yes) return;
    try {
      applySnapshot(await api.DeleteAccount(id));
      toast("账号已删除", "ok");
    } catch (e) {
      toast(errMsg(e), "err");
    }
  },

  async setRecoveryPassword(password) {
    const snapshot = await api.SetRecoveryPassword(password);
    applySnapshot(snapshot);
    return snapshot;
  },

  /* ----- 存储方式切换 ----- */

  // 切换主存储：验证目标账号/停用旧账号/游戏重挂/目录与存档同步全流程由后端一次完成，
  // 进度经 storage:switch_progress 事件由视图的进度对话框呈现；此处只负责调用与新快照落地。
  // 前置的"先同步旧云端"由视图编排（走既有交互式 syncAll）。
  async switchStorage(request) {
    const result = await api.SwitchStoragePrimary(request);
    if (result?.snapshot) applySnapshot(result.snapshot);
    return result;
  },

  async resumeStorageMigration(transactionId, conflictChoice = "") {
    const result = await api.ResumeStorageMigration({ transactionId, conflictChoice });
    if (result?.snapshot) applySnapshot(result.snapshot);
    return result;
  },

  async cancelStorageMigration(transactionId) {
    const snapshot = await api.CancelStorageMigration(transactionId);
    applySnapshot(snapshot);
    return snapshot;
  },

  async savePreferences(patch) {
    return queuePrefsWrite(async () => {
      applySnapshot(await api.SavePreferences({ ...prefs(), ...patch }));
    });
  },

  /* ----- 后端事件绑定（main.js 启动时调用一次） ----- */

  bindBackendEvents() {
    api.onEvent("game:started", (p) => {
      setRuntimeStatus(eventGameId(p), { text: "游戏中", tone: "playing" });
      toast("游戏已启动", "ok");
    });
    api.onEvent("game:ended", (p) => {
      setRuntimeStatus(eventGameId(p), { text: "打包保存中", tone: "syncing" });
      toast("游戏进程已结束，时长已记录", "ok");
    });
    api.onEvent("game:monitor_timeout", (p) => {
      // 进程监控超时：后端未记录时长、未做自动备份，仅清理界面挂起状态
      setRuntimeStatus(eventGameId(p), null);
      toast("未能识别游戏进程，本次未记录时长", "info");
    });
    api.onEvent("game:backup_starting", () => {
      toast("正在上传游戏结束备份", "info");
    });
    api.onEvent("game:backup_success", (p) => {
      const id = eventGameId(p);
      setRuntimeStatus(id, { text: "同步完成", tone: "success" });
      toast("自动游戏保存完成", "ok");
      window.setTimeout(() => {
        if (state.runtimeStatus[id]?.tone === "success") setRuntimeStatus(id, null);
      }, 3000);
    });
    api.onEvent("game:backup_error", (p) => {
      setRuntimeStatus(eventGameId(p), null);
      toast(`自动存档发生错误：${p?.error || "未知错误"}`, "err");
    });
    api.onEvent("sync:progress", (p) => {
      if (!p?.message) return;
      setNet("syncing", p.message);
      if (p.gameId) {
        setSaveSyncStatus(p.gameId, "syncing", p.message);
        setRuntimeStatus(p.gameId, { text: p.message, tone: "syncing" });
      }
    });
    api.onEvent("cover:sync_state", (p) => setCoverSyncStatus(p));
    api.onEvent("cover:warning", (p) => {
      if (p?.message) toast(p.message, "warn");
    });
    api.onEvent("game:delete_queued", (p) => {
      setNet("syncing", p?.name ? `正在删除：${p.name}` : "正在删除游戏");
    });
    api.onEvent("game:delete_succeeded", (p) => {
      state.pendingDeletes.delete(eventGameId(p));
      setNet("online", "游戏删除完成");
    });
    api.onEvent("game:delete_failed", (p) => {
      const id = eventGameId(p);
      if (p?.stage === "remote_cleanup") {
        state.pendingDeletes.delete(id);
        toast(p?.error || "游戏已从本机移除，但云端清理失败", "warn");
      } else {
        state.pendingDeletes.delete(id);
        toast(p?.error || "删除游戏失败", "err");
      }
      notify();
    });
    api.onEvent("catalog:sync_state", (p) => {
      setCatalogSyncStatus(p?.status || "checking", p?.message || "");
    });
    api.onEvent("catalog:sync_failed", (p) => {
      setCatalogSyncStatus("offline", p?.message || "后台上传失败");
      // 后台失败会自动重试并反复上报，同一错误 5 分钟内只提示一次
      const msg = p?.message || "未知错误";
      const now = Date.now();
      const last = catalogFailToastAt.get(msg) || 0;
      if (now - last > 5 * 60 * 1000) {
        catalogFailToastAt.set(msg, now);
        toast(`后台上传数据库失败：${msg}`, "err");
      }
    });
    api.onEvent("state:updated", (appStateNext) => {
      applyAppState(appStateNext);
    });
  },
};

export const store = { state, subscribe, select, actions };
