// ============================================================
// api.js —— 后端桥。Wails 环境用真实绑定；浏览器试驾自动换 mock。
// 视图/内核只经由本模块触达后端。
// ============================================================

import * as App from "../wailsjs/go/main/App.js";
import { createMockBackend } from "./mock.js";

const isWails = Boolean(window.go?.main?.App || window.runtime);

const BINDING_NAMES = [
  "ApplyUpdateAndRestart",
  "Bootstrap",
  "CancelStorageMigration",
  "CheckForUpdates",
  "CreateGameBackup",
  "DeleteAccount",
  "DeleteGame",
  "DeleteGameBackup",
  "DownloadUpdate",
  "ExportAppBackup",
  "GetAppInfo",
  "GetGameBackups",
  "GetRAWGGame",
  "ImportAppBackup",
  "IsFirstLaunch",
  "LaunchAndMonitorGame",
  "OpenPath",
  "PickFile",
  "PickFolder",
  "PrepareGameLaunch",
  "ReorderGames",
  "RequestDeleteGame",
  "ResolveCoverSource",
  "RestoreFromPrimary",
  "RestoreGameBackup",
  "ResumeStorageMigration",
  "RunSync",
  "RunSyncAll",
  "SaveAccount",
  "SaveGame",
  "SavePreferences",
  "SearchRAWGGames",
  "SearchSteamGridDBGames",
  "SetRecoveryPassword",
  "SwitchStoragePrimary",
  "UpdateSidebarNavOrder",
  "UpdateTagOrder",
  "VerifyAccount",
];

const mock = isWails ? null : createMockBackend();

export const api = { isMock: !isWails };

for (const name of BINDING_NAMES) {
  api[name] = isWails
    ? (...args) => App[name](...args)
    : (...args) => mock.call(name, ...args);
}

/* ---------------- 事件订阅 ---------------- */

api.onEvent = (topic, handler) => {
  if (isWails) {
    // 动态取运行时，避免浏览器环境下模块副作用
    return window.runtime?.EventsOn?.(topic, handler) ?? (() => {});
  }
  return mock.on(topic, handler);
};

/* ---------------- 窗口控制 ---------------- */

api.window = {
  minimise: () => window.runtime?.WindowMinimise?.(),
  toggleMaximise: () => window.runtime?.WindowToggleMaximise?.(),
  isMaximised: () => window.runtime?.WindowIsMaximised?.() ?? Promise.resolve(false),
  hide: () => window.runtime?.WindowHide?.(),
};

/* ---------------- 封面解析（缓存 + 并发去重） ---------------- */

const coverCache = new Map();
const coverInflight = new Map();

api.resolveCover = (ref) => {
  const key = String(ref || "");
  if (!key) return Promise.resolve("");
  if (/^(https?:|data:)/i.test(key)) return Promise.resolve(key);
  if (coverCache.has(key)) return Promise.resolve(coverCache.get(key));
  if (coverInflight.has(key)) return coverInflight.get(key);
  const req = Promise.resolve(api.ResolveCoverSource(key))
    .then((src) => {
      if (src) coverCache.set(key, src);
      coverInflight.delete(key);
      return src || "";
    })
    .catch((err) => {
      coverInflight.delete(key);
      throw err;
    });
  coverInflight.set(key, req);
  return req;
};

api.invalidateCover = (ref) => {
  coverCache.delete(String(ref || ""));
};
