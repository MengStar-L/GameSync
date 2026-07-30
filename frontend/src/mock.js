// ============================================================
// mock.js —— 浏览器试驾用假后端：实现全部绑定 + 假事件流。
// 仅在非 Wails 环境（npm run dev 直接开浏览器）被 api.js 使用。
// ============================================================

const delay = (ms) => new Promise((r) => setTimeout(r, ms));
const clone = (v) => JSON.parse(JSON.stringify(v));
const nowIso = () => new Date().toISOString();
const agoIso = (min) => new Date(Date.now() - min * 60000).toISOString();

function svgCover(c1, c2, label) {
  const svg = `<svg xmlns='http://www.w3.org/2000/svg' width='360' height='480'><defs><linearGradient id='g' x1='0' y1='0' x2='1' y2='1'><stop offset='0' stop-color='${c1}'/><stop offset='1' stop-color='${c2}'/></linearGradient></defs><rect width='360' height='480' fill='url(#g)'/><text x='24' y='444' font-family='Georgia' font-size='30' font-weight='700' fill='rgba(255,255,255,0.9)'>${label}</text></svg>`;
  return `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`;
}

function makeGame(i, over = {}) {
  return {
    id: `g-${i}`,
    name: over.name || `游戏 ${i}`,
    installPath: over.noPath ? "" : `D:/Games/game-${i}/start.exe`,
    savePath: over.noPath ? "" : `C:/Users/player/Saves/game-${i}`,
    coverPath: over.cover || "",
    description: over.desc || "",
    released: over.released || "2023-04-12",
    rating: over.rating ?? 4.4,
    ratingTop: 5,
    metacritic: over.metacritic ?? 88,
    genres: over.genres || ["动作", "冒险"],
    platforms: ["PC"],
    isSteam: over.isSteam ?? true,
    developers: over.dev || ["Studio Alpha"],
    publishers: over.pub || ["Publisher Beta"],
    website: "https://example.com",
    rawgId: 1000 + i,
    rawgSlug: `game-${i}`,
    rawgUrl: "",
    rawgTags: over.rawgTags || ["Singleplayer", "Atmospheric", "Great Soundtrack"],
    tags: over.tags || [],
    storageAccountId: "acc-1",
    backupStorageAccountId: "acc-1",
    backupCount: over.backupCount ?? 6,
    sync: { enabled: true, includePatterns: [], excludePatterns: [], conflictStrategy: over.conflict || "manual" },
    anchor: { lastRemoteVersion: 3, lastManifest: { version: 1, generatedAt: agoIso(600), files: [], totalBytes: 0, hash: "" } },
    lastSync: over.lastSync,
    playTime: over.playTime ?? 320,
    lastPlayed: over.lastPlayed,
  };
}

export function createMockBackend() {
  const listeners = new Map();
  const on = (topic, fn) => {
    if (!listeners.has(topic)) listeners.set(topic, new Set());
    listeners.get(topic).add(fn);
    return () => listeners.get(topic)?.delete(fn);
  };
  const emit = (topic, payload) => {
    listeners.get(topic)?.forEach((fn) => {
      try {
        fn(payload);
      } catch (e) {
        console.error(e);
      }
    });
  };

  const state = {
    device: { id: "dev-1", name: "DESKTOP-ATELIER", platform: "windows", lastStartedAt: nowIso() },
    accounts: [
      {
        id: "acc-1",
        name: "主账号",
        provider: "cloudflare",
        accountId: "f3c2a1b09d8e4765b4a3928170fedcba",
        apiToken: "tok",
        d1DatabaseId: "b2d4a6bc-78ef-4d90-a123-4567bc89def0",
        r2Bucket: "gamesync-prod",
        r2AccessKeyId: "8f6c2d8a9b1e3f50",
        r2SecretAccessKey: "sec",
        isPrimary: true,
        enabled: true,
        usedBytes: 3.2 * 2 ** 30,
        lastVerifiedAt: agoIso(60),
        verificationState: "valid",
      },
      {
        id: "acc-2",
        name: "副账号 1",
        provider: "cloudflare",
        accountId: "77aa11bb22cc33dd44ee55ff66aa77bb",
        apiToken: "",
        d1DatabaseId: "",
        r2Bucket: "gamesync-alt",
        r2AccessKeyId: "1a2b3c4d5e6f7a8b",
        r2SecretAccessKey: "sec2",
        isPrimary: false,
        enabled: true,
        usedBytes: 0.8 * 2 ** 30,
        lastVerifiedAt: null,
        verificationState: "unverified",
        usageWarning: "",
      },
      {
        id: "acc-3",
        name: "副账号 2",
        provider: "webdav",
        accountId: "",
        apiToken: "",
        d1DatabaseId: "",
        r2Bucket: "",
        r2AccessKeyId: "",
        r2SecretAccessKey: "",
        webdavUrl: "https://dav.jianguoyun.com/dav/",
        webdavUsername: "player@example.com",
        webdavPassword: "app-pass-mock",
        webdavRoot: "GameSync",
        isPrimary: false,
        enabled: true,
        usedBytes: 1.6 * 2 ** 30,
        lastVerifiedAt: agoIso(60 * 5),
        verificationState: "valid",
      },
    ],
    games: [
      makeGame(1, { name: "艾尔登法环", cover: svgCover("#8a7cc9", "#241d40", "ELDEN"), tags: ["角色扮演", "魂类"], playTime: 5480, lastPlayed: agoIso(130), lastSync: { status: "success", message: "同步完成", uploaded: 128e6, downloaded: 0, conflicts: 0, syncedAt: agoIso(128) }, backupCount: 12 }),
      makeGame(2, { name: "星露谷物语", cover: svgCover("#69a06f", "#1c3a2a", "STARDEW"), tags: ["模拟经营", "像素"], playTime: 9210, lastPlayed: agoIso(60 * 26), lastSync: { status: "conflict", message: "本地与云端均有修改", uploaded: 0, downloaded: 0, conflicts: 1, syncedAt: agoIso(70) }, backupCount: 34 }),
      makeGame(3, { name: "哈迪斯 II", cover: svgCover("#d97757", "#4c1f30", "HADES II"), isSteam: false, tags: ["Roguelike", "动作"], playTime: 2130, lastPlayed: agoIso(8), backupCount: 8 }),
      makeGame(4, { name: "空洞骑士：丝之歌", cover: svgCover("#5f93bd", "#122a44", "SILKSONG"), tags: ["银河恶魔城", "动作"], playTime: 1240, lastPlayed: agoIso(60 * 74), lastSync: { status: "success", message: "同步完成", uploaded: 0, downloaded: 4.2e6, conflicts: 0, syncedAt: agoIso(60 * 74) }, backupCount: 21 }),
      makeGame(5, { name: "博德之门 3", cover: svgCover("#a678b8", "#301a42", "BG3"), tags: ["角色扮演", "CRPG"], playTime: 15320, lastPlayed: agoIso(60 * 24 * 9), backupCount: 17 }),
      makeGame(6, { name: "泰拉瑞亚", cover: svgCover("#7aa869", "#243d1e", "TERRARIA"), tags: ["沙盒", "像素"], playTime: 3450, lastPlayed: agoIso(60 * 24 * 21), backupCount: 9 }),
      makeGame(7, { name: "赛博朋克 2077", cover: svgCover("#c9b458", "#33291a", "CP2077"), tags: ["角色扮演", "开放世界"], playTime: 6110, lastPlayed: agoIso(60 * 24 * 40), backupCount: 14 }),
      makeGame(8, { name: "尚未配置的游戏", noPath: true, isSteam: false, tags: [], playTime: 0, lastPlayed: null, backupCount: 0 }),
    ],
    preferences: {
      autoSyncOnLaunch: true,
      startupSyncMode: "smart",
      conflictPolicy: "manual",
      backgroundSyncIntervalSeconds: 60,
      gameCardMode: "classic",
      gameCardModeUpdatedAt: agoIso(60),
      defaultInstallDir: "",
      defaultSaveDir: "",
      defaultSteamInstallDir: "",
      defaultSteamSaveDir: "",
      defaultThirdInstallDir: "",
      defaultThirdSaveDir: "",
      rawgApiKey: "demo-key",
      steamGridDbApiKey: "",
      favoriteGames: ["g-1", "g-3"],
      tagOrder: ["角色扮演", "动作", "像素"],
      pinnedTags: ["角色扮演", "像素"],
      sidebarNavOrder: [],
    },
    activities: [
      { id: "a-1", gameId: "g-1", gameName: "艾尔登法环", accountId: "acc-1", status: "success", message: "本地 → 云端，用时 6.1s", uploaded: 128e6, downloaded: 0, conflicts: 0, startedAt: agoIso(128), endedAt: agoIso(128) },
      { id: "a-2", gameId: "g-2", gameName: "星露谷物语", accountId: "acc-1", status: "conflict", message: "本地与云端均有修改，等待手动选择", uploaded: 0, downloaded: 0, conflicts: 1, startedAt: agoIso(70), endedAt: agoIso(70) },
      { id: "a-3", gameId: "g-4", gameName: "空洞骑士：丝之歌", accountId: "acc-1", status: "success", message: "云端 → 本地，用时 2.4s", uploaded: 0, downloaded: 4.2e6, conflicts: 0, startedAt: agoIso(60 * 74), endedAt: agoIso(60 * 74) },
      { id: "a-4", gameId: "g-7", gameName: "赛博朋克 2077", accountId: "acc-2", status: "failed", message: "网络错误，上传中断", uploaded: 0, downloaded: 0, conflicts: 0, startedAt: agoIso(60 * 30), endedAt: agoIso(60 * 30) },
    ],
    recoveryStatus: { hasRecoveryPassword: false, remoteCatalogAvailable: true, pendingCredentialBackup: false, lastCatalogSyncAt: agoIso(12) },
    catalogSync: { dirty: false, initialPullCompleted: true, lastKnownRevision: 42, lastSuccessAt: agoIso(12) },
  };

  const backupsByGame = new Map();
  const ensureBackups = (gameId) => {
    if (!backupsByGame.has(gameId)) {
      const game = state.games.find((g) => g.id === gameId);
      const n = Math.min(game?.backupCount || 0, 6);
      const list = [];
      for (let i = 0; i < n; i++) {
		const filename = `${gameId}-backup-${i}.zip`;
        list.push({
		  id: `dev-1:${filename}`,
          gameId,
          type: i % 3 === 0 ? "manual" : "auto",
          name: i % 3 === 0 ? `通关前备份 ${i}` : "",
		  filename,
          size: 40e6 + i * 7e6,
          storageAccountId: "acc-1",
          createdAt: agoIso(60 * (i * 9 + 3)),
          sourceDeviceId: "dev-1",
          localExists: true,
          cloudExists: i !== 1,
        });
      }
      backupsByGame.set(gameId, list);
    }
    return backupsByGame.get(gameId);
  };

  const snapshot = () => clone({ state, dataDir: "C:/Users/player/AppData/Roaming/GameSync", schemaVersion: 7 });

  const resolvedConflicts = new Set();
  let launchChoiceServed = false;

  const runSaveSync = (game, conflictChoice = "") => {
    let result;
    if (game.sync?.enabled === false) {
      result = { status: "disabled", message: "该游戏已禁用存档同步", uploaded: 0, downloaded: 0, conflicts: 0 };
    } else if (!game.savePath) {
      result = { status: "unconfigured", message: "当前设备未配置存档目录", uploaded: 0, downloaded: 0, conflicts: 0 };
    } else if (game.mockSyncFailure) {
      result = { status: "failed", message: String(game.mockSyncFailure), uploaded: 0, downloaded: 0, conflicts: 0 };
    } else if (game.name.includes("星露谷") && !conflictChoice && !resolvedConflicts.has(game.id)) {
      result = { status: "conflict", message: "本地与云端均有修改，请选择保留哪一侧", uploaded: 0, downloaded: 0, conflicts: 1 };
    } else {
      if (conflictChoice) resolvedConflicts.add(game.id);
      result = {
        status: "success",
        message: conflictChoice ? `已按「${conflictChoice === "local" ? "保留本地" : "保留云端"}」完成同步` : "同步完成",
        uploaded: 23e6,
        downloaded: 0,
        conflicts: 0,
      };
    }

    game.lastSync = { ...result, syncedAt: nowIso() };
    if (result.status === "success" || result.status === "conflict" || result.status === "failed") {
      state.activities.unshift({
        id: `a-${Date.now()}-${game.id}`,
        gameId: game.id,
        gameName: game.name,
        accountId: game.storageAccountId || "acc-1",
        ...result,
        startedAt: nowIso(),
        endedAt: nowIso(),
      });
    }
    return { gameId: game.id, gameName: game.name, ...result };
  };

  const rawgResult = (i, name) => ({
    id: 9000 + i,
    name,
    slug: name.toLowerCase().replaceAll(" ", "-"),
    released: `201${i % 10}-0${(i % 8) + 1}-15`,
    coverPath: svgCover(["#8a7cc9", "#d97757", "#5f93bd", "#69a06f"][i % 4], "#22203a", name.slice(0, 7)),
    coverOptions: [0, 1, 2].map((k) => svgCover(["#8a7cc9", "#d97757", "#5f93bd"][k], "#1c1a2e", `${name.slice(0, 5)} ${k + 1}`)),
    rating: 4 + (i % 10) / 10,
    metacritic: 78 + (i % 20),
  });

  let storageMigration = null;

  const impl = {
    async Bootstrap() {
      await delay(420);
      return snapshot();
    },
    async IsFirstLaunch() {
      return new URLSearchParams(location.search).has("welcome");
    },
    async GetAppInfo() {
      return { version: "0.9.0-mock", commit: "abc1234", buildDate: nowIso(), updateChannel: "stable", updateRepo: "", updateManifestUrl: "", platform: "windows/amd64" };
    },
    async ResolveCoverSource(ref) {
      await delay(90);
      const game = state.games.find((g) => g.id === ref);
      return game?.coverPath || "";
    },
    async SaveGame(game) {
      await delay(350);
      const idx = state.games.findIndex((g) => g.id === game.id);
      if (idx >= 0) state.games[idx] = { ...state.games[idx], ...game };
      else state.games.push({ ...makeGame(Date.now()), ...game, id: game.id || `g-${Date.now()}` });
      return snapshot();
    },
    async RequestDeleteGame(id) {
      await delay(200);
      emit("game:delete_queued", { id, name: state.games.find((g) => g.id === id)?.name });
      setTimeout(() => {
        state.games = state.games.filter((g) => g.id !== id);
        emit("game:delete_succeeded", { id });
        emit("state:updated", clone(state));
      }, 1400);
    },
    async DeleteGame(id) {
      await delay(300);
      state.games = state.games.filter((g) => g.id !== id);
      return snapshot();
    },
    async ReorderGames(ids) {
      await delay(150);
      state.games.sort((a, b) => ids.indexOf(a.id) - ids.indexOf(b.id));
      return snapshot();
    },
    async RunSync({ gameId, conflictChoice }) {
      const game = state.games.find((g) => g.id === gameId);
      if (!game) return snapshot();
      emit("cover:sync_state", { gameId, status: "syncing", message: "正在同步游戏封面" });
      emit("cover:sync_state", {
        gameId,
        status: game.mockCoverPending ? "pending" : "skipped",
        message: game.mockCoverPending ? String(game.mockCoverPending) : "",
      });
      emit("sync:progress", { gameId, message: `正在比对 ${game.name} 的存档…` });
      await delay(900);
      runSaveSync(game, conflictChoice);
      return snapshot();
    },
    async RunSyncAll() {
      emit("sync:progress", { message: "正在同步游戏库与封面…" });
      await delay(900);
      const saves = [];
      for (const game of state.games) {
        emit("sync:progress", { gameId: game.id, message: `正在比对 ${game.name} 的存档…` });
        saves.push(runSaveSync(game));
      }
      state.catalogSync = {
        ...state.catalogSync,
        dirty: false,
        lastKnownRevision: (Number(state.catalogSync?.lastKnownRevision) || 0) + 1,
        lastSuccessAt: nowIso(),
      };
      const covers = state.games.map((game) => ({
        gameId: game.id,
        gameName: game.name,
        status: game.mockCoverPending ? "pending" : "skipped",
        message: game.mockCoverPending ? String(game.mockCoverPending) : game.coverPath ? "封面内容未变化" : "未配置封面",
      }));
      for (const cover of covers) {
        emit("cover:sync_state", {
          gameId: cover.gameId,
          status: cover.status,
          message: cover.message,
        });
      }
      return {
        snapshot: snapshot(),
        catalog: { status: "success", revision: state.catalogSync.lastKnownRevision },
        covers,
        saves: clone(saves),
        stats: {
          enumeratedGames: state.games.filter((game) => game.savePath && game.sync?.enabled !== false).length,
          stattedFiles: 0,
          hashedFiles: 0,
          uploadedObjects: saves.filter((result) => result.status === "success").length,
          downloadedObjects: 0,
        },
      };
    },
    async PrepareGameLaunch(gameId, conflictChoice) {
      await delay(700);
      if (!launchChoiceServed && !conflictChoice && state.games.find((g) => g.id === gameId)?.name.includes("哈迪斯")) {
        launchChoiceServed = true;
        return { status: "needs_choice", message: "云端存在更新的存档（来自 LAPTOP-B），请选择保留哪一侧。" };
      }
      return { status: "ready", message: "启动前存档检查完成", snapshot: snapshot() };
    },
    async LaunchAndMonitorGame(gameId) {
      await delay(400);
      const game = state.games.find((g) => g.id === gameId);
      // 可测钩子：无启动路径的游戏模拟"进程监控超时"（真实后端 60s 扫不到进程，
      // 不发 game:started/ended，不记时长不备份；控制台 __gs.api 可直接触发）
      if (game && !game.installPath) {
        setTimeout(() => emit("game:monitor_timeout", { gameId }), 3000);
        return;
      }
      emit("game:started", gameId);
      setTimeout(() => {
        if (game) {
          game.playTime = (game.playTime || 0) + 12;
          game.lastPlayed = nowIso();
        }
        emit("game:ended", { gameId });
        emit("game:backup_starting", gameId);
        setTimeout(() => {
          if (game) game.backupCount = (game.backupCount || 0) + 1;
          backupsByGame.delete(gameId);
          emit("game:backup_success", gameId);
          emit("state:updated", clone(state));
        }, 2000);
      }, 6000);
    },
    async GetGameBackups(gameId) {
      await delay(500);
      return { backups: clone(ensureBackups(gameId)), partial: false };
    },
    async CreateGameBackup(gameId, accountId, name) {
      await delay(1200);
      const list = ensureBackups(gameId);
	  const filename = `${gameId}-${Date.now()}.zip`;
	  list.unshift({ id: `dev-1:${filename}`, gameId, type: "manual", name: name || "", filename, size: 52e6, storageAccountId: "acc-1", createdAt: nowIso(), sourceDeviceId: "dev-1", localExists: true, cloudExists: true });
      const game = state.games.find((g) => g.id === gameId);
      if (game) game.backupCount = (game.backupCount || 0) + 1;
      return clone(list[0]);
    },
    async RestoreGameBackup(gameId, backupId) {
	  await delay(1100);
	},
    async DeleteGameBackup(gameId, backupId) {
	  await delay(600);
	  const backup = ensureBackups(gameId).find((b) => b.id === backupId);
	  backupsByGame.set(gameId, ensureBackups(gameId).filter((b) => b.id !== backupId));
	  const game = state.games.find((g) => g.id === gameId);
	  if (game && game.backupCount > 0) game.backupCount -= 1;
	  emit("game:backup_delete_succeeded", { id: gameId, backupId, filename: backup?.filename || "" });
	},
    async SaveAccount(account) {
      await delay(420);
      const idx = state.accounts.findIndex((a) => a.id === account.id);
      if (idx >= 0) state.accounts[idx] = { ...state.accounts[idx], ...account };
      else {
        const isPrimary = state.accounts.length === 0;
        state.accounts.push({ ...account, id: `acc-${Date.now()}`, name: isPrimary ? "主账号" : `副账号 ${state.accounts.length}`, isPrimary, usedBytes: 0, verificationState: "unverified" });
      }
      return snapshot();
    },
    async DeleteAccount(id) {
      await delay(300);
      state.accounts = state.accounts.filter((a) => a.id !== id);
      return snapshot();
    },
    async VerifyAccount(id) {
      await delay(1300);
      const acc = state.accounts.find((a) => a.id === id);
      if (acc) {
        acc.verificationState = "valid";
        acc.lastVerifiedAt = nowIso();
        acc.lastError = "";
      }
      return snapshot();
    },
    async SavePreferences(prefs) {
      await delay(220);
      state.preferences = { ...state.preferences, ...prefs };
      return snapshot();
    },
    async UpdateTagOrder(order) {
      await delay(150);
      state.preferences.tagOrder = order;
      return snapshot();
    },
    async UpdateSidebarNavOrder(order) {
      await delay(150);
      state.preferences.sidebarNavOrder = order;
      return snapshot();
    },
    async SearchRAWGGames(q) {
      await delay(750);
      if (!state.preferences.rawgApiKey) throw new Error("未配置 RAWG API Key");
      return [1, 2, 3, 4].map((i) => rawgResult(i, `${q} Result ${i}`));
    },
    async GetRAWGGame(id) {
      await delay(600);
      const r = rawgResult(id % 10, `RAWG Game ${id}`);
      return { rawgId: id, rawgSlug: r.slug, rawgUrl: "https://rawg.io", name: r.name, coverPath: r.coverPath, coverOptions: r.coverOptions, description: "这是一段来自 RAWG 的示例简介。黑暗奇幻世界中的动作冒险，探索、战斗与成长。\n\n支持多结局与丰富的支线。", released: r.released, rating: r.rating, ratingTop: 5, metacritic: r.metacritic, genres: ["动作", "角色扮演"], platforms: ["PC"], developers: ["Mock Studio"], publishers: ["Mock Pub"], website: "https://example.com", rawgTags: ["Singleplayer", "Souls-like", "Dark Fantasy", "Atmospheric"] };
    },
    async SearchSteamGridDBGames(q) {
      await delay(750);
      if (!state.preferences.steamGridDbApiKey) throw new Error("未配置 SteamGridDB API Key");
      return [1, 2].map((i) => ({ id: i, name: `${q} Grid ${i}`, types: ["static"], verified: i === 1, coverPath: svgCover("#3d5a92", "#141b2e", `GRID ${i}`), coverOptions: [0, 1, 2, 3].map((k) => svgCover(["#3d5a92", "#8a7cc9", "#d97757", "#69a06f"][k], "#10131f", `G${i}-${k}`)) }));
    },
    async PickFile() {
      await delay(300);
      return "D:/Games/demo/start.exe";
    },
    async PickFolder(request) {
      await delay(300);
      if (String(request?.title || "").includes("Steam 游戏路径")) return "D:/SteamLibrary/steamapps/common";
      if (String(request?.title || "").includes("Steam 游戏存档")) return "C:/Users/player/SteamSaves";
      if (String(request?.title || "").includes("第三方游戏路径")) return "D:/Games";
      return "C:/Users/player/Saves/demo";
    },
    async OpenPath() {},
    async ExportAppBackup() {
      await delay(500);
    },
    async ImportAppBackup() {
      await delay(900);
    },
    async ExportWindowState() {
      return "{}";
    },
    async RestoreFromPrimary() {
      await delay(1200);
      return snapshot();
    },
    async SetRecoveryPassword() {
      await delay(400);
      state.recoveryStatus.hasRecoveryPassword = true;
      return snapshot();
    },
    // 运行时切换存储方式：事件阶段与真实迁移协调器保持一致。
    async SwitchStoragePrimary(request) {
      if (!state.recoveryStatus.hasRecoveryPassword) throw new Error("恢复密码不能为空");
      const existingId = String(request?.existingAccountId || "").trim();
      const hasNew = Boolean(request?.newAccount);
      if (Boolean(existingId) === hasNew) throw new Error("请选择一个已有连接或填写一个新连接");

      const current = state.accounts.find((account) => account.isPrimary);
      const stored = existingId ? state.accounts.find((account) => account.id === existingId) : null;
      if (existingId && !stored) throw new Error("未找到对应的云账号");
      const account = existingId ? { ...stored } : { ...request.newAccount, id: `acc-${Date.now()}` };
      const provider = (account.provider || "cloudflare") === "webdav" ? "webdav" : "cloudflare";
      const currentProvider = (current?.provider || "cloudflare") === "webdav" ? "webdav" : "cloudflare";
      if (provider === currentProvider) throw new Error("目标连接必须使用另一种存储方式");

      const emitStage = (stage, message, current = 0, total = 0) =>
        emit("storage:switch_progress", { stage, message, current, total });
      emitStage("verify", "正在验证新存储账号的连通性与可写性...");
      await delay(600);
      emitStage("source_sync", "正在同步当前连接中的游戏数据...", 1, Math.max(state.games.length, 1));
      await delay(500);
      if (!request?.useLocalData) {
        const transactionId = `migration-${Date.now()}`;
        storageMigration = { transactionId, request: clone(request) };
        state.storageMigration = { transactionId, phase: "copying", conflictGameId: state.games[0]?.id || "", lastError: "目标存储已有数据" };
        return { snapshot: snapshot(), status: "paused", transactionId, conflictGameId: state.games[0]?.id || "", message: "目标存储已有数据，请取消切换或使用本地数据。" };
      }

      emitStage("inventory", "正在生成存档、封面和历史备份清单...");
      await delay(350);
      const targets = state.games.filter((game) => game.savePath && game.sync?.enabled !== false);
      const total = Math.max(targets.length, 1);
      for (const [index, game] of targets.entries()) {
        emitStage("copy", `正在迁移「${game.name}」的数据...`, index + 1, total);
        await delay(320);
      }
      emitStage("target", "正在发布并复核目标端目录...");
      await delay(350);
      emitStage("handoff", "正在提交存储连接交接...");
      await delay(350);
      emitStage("commit", "正在切换本地连接和同步路由...");
      await delay(300);
      for (const acc of state.accounts) {
        acc.enabled = false;
        acc.isPrimary = false;
      }
      const next = {
        accountId: "",
        apiToken: "",
        d1DatabaseId: "",
        r2Bucket: "",
        r2AccessKeyId: "",
        r2SecretAccessKey: "",
        webdavUrl: "",
        webdavUsername: "",
        webdavPassword: "",
        webdavRoot: "",
        ...account,
        name: account.name || (provider === "webdav" ? "WebDAV 主账号" : "Cloudflare 主账号"),
        provider,
        isPrimary: true,
        enabled: true,
        usedBytes: 0,
        lastVerifiedAt: nowIso(),
        verificationState: "valid",
        lastError: "",
        usageWarning: "",
      };
      const existingIndex = state.accounts.findIndex((candidate) => candidate.id === next.id);
      if (existingIndex >= 0) state.accounts[existingIndex] = next;
      else state.accounts.unshift(next);
      let secondaryNumber = 0;
      for (const candidate of state.accounts) {
        candidate.name = candidate.id === next.id ? "主账号" : `副账号 ${++secondaryNumber}`;
      }
      for (const game of state.games) {
        game.storageAccountId = next.id;
        game.backupStorageAccountId = next.id;
        game.anchor = { lastRemoteVersion: 0, lastManifest: null };
      }
      for (const [index, game] of targets.entries()) {
        emitStage("sync", `正在新连接同步「${game.name}」...`, index + 1, total);
        await delay(420);
        game.lastSync = { status: "success", message: "目标连接首次同步完成", uploaded: 18e6, downloaded: 0, conflicts: 0, syncedAt: nowIso() };
        state.activities.unshift({ id: `a-${Date.now()}-${index}`, gameId: game.id, gameName: game.name, accountId: next.id, status: "success", message: "存储切换：目标连接首次同步", uploaded: 18e6, downloaded: 0, conflicts: 0, startedAt: nowIso(), endedAt: nowIso() });
      }
      emitStage("done", "存储切换完成");
      storageMigration = null;
      state.storageMigration = null;
      return { snapshot: snapshot(), status: "completed", transactionId: `migration-${Date.now()}`, message: "存储切换完成" };
    },
    async ResumeStorageMigration(request) {
      if (!state.recoveryStatus.hasRecoveryPassword) throw new Error("恢复密码不能为空");
      if (!storageMigration || storageMigration.transactionId !== request?.transactionId) throw new Error("迁移事务已变化");
      if (request?.conflictChoice !== "local") {
        return { snapshot: snapshot(), status: "retryable", transactionId: storageMigration.transactionId, message: "请选择使用本地数据后继续。" };
      }
      const pending = storageMigration;
      storageMigration = null;
      return impl.SwitchStoragePrimary({ ...pending.request, useLocalData: true });
    },
    async CancelStorageMigration(transactionId) {
      if (!storageMigration || storageMigration.transactionId !== transactionId) throw new Error("迁移事务已变化");
      storageMigration = null;
      state.storageMigration = null;
      await delay(200);
      return snapshot();
    },
    async CheckForUpdates() {
      await delay(1100);
      return { status: "available", currentVersion: "0.9.0", latestVersion: "1.0.0", channel: "stable", platform: "windows/amd64", notes: "· 全新 Paper Atelier 界面\n· 更快的同步引擎\n· 修复若干问题", publishedAt: nowIso(), asset: { url: "https://example.com/a.zip", sha256: "x", size: 48e6 }, message: "发现新版本" };
    },
    async DownloadUpdate(req) {
      await delay(1800);
      return { version: req.version, archivePath: "C:/temp/update.zip", sha256: "x", size: 48e6 };
    },
    async ApplyUpdateAndRestart() {
      await delay(400);
    },
  };

  // 背景目录同步状态心跳：对齐真实后端词表 queued → syncing → succeeded
  setTimeout(() => emit("catalog:sync_state", { status: "queued", message: "目录变更已排队" }), 900);
  setTimeout(() => emit("catalog:sync_state", { status: "syncing", message: "正在同步云端目录" }), 1700);
  setTimeout(() => emit("catalog:sync_state", { status: "succeeded", message: "云端目录已同步" }), 2600);

  return {
    on,
    call(name, ...args) {
      const fn = impl[name];
      if (!fn) return Promise.reject(new Error(`mock 未实现: ${name}`));
      return fn(...args);
    },
  };
}
