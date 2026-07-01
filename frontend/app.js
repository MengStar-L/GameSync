import {
  ApplyUpdateAndRestart as WailsApplyUpdateAndRestart,
  Bootstrap as WailsBootstrap,
  CheckForUpdates as WailsCheckForUpdates,
  CreateGameBackup as WailsCreateGameBackup,
  DeleteAccount as WailsDeleteAccount,
  DeleteGame as WailsDeleteGame,
  DeleteGameBackup as WailsDeleteGameBackup,
  DownloadUpdate as WailsDownloadUpdate,
  ExportAppBackup as WailsExportAppBackup,
  GetAppInfo as WailsGetAppInfo,
  GetGameBackups as WailsGetGameBackups,
  GetRAWGGame as WailsGetRAWGGame,
  ImportAppBackup as WailsImportAppBackup,
  IsFirstLaunch as WailsIsFirstLaunch,
  LaunchAndMonitorGame as WailsLaunchAndMonitorGame,
  OpenPath as WailsOpenPath,
  PickFile as WailsPickFile,
  PickFolder as WailsPickFolder,
  PrepareGameLaunch as WailsPrepareGameLaunch,
  RequestDeleteGame as WailsRequestDeleteGame,
  ResolveCoverSource as WailsResolveCoverSource,
  ReorderGames as WailsReorderGames,
  RestoreFromPrimary as WailsRestoreFromPrimary,
  RestoreGameBackup as WailsRestoreGameBackup,
  RunSync as WailsRunSync,
  SaveAccount as WailsSaveAccount,
  SaveGame as WailsSaveGame,
  SavePreferences as WailsSavePreferences,
  SearchRAWGGames as WailsSearchRAWGGames,
  SearchSteamGridDBGames as WailsSearchSteamGridDBGames,
  UpdateTagOrder as WailsUpdateTagOrder,
  VerifyAccount as WailsVerifyAccount,
} from "./wailsjs/go/main/App.js";

import {
  Activity,
  ArrowDown,
  ArrowUp,
  Clock3,
  Cloud,
  Eye,
  EyeOff,
  createIcons,
  FolderOpen,
  Gamepad2,
  HardDrive,
  Heart,
  History,
  Image,
  Menu,
  RefreshCw,
  Search,
  Settings,
  Tags,
  TriangleAlert,
  X,
  Play,
  Archive,
  Info,
  Pin,
  PinOff,
  Trash2,
  CheckCircle,
  XCircle,
  Rocket,
  CloudUpload,
  Download,
  Upload,
} from "lucide";
import { EventsOn } from "./wailsjs/runtime/runtime.js";

const ICON_ATTRS = {
  width: "18",
  height: "18",
  "stroke-width": "1.9",
};

const LUCIDE_ICONS = {
  Activity,
  ArrowDown,
  ArrowUp,
  Clock3,
  Cloud,
  Eye,
  EyeOff,
  FolderOpen,
  Gamepad2,
  HardDrive,
  Heart,
  History,
  Image,
  Menu,
  RefreshCw,
  Search,
  Settings,
  Tags,
  TriangleAlert,
  X,
  Play,
  Archive,
  Info,
  Pin,
  PinOff,
  Trash2,
  CheckCircle,
  XCircle,
  Rocket,
  CloudUpload,
  Download,
  Upload,
};

/* ========== 自定义下拉选择组件 ========== */
class CustomSelect {
  static instances = [];
  static openInstance = null;

  constructor(selectEl) {
    this.select = selectEl;
    this.isOpen = false;
    this.highlightIndex = -1;
    this._build();
    this._bind();
    CustomSelect.instances.push(this);
  }

  _build() {
    this.wrapper = document.createElement("div");
    this.wrapper.className = "custom-select";
    this.wrapper.setAttribute("tabindex", "0");
    this.wrapper.setAttribute("role", "listbox");
    this.wrapper.setAttribute(
      "aria-label",
      this.select.getAttribute("aria-label") || "",
    );

    this.trigger = document.createElement("div");
    this.trigger.className = "custom-select-trigger";
    this.trigger.setAttribute("tabindex", "-1");

    this.triggerText = document.createElement("span");
    this.triggerText.className = "custom-select-trigger-text";
    this.trigger.appendChild(this.triggerText);

    this.dropdown = document.createElement("div");
    this.dropdown.className = "custom-select-dropdown";

    this.wrapper.appendChild(this.trigger);
    this.wrapper.appendChild(this.dropdown);

    this.select.parentNode.insertBefore(this.wrapper, this.select);
    this.wrapper.appendChild(this.select);

    this._syncOptions();
  }

  _bind() {
    this.trigger.addEventListener("click", (e) => {
      e.stopPropagation();
      this.toggle();
    });

    this.wrapper.addEventListener("keydown", (e) => {
      if (e.key === "Escape") {
        this.close();
        return;
      }
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        if (this.isOpen && this.highlightIndex >= 0) {
          this._selectByIndex(this.highlightIndex);
        } else {
          this.toggle();
        }
        return;
      }
      if (e.key === "ArrowDown") {
        e.preventDefault();
        if (!this.isOpen) {
          this.open();
          return;
        }
        this._moveHighlight(1);
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        if (!this.isOpen) {
          this.open();
          return;
        }
        this._moveHighlight(-1);
        return;
      }
    });

    this.select.addEventListener("change", () => {
      this._syncOptions();
    });
  }

  _syncOptions() {
    const options = Array.from(this.select.querySelectorAll("option"));
    const currentValue = this.select.value;

    this.dropdown.innerHTML = "";
    this.highlightIndex = -1;

    options.forEach((opt, i) => {
      const el = document.createElement("div");
      el.className = "custom-select-option";
      el.textContent = opt.textContent;
      el.dataset.value = opt.value;
      el.setAttribute("role", "option");

      if (opt.value === currentValue) {
        el.classList.add("is-selected");
        this.highlightIndex = i;
      }

      el.addEventListener("click", (e) => {
        e.stopPropagation();
        this._selectByValue(opt.value);
        this.close();
      });

      el.addEventListener("mouseenter", () => {
        this._setHighlight(i);
      });

      this.dropdown.appendChild(el);
    });

    const selectedOpt = options.find((o) => o.value === currentValue);
    if (selectedOpt && selectedOpt.textContent) {
      this.triggerText.textContent = selectedOpt.textContent;
      this.triggerText.classList.remove("is-placeholder");
    } else if (options.length > 0 && !currentValue) {
      this.triggerText.textContent = options[0].textContent;
      this.triggerText.classList.remove("is-placeholder");
    } else {
      this.triggerText.textContent = "";
      this.triggerText.classList.add("is-placeholder");
    }
  }

  _selectByValue(val) {
    this.select.value = val;
    this._syncOptions();
    this.select.dispatchEvent(new Event("change", { bubbles: true }));
  }

  _selectByIndex(idx) {
    const options = Array.from(this.select.querySelectorAll("option"));
    if (options[idx]) {
      this._selectByValue(options[idx].value);
      this.close();
    }
  }

  _setHighlight(idx) {
    const items = this.dropdown.querySelectorAll(".custom-select-option");
    items.forEach((el) => el.classList.remove("is-highlighted"));
    this.highlightIndex = idx;
    if (items[idx]) {
      items[idx].classList.add("is-highlighted");
      items[idx].scrollIntoView({ block: "nearest" });
    }
  }

  _moveHighlight(dir) {
    const count = this.dropdown.querySelectorAll(
      ".custom-select-option",
    ).length;
    if (count === 0) return;
    let next = this.highlightIndex + dir;
    if (next < 0) next = count - 1;
    if (next >= count) next = 0;
    this._setHighlight(next);
  }

  open() {
    if (CustomSelect.openInstance && CustomSelect.openInstance !== this) {
      CustomSelect.openInstance.close();
    }
    this.isOpen = true;
    this.wrapper.classList.add("is-open");
    CustomSelect.openInstance = this;
  }

  close() {
    this.isOpen = false;
    this.wrapper.classList.remove("is-open");
    if (CustomSelect.openInstance === this) {
      CustomSelect.openInstance = null;
    }
  }

  toggle() {
    if (this.isOpen) this.close();
    else this.open();
  }

  sync() {
    this._syncOptions();
  }

  static closeAll() {
    CustomSelect.instances.forEach((i) => i.close());
  }
}

document.addEventListener("click", () => CustomSelect.closeAll());

const PAGE_META = {
  "all-games": {
    title: "全部游戏",
    searchPlaceholder: "搜索游戏名称、标签...",
    primaryText: "添加游戏",
    secondaryText: "同步全部",
  },
  "favorite-games": {
    title: "常玩游戏",
    searchPlaceholder: "搜索常玩游戏...",
    primaryText: "添加游戏",
    secondaryText: "同步全部",
  },
  "all-tags": {
    title: "全部标签",
    searchPlaceholder: "搜索标签...",
    primaryText: "添加游戏",
    secondaryText: "",
  },
  accounts: {
    title: "Cloudflare 账号",
    searchPlaceholder: "搜索账号别名、Bucket、D1 ID...",
    primaryText: "添加账号",
    secondaryText: "刷新数据",
  },
  activities: {
    title: "同步状态",
    searchPlaceholder: "搜索游戏名、同步结果...",
    primaryText: "刷新数据",
    secondaryText: "",
  },
  settings: {
    title: "设置",
    searchPlaceholder: "搜索设置项...",
    primaryText: "添加账号",
    secondaryText: "同步全部",
  },
};

const STORAGE_KEYS = {
  sidebar: "gamesync.sidebar.expanded",
  favorites: "gamesync.favoriteGames",
};

const RESERVED_PLATFORM_TAGS = new Set(["Steam 游戏", "第三方游戏"]);

const R2_FREE_TIER_STORAGE_BYTES = 10 * 1024 * 1024 * 1024;

const App = {
  state: {
    page: "all-games",
    filterTag: "",
    snapshot: null,
    searchQuery: "",
    searchDebounce: null,
    sidebarExpanded: true,
    favoriteGames: [],
    backupCounts: {},
    backupCountRequests: {},
    pendingConflictGameId: "",
    pendingConflictMessage: "",
    pendingConflictAction: "",
    currentGameId: "",
    pendingLaunchGameId: "",
    verifyingAccountId: "",
    globalNetworkState: {
      catalog: { status: "checking", message: "检测中" },
      foreground: { status: "", message: "", expiresAt: 0 },
    },
    lastSnapshotRefreshAt: 0,
    lastLocalSnapshotEchoSignature: "",
    lastLocalSnapshotEchoAt: 0,
    pendingDeletedGameIds: new Set(),
    pendingDeletedGamesById: {},
    pendingDeletedBackupsByGame: {},
    startupQuietUntil: 0,
    pendingStartupRender: false,
    appInfo: null,
    updateCheck: null,
    updateDownload: null,
    updating: false,
  },

  async init() {
    this.beginStartupQuietPeriod();
    this.bridge = this.createBridge();
    this.coverSourceCache = new Map();
    this.coverSourceInflight = new Map();
    this.cacheDom();
    this.initCustomSelects();
    this.restoreSidebar();
    this.loadFavoriteGames();
    this.bindEvents();
    this.bindExternalRefreshEvents();
    this.bindRuntimeEvents();
    this.bindDragAndDrop();
    this.updateNetworkStatus("checking");
    await this.refreshSnapshot("已加载本地状态");
    await this.refreshAppInfo();
    this.finishStartupVisuals();
    this.checkFirstLaunchRestore();
  },

  beginStartupQuietPeriod(duration = 4000) {
    this.state.startupQuietUntil = Date.now() + duration;
    this.state.pendingStartupRender = false;
    window.setTimeout(() => this.finishStartupQuietPeriod(), duration);
  },

  finishStartupQuietPeriod() {
    this.state.startupQuietUntil = 0;
    if (!this.state.pendingStartupRender) {
      return;
    }
    this.state.pendingStartupRender = false;
    if (
      ["accounts", "activities", "settings", "all-tags"].includes(
        this.state.page,
      )
    ) {
      this.renderDataViews({ renderSettings: true });
    }
  },

  isInStartupQuietPeriod() {
    return Date.now() < (this.state.startupQuietUntil || 0);
  },

  finishStartupVisuals() {
    window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => {
        document.body.classList.remove("app-booting");
      });
    });
  },

  cacheDom() {
    this.dom = {
      sidebar: document.getElementById("icon-sidebar"),
      pinnedTagsNav: document.getElementById("pinned-tags-nav"),
      pageTitle: document.getElementById("page-title"),
      searchInput: document.getElementById("global-search-input"),
      searchClear: document.getElementById("search-clear-btn"),
      networkStatus: document.getElementById("network-status"),
      networkStatusText: document.getElementById("network-status-text"),
      topbarPrimaryBtn: document.getElementById("topbar-primary-btn"),
      topbarSecondaryBtn: document.getElementById("topbar-secondary-btn"),
      gamesGrid: document.getElementById("games-grid"),
      gamesEmpty: document.getElementById("games-empty-state"),
      favoriteGamesGrid: document.getElementById("favorite-games-grid"),
      favoriteGamesEmpty: document.getElementById("favorite-games-empty-state"),
      tagsGrid: document.getElementById("tags-grid"),
      tagsEmpty: document.getElementById("tags-empty-state"),
      syncSummaryCard: document.getElementById("sync-summary-card"),
      accountsGrid: document.getElementById("accounts-grid"),
      accountsEmpty: document.getElementById("accounts-empty-state"),
      activityList: document.getElementById("activity-list"),
      activityEmpty: document.getElementById("activity-empty-state"),
      localSettingsList: document.getElementById("local-settings-list"),
      deviceInfoList: document.getElementById("device-info-list"),
      architectureInfo: document.getElementById("architecture-info"),
      syncOverviewList: document.getElementById("sync-overview-list"),
      appUpdateInfo: document.getElementById("app-update-info"),
      appUpdateStatus: document.getElementById("app-update-status"),
      checkUpdateBtn: document.getElementById("check-update-btn"),
      applyUpdateBtn: document.getElementById("apply-update-btn"),
      gameModal: document.getElementById("game-modal"),
      accountModal: document.getElementById("account-modal"),
      conflictModal: document.getElementById("conflict-modal"),
      gameForm: document.getElementById("game-form"),
      accountForm: document.getElementById("account-form"),
      preferencesForm: document.getElementById("preferences-form"),
      gameSaveBtn: document.getElementById("game-save-btn"),
      toastContainer: document.getElementById("toast-container"),
      coverPreviewBox: document.getElementById("cover-preview-box"),
      coverPreviewImage: document.getElementById("cover-preview-image"),
      coverPreviewPlaceholder: document.getElementById(
        "cover-preview-placeholder",
      ),
      gameAccountSelect: document.getElementById("game-account-id"),
      modalSyncBtn: document.getElementById("modal-sync-btn"),
      fetchRawgBtn: document.getElementById("fetch-rawg-btn"),
      fetchSgdbBtn: document.getElementById("fetch-sgdb-btn"),
      rawgSearchBtn: document.getElementById("rawg-search-btn"),
      rawgSearchInput: document.getElementById("rawg-search-input"),
      rawgSearchResults: document.getElementById("rawg-search-results"),
      sgdbSearchBtn: document.getElementById("sgdb-search-btn"),
      sgdbSearchInput: document.getElementById("sgdb-search-input"),
      sgdbSearchResults: document.getElementById("sgdb-search-results"),
      gameDescription: document.getElementById("game-description"),
      gameTagSuggestions: document.getElementById("game-tag-suggestions"),
      gameTagSuggestionsGroup: document.getElementById(
        "game-tag-suggestions-group",
      ),
      gameTagsDisplay: document.getElementById("game-tags-display"),
      prefRawgApiKey: document.getElementById("pref-rawg-api-key"),
      prefSgdbApiKey: document.getElementById("pref-sgdb-api-key"),
      pages: {
        "all-games": document.getElementById("page-all-games"),
        "favorite-games": document.getElementById("page-favorite-games"),
        "all-tags": document.getElementById("page-all-tags"),
        accounts: document.getElementById("page-accounts"),
        activities: document.getElementById("page-activities"),
        settings: document.getElementById("page-settings"),
      },
    };
  },

  initCustomSelects() {
    this.customSelects = {};
    document.querySelectorAll("select.form-input").forEach((sel) => {
      this.customSelects[sel.id] = new CustomSelect(sel);
    });
  },

  syncCustomSelect(id) {
    if (this.customSelects && this.customSelects[id]) {
      this.customSelects[id].sync();
    }
  },

  createBridge() {
    if (window.go?.main?.App) {
      return {
        Bootstrap: WailsBootstrap,
        GetAppInfo: WailsGetAppInfo,
        CheckForUpdates: WailsCheckForUpdates,
        DownloadUpdate: WailsDownloadUpdate,
        ApplyUpdateAndRestart: WailsApplyUpdateAndRestart,
        CreateGameBackup: WailsCreateGameBackup,
        SaveAccount: WailsSaveAccount,
        RestoreFromPrimary: WailsRestoreFromPrimary,
        DeleteAccount: WailsDeleteAccount,
        SaveGame: WailsSaveGame,
        DeleteGame: WailsDeleteGame,
        RequestDeleteGame: WailsRequestDeleteGame,
        DeleteGameBackup: WailsDeleteGameBackup,
        ExportAppBackup: WailsExportAppBackup,
        GetGameBackups: WailsGetGameBackups,
        ImportAppBackup: WailsImportAppBackup,
        IsFirstLaunch: WailsIsFirstLaunch,
        LaunchAndMonitorGame: WailsLaunchAndMonitorGame,
        SavePreferences: WailsSavePreferences,
        ReorderGames: WailsReorderGames,
        RestoreGameBackup: WailsRestoreGameBackup,
        RunSync: WailsRunSync,
        PrepareGameLaunch: WailsPrepareGameLaunch,
        ResolveCoverSource: WailsResolveCoverSource,
        PickFolder: WailsPickFolder,
        PickFile: WailsPickFile,
        OpenPath: WailsOpenPath,
        UpdateTagOrder: WailsUpdateTagOrder,
        VerifyAccount: WailsVerifyAccount,
        SearchRAWGGames: WailsSearchRAWGGames,
        SearchSteamGridDBGames: WailsSearchSteamGridDBGames,
        GetRAWGGame: WailsGetRAWGGame,
      };
    }

    // Browser preview fallback. These demo IDs are only used when the Wails bridge
    // is unavailable, so they won't affect real app data.
    const now = new Date().toISOString();
    const mockSnapshot = {
      schemaVersion: 1,
      dataDir: "C:/Users/Example/AppData/Roaming/GameSync",
      state: {
        device: {
          id: "preview-device",
          name: "预览设备",
          platform: "windows/amd64",
          lastStartedAt: now,
        },
        accounts: [
          {
            id: "preview-account-main",
            name: "主账号",
            accountId: "cf-demo-account",
            apiToken: "******",
            d1DatabaseId: "d1-demo-id",
            r2Bucket: "gamesync-demo",
            r2AccessKeyId: "R2KEY",
            r2SecretAccessKey: "******",
            isPrimary: true,
            enabled: true,
            usedBytes: 73400320,
            lastVerifiedAt: now,
            tokenExpiresAt: "2026-12-31T23:59:59.000Z",
            lastError: "",
            usageWarning: "",
            verificationState: "valid",
            credentialsBackedUp: true,
          },
        ],
        games: [
          {
            id: "preview-game-wa2",
            name: "白色相簿2",
            installPath: "D:/Games/WA2/WA2.exe",
            savePath: "C:/Users/Example/Documents/My Games/WA2",
            coverPath: "",
            tags: ["Gal", "汉化"],
            storageAccountId: "preview-account-main",
            sync: {
              enabled: true,
              includePatterns: ["*"],
              excludePatterns: [],
              conflictStrategy: "manual",
            },
            anchor: {
              lastRemoteVersion: 2,
              lastManifest: {
                version: 2,
                generatedAt: now,
                files: [],
                totalBytes: 1024,
                hash: "preview-hash",
              },
            },
            lastSync: {
              status: "success",
              message: "预览模式：最近一次同步成功",
              uploaded: 2,
              downloaded: 0,
              conflicts: 0,
              syncedAt: now,
            },
          },
          {
            id: "preview-game-summer-pockets",
            name: "Summer Pockets",
            installPath: "",
            savePath: "D:/Save/SummerPockets",
            coverPath: "",
            tags: ["Gal", "Key"],
            storageAccountId: "",
            sync: {
              enabled: false,
              includePatterns: ["*"],
              excludePatterns: [],
              conflictStrategy: "local",
            },
            anchor: {
              lastRemoteVersion: 0,
              lastManifest: {
                version: 0,
                generatedAt: "",
                files: [],
                totalBytes: 0,
                hash: "",
              },
            },
            lastSync: {
              status: "warning",
              message: "预览模式：尚未配置完整同步路径",
              uploaded: 0,
              downloaded: 0,
              conflicts: 0,
              syncedAt: now,
            },
          },
        ],
        preferences: {
          autoSyncOnLaunch: true,
          startupSyncMode: "smart",
          conflictPolicy: "manual",
          rawgApiKey: "preview-rawg-key",
          steamGridDbApiKey: "preview-sgdb-key",
        },
        recoveryStatus: {
          hasRecoveryPassword: true,
          remoteCatalogAvailable: true,
          pendingCredentialBackup: false,
          lastCatalogSyncAt: now,
          lastCredentialBackupAt: now,
          lastRecoveryError: "",
        },

        activities: [
          {
            id: "preview-activity-sync",
            gameId: "preview-game-wa2",
            gameName: "白色相簿2",
            accountId: "preview-account-main",
            status: "success",
            message: "预览模式：同步完成",
            uploaded: 2,
            downloaded: 0,
            conflicts: 0,
            startedAt: now,
            endedAt: now,
          },
        ],
      },
    };

    const rawgCatalog = [
      {
        rawgId: 100001,
        rawgSlug: "white-album-2",
        rawgUrl: "https://rawg.io/games/white-album-2",
        name: "WHITE ALBUM2",
        coverPath:
          "https://media.rawg.io/media/games/e9a/e9a7dcea4b4f11f0d8907bb4f094f224.jpg",
        description:
          "冬马、雪菜与春希之间的关系在漫长的冬季里逐渐失衡，情感张力与日常细节共同构成了这部经典视觉小说。",
        released: "2010-03-26",
        rating: 4.6,
        ratingTop: 5,
        metacritic: 88,
        genres: ["Adventure", "Visual Novel"],
        platforms: ["PC"],
        developers: ["Leaf"],
        publishers: ["AQUAPLUS"],
        website: "https://aquaplus.jp/wa2/",
        rawgTags: [
          "Visual Novel",
          "Story Rich",
          "Romance",
          "Anime",
          "Choices Matter",
        ],
      },
      {
        rawgId: 100002,
        rawgSlug: "summer-pockets",
        rawgUrl: "https://rawg.io/games/summer-pockets",
        name: "Summer Pockets",
        coverPath:
          "https://media.rawg.io/media/screenshots/7e0/7e0fd6f348f09d1044f4df5f22979d8d.jpg",
        description:
          "Key 的代表作之一，以暑假群像与岛屿日常为核心，兼具轻松节奏与温柔的情感推进。",
        released: "2018-06-29",
        rating: 4.4,
        ratingTop: 5,
        metacritic: 84,
        genres: ["Adventure", "Visual Novel"],
        platforms: ["PC"],
        developers: ["Key"],
        publishers: ["VisualArts"],
        website: "https://key.visualarts.gr.jp/summer/",
        rawgTags: [
          "Visual Novel",
          "Anime",
          "Atmospheric",
          "Story Rich",
          "Emotional",
        ],
      },
      {
        rawgId: 100003,
        rawgSlug: "nekopara-vol-1",
        rawgUrl: "https://rawg.io/games/nekopara-vol-1",
        name: "NEKOPARA Vol. 1",
        coverPath:
          "https://media.rawg.io/media/screenshots/933/933c9d9a10f5d457f41726b2dfcae6ea.jpg",
        description:
          "水无月嘉祥经营着自己的蛋糕店，并与可爱的猫娘们展开轻松治愈的日常故事。",
        released: "2014-12-29",
        rating: 4.2,
        ratingTop: 5,
        metacritic: 80,
        genres: ["Adventure", "Visual Novel"],
        platforms: ["PC", "Nintendo Switch"],
        developers: ["NEKO WORKs"],
        publishers: ["Sekai Project"],
        website: "https://nekopara.com/",
        rawgTags: ["Visual Novel", "Anime", "Cute", "Story Rich", "Casual"],
      },
    ];

    const clone = (value) => JSON.parse(JSON.stringify(value));
    const upsert = (list, item) => {
      const index = list.findIndex((entry) => entry.id === item.id);
      if (index >= 0) {
        list[index] = item;
      } else {
        list.push(item);
      }
    };

    return {
      async Bootstrap() {
        return clone(mockSnapshot);
      },
      async GetAppInfo() {
        return {
          version: "0.1.0",
          commit: "preview",
          buildDate: now,
          updateChannel: "stable",
          updateRepo: "",
          updateManifestUrl: "",
          platform: "windows-amd64",
        };
      },
      async CheckForUpdates() {
        return {
          status: "latest",
          currentVersion: "0.1.0",
          latestVersion: "0.1.0",
          channel: "stable",
          platform: "windows-amd64",
          notes: "",
          message: "预览模式：当前已是最新版本",
        };
      },
      async DownloadUpdate(request) {
        return {
          version: request.version,
          archivePath: "C:/Users/Example/AppData/Roaming/GameSync/updates/mock.zip",
          sha256: request.asset?.sha256 || "",
          size: request.asset?.size || 0,
        };
      },
      async ApplyUpdateAndRestart() {
        return undefined;
      },
      async SaveAccount(account) {
        const current = mockSnapshot.state.accounts.find(
          (item) => item.id === account.id,
        );
        const isFirstAccount = mockSnapshot.state.accounts.length === 0;
        account.id = account.id || crypto.randomUUID();
        account.usedBytes = current?.usedBytes || account.usedBytes || 0;
        account.lastVerifiedAt =
          current?.lastVerifiedAt || account.lastVerifiedAt || null;
        account.lastError = current?.lastError || account.lastError || "";
        account.usageWarning =
          current?.usageWarning || account.usageWarning || "";
        account.verificationState =
          account.verificationState || current?.verificationState || "pending";
        account.isPrimary = current?.isPrimary ?? isFirstAccount;
        upsert(mockSnapshot.state.accounts, account);
        let nextSecondary = 1;
        mockSnapshot.state.accounts.forEach((item) => {
          item.name = item.isPrimary ? "主账号" : `副账号 ${nextSecondary++}`;
        });
        return clone(mockSnapshot);
      },
      async RestoreFromPrimary() {
        mockSnapshot.state.recoveryStatus.remoteCatalogAvailable = true;
        return clone(mockSnapshot);
      },
      async VerifyAccount(accountId) {
        const account = mockSnapshot.state.accounts.find(
          (item) => item.id === accountId,
        );
        if (!account) {
          throw new Error("未找到对应 Cloudflare 账号");
        }
        account.lastVerifiedAt = new Date().toISOString();
        account.lastError = "";
        account.usageWarning = "";
        account.verificationState = "valid";
        account.usedBytes = account.usedBytes || 73400320;
        return clone(mockSnapshot);
      },

      async DeleteAccount(accountId) {
        mockSnapshot.state.accounts = mockSnapshot.state.accounts.filter(
          (item) => item.id !== accountId,
        );
        mockSnapshot.state.games = mockSnapshot.state.games.map((game) => ({
          ...game,
          storageAccountId:
            game.storageAccountId === accountId ? "" : game.storageAccountId,
        }));
        return clone(mockSnapshot);
      },
      async SaveGame(game) {
        const current = mockSnapshot.state.games.find(
          (item) => item.id === game.id,
        );
        game.id = game.id || crypto.randomUUID();
        game.anchor = current?.anchor ||
          game.anchor || {
            lastRemoteVersion: 0,
            lastManifest: {
              version: 0,
              generatedAt: "",
              files: [],
              totalBytes: 0,
              hash: "",
            },
          };
        game.lastSync = current?.lastSync || game.lastSync || null;
        upsert(mockSnapshot.state.games, game);
        return clone(mockSnapshot);
      },
      async DeleteGame(gameId) {
        mockSnapshot.state.games = mockSnapshot.state.games.filter(
          (item) => item.id !== gameId,
        );
        mockSnapshot.state.preferences.favoriteGames = (
          mockSnapshot.state.preferences.favoriteGames || []
        ).filter((id) => id !== gameId);
        return clone(mockSnapshot);
      },
      async RequestDeleteGame(gameId) {
        mockSnapshot.state.games = mockSnapshot.state.games.filter(
          (item) => item.id !== gameId,
        );
        mockSnapshot.state.preferences.favoriteGames = (
          mockSnapshot.state.preferences.favoriteGames || []
        ).filter((id) => id !== gameId);
      },
      async SavePreferences(preferences) {
        mockSnapshot.state.preferences = {
          ...(mockSnapshot.state.preferences || {}),
          ...preferences,
        };
        return clone(mockSnapshot);
      },
      async RunSync(request) {
        const game = mockSnapshot.state.games.find(
          (item) => item.id === request.gameId,
        );
        if (game) {
          if (
            !request.conflictChoice &&
            game.sync?.conflictStrategy === "manual"
          ) {
            game.lastSync = {
              status: "conflict",
              message: "预览模式：检测到本地与云端冲突，请手动选择保留哪一侧",
              uploaded: 0,
              downloaded: 0,
              conflicts: 1,
              syncedAt: new Date().toISOString(),
            };
          } else {
            game.lastSync = {
              status: "success",
              message: `预览模式：已按${request.conflictChoice === "remote" ? "云端" : "本地"}策略完成同步`,
              uploaded: 1,
              downloaded: request.conflictChoice === "remote" ? 1 : 0,
              conflicts: 0,
              syncedAt: new Date().toISOString(),
            };
          }
          mockSnapshot.state.activities.unshift({
            id: crypto.randomUUID(),
            gameId: game.id,
            gameName: game.name,
            accountId: game.storageAccountId || "",
            status: game.lastSync.status,
            message: game.lastSync.message,
            uploaded: game.lastSync.uploaded,
            downloaded: game.lastSync.downloaded,
            conflicts: game.lastSync.conflicts,
            startedAt: new Date().toISOString(),
            endedAt: new Date().toISOString(),
          });
        }
        return clone(mockSnapshot);
      },
      async PickFolder(defaultDirectory = "") {
        return (
          window.prompt("预览模式：请输入目录路径", defaultDirectory) || ""
        );
      },
      async PickFile(defaultDirectory = "") {
        return (
          window.prompt("预览模式：请输入文件路径", defaultDirectory) || ""
        );
      },
      async ResolveCoverSource(identifier) {
        const game = (mockSnapshot.state.games || []).find(
          (item) => item.id === identifier,
        );
        if (game?.coverPath) {
          return game.coverPath;
        }
        return identifier || "";
      },

      async OpenPath(path) {
        window.alert(`预览模式无法打开路径：${path}`);
      },
      async LaunchAndMonitorGame() {},
      async PrepareGameLaunch() {
        return {
          status: "ready",
          reason: "ready",
          message: "预览模式：本地存档已是最新版本，正在启动游戏。",
          snapshot: clone(mockSnapshot),
        };
      },
      async GetGameBackups() {
        return { backups: [], partial: false, message: "", failedAccounts: [] };
      },
      async CreateGameBackup() {
        return null;
      },
      async RestoreGameBackup() {},
      async DeleteGameBackup() {},
      async ExportAppBackup() {},
      async ImportAppBackup() {},
      async IsFirstLaunch() {
        return false;
      },
      async ReorderGames(gameIds) {
        const order = new Map(gameIds.map((id, index) => [id, index]));
        mockSnapshot.state.games.sort(
          (left, right) =>
            (order.get(left.id) ?? 9999) - (order.get(right.id) ?? 9999),
        );
        return clone(mockSnapshot);
      },
      async UpdateTagOrder(tags) {
        mockSnapshot.state.preferences.tagOrder = tags;
        return clone(mockSnapshot);
      },
      async SearchRAWGGames(query) {
        const normalized = String(query || "")
          .trim()
          .toLowerCase();
        return mockRawgGames
          .filter(
            (item) =>
              !normalized ||
              item.name.toLowerCase().includes(normalized) ||
              item.slug.includes(normalized),
          )
          .map((item) => ({
            id: item.id,
            name: item.name,
            released: item.released,
            backgroundImage: item.backgroundImage,
            rating: item.rating,
            ratingTop: item.ratingTop,
            genres: item.genres,
            platforms: item.platforms,
          }));
      },
      async GetRAWGGame(rawgId) {
        const details = mockRawgGames.find(
          (item) => item.id === Number(rawgId),
        );
        if (!details) {
          throw new Error("预览模式：未找到 RAWG 游戏");
        }
        return clone(details);
      },
      async SearchSteamGridDBGames(query) {
        const normalized = String(query || "")
          .trim()
          .toLowerCase();
        return mockRawgGames
          .filter(
            (item) =>
              !normalized ||
              item.name.toLowerCase().includes(normalized) ||
              item.slug.includes(normalized),
          )
          .map((item) => ({
            id: item.id,
            name: item.name,
            types: item.platforms?.length ? ["steam"] : [],
            verified: true,
            coverPath:
              Array.isArray(item.coverOptions) && item.coverOptions.length
                ? item.coverOptions[0]
                : item.coverPath || "",
            coverOptions:
              Array.isArray(item.coverOptions) && item.coverOptions.length
                ? item.coverOptions
                : item.coverPath
                  ? [item.coverPath]
                  : [],
          }));
      },
    };
  },

  bindEvents() {
    document.addEventListener("contextmenu", (event) => {
      if (
        !event.target.closest(".game-card[data-game-id]") &&
        !event.target.closest(".tag-card[data-tag]")
      ) {
        event.preventDefault();
        this.hideGameContextMenu();
        this.hideTagContextMenu();
      }
    });

    document
      .getElementById("menu-toggle")
      .addEventListener("click", () => this.toggleSidebar());
    document.querySelectorAll(".nav-btn").forEach((button) => {
      button.addEventListener("click", () => this.setPage(button.dataset.page));
    });

    document
      .getElementById("sidebar-sync-btn")
      .addEventListener("click", () => this.syncAllGames());
    this.dom.topbarPrimaryBtn.addEventListener("click", () =>
      this.handleTopbarPrimaryAction(),
    );
    this.dom.topbarSecondaryBtn.addEventListener("click", () =>
      this.handleTopbarSecondaryAction(),
    );

    this.dom.searchInput.addEventListener("input", (event) =>
      this.onSearchInput(event),
    );
    this.dom.searchClear.addEventListener("click", () => this.clearSearch());

    [this.dom.gamesGrid, this.dom.favoriteGamesGrid].forEach((grid) => {
      grid.addEventListener("click", (event) =>
        this.handleGameGridClick(event),
      );
      grid.addEventListener("contextmenu", (event) =>
        this.showGameContextMenu(event),
      );
    });
    document.addEventListener("click", () => this.hideGameContextMenu());
    document
      .getElementById("game-context-menu")
      .addEventListener("click", (event) =>
        this.handleContextMenuAction(event),
      );
    this.dom.tagsGrid.addEventListener("contextmenu", (event) =>
      this.showTagContextMenu(event),
    );
    document.addEventListener("click", () => this.hideTagContextMenu());
    document
      .getElementById("tag-context-menu")
      .addEventListener("click", (event) =>
        this.handleTagContextMenuAction(event),
      );
    this.dom.pinnedTagsNav?.addEventListener("click", (event) =>
      this.handlePinnedTagNavClick(event),
    );
    this.dom.tagsGrid.addEventListener("click", (event) =>
      this.handleTagGridClick(event),
    );
    this.dom.accountsGrid.addEventListener("click", (event) =>
      this.handleAccountGridClick(event),
    );

    document
      .getElementById("modal-launch-btn")
      .addEventListener("click", () => this.launchGameFromModal());
    document
      .getElementById("modal-manage-backup-btn")
      .addEventListener("click", () =>
        this.openBackupModal(this.state.currentGameId),
      );
    document
      .getElementById("backup-create-btn")
      .addEventListener("click", () => this.createManualBackup());
    document
      .getElementById("backup-list-container")
      .addEventListener("click", (event) => this.handleBackupListClick(event));

    this.dom.localSettingsList.addEventListener("click", (event) =>
      this.handleLocalSettingsClick(event),
    );
    document
      .getElementById("open-data-dir-btn")
      .addEventListener("click", () => this.openDataDir());
    document
      .getElementById("export-app-backup-btn")
      .addEventListener("click", () => this.exportAppBackup());
    document
      .getElementById("import-app-backup-btn")
      .addEventListener("click", () => this.importAppBackup());

    this.dom.gameTagsDisplay?.addEventListener("click", (event) => {
      const btn = event.target.closest('[data-action="remove-form-tag"]');
      if (btn?.dataset.tag) {
        this.toggleCurrentFormTag(btn.dataset.tag);
      }
    });

    const customTagInput = document.getElementById("game-tag-custom-input");
    const addCustomTag = () => {
      const val = customTagInput?.value.trim();
      if (!val) return;
      const tags = this.currentFormTags();
      if (!tags.includes(val)) {
        this.setCurrentFormTags([...tags, val]);
      }
      customTagInput.value = "";
    };
    customTagInput?.addEventListener("keydown", (e) => {
      if (e.key === "Enter") {
        e.preventDefault();
        addCustomTag();
      }
    });
    document
      .getElementById("game-tag-add-btn")
      ?.addEventListener("click", addCustomTag);

    document
      .getElementById("pick-install-path-btn")
      .addEventListener("click", async () => {
        const path = await this.bridge.PickFile(
          this.defaultInstallDirectoryForCurrentForm(),
        );
        if (path) {
          document.getElementById("game-install-path").value = path;
          this.autofillGameNameFromInstallPath(path);
        }
      });

    document
      .getElementById("pick-save-path-btn")
      .addEventListener("click", async () => {
        const path = await this.bridge.PickFolder(
          this.defaultSaveDirectoryForCurrentForm(),
        );
        if (path) {
          document.getElementById("game-save-path").value = path;
        }
      });
    document
      .getElementById("game-is-steam")
      ?.addEventListener("change", (event) =>
        this.handlePlatformToggleChange(event),
      );
    this.dom.coverPreviewBox.addEventListener("click", async () => {
      const path = await this.bridge.PickFile("");
      if (path) {
        document.getElementById("game-cover-path").value = path;
        this.updateCoverPreview(path);
      }
    });
    this.dom.fetchRawgBtn?.addEventListener("click", () => {
      const currentName =
        document.getElementById("game-name").value.trim() ||
        this.filenameWithoutExt(
          document.getElementById("game-install-path").value.trim(),
        );
      this.openRawgPicker("game-form", currentName);
    });
    this.dom.fetchSgdbBtn?.addEventListener("click", () => {
      const currentName =
        document.getElementById("game-name").value.trim() ||
        this.filenameWithoutExt(
          document.getElementById("game-install-path").value.trim(),
        );
      this.openSteamGridDBPicker("game-form", currentName);
    });
    this.dom.rawgSearchBtn?.addEventListener("click", () =>
      this.searchRawgFromPicker(),
    );
    this.dom.rawgSearchInput?.addEventListener("keydown", (event) => {
      if (event.key === "Enter") {
        event.preventDefault();
        this.searchRawgFromPicker();
      }
    });
    this.dom.rawgSearchResults?.addEventListener("click", (event) =>
      this.handleRawgSearchResultClick(event),
    );
    this.dom.sgdbSearchBtn?.addEventListener("click", () =>
      this.searchSteamGridDBFromPicker(),
    );
    this.dom.sgdbSearchInput?.addEventListener("keydown", (event) => {
      if (event.key === "Enter") {
        event.preventDefault();
        this.searchSteamGridDBFromPicker();
      }
    });
    this.dom.sgdbSearchResults?.addEventListener("click", (event) =>
      this.handleSteamGridDBSearchResultClick(event),
    );
    this.dom.gameTagSuggestions?.addEventListener("click", (event) =>
      this.handleGameTagSuggestionClick(event),
    );

    this.dom.modalSyncBtn?.addEventListener("click", () =>
      this.syncCurrentGame(),
    );
    document
      .getElementById("account-is-primary")
      ?.addEventListener("change", () => this.updateAccountD1Required());

    document
      .getElementById("game-description-edit-btn")
      ?.addEventListener("click", (event) => {
        const readEl = document.getElementById("game-description-read");
        const editEl = document.getElementById("game-description");
        const isReading = readEl.style.display !== "none";
        if (isReading) {
          readEl.style.display = "none";
          editEl.style.display = "block";
          event.target.textContent = "收起";
          editEl.focus();
        } else {
          readEl.textContent = editEl.value || "暂无简介。";
          editEl.style.display = "none";
          readEl.style.display = "block";
          event.target.textContent = "编辑";
        }
      });

    this.dom.gameForm.addEventListener("submit", (event) =>
      this.submitGameForm(event),
    );
    this.dom.accountForm.addEventListener("click", (event) =>
      this.handleAccountFormClick(event),
    );
    this.dom.accountForm.addEventListener("submit", (event) =>
      this.submitAccountForm(event),
    );
    this.dom.preferencesForm.addEventListener("submit", (event) =>
      this.submitPreferences(event),
    );
    this.dom.checkUpdateBtn?.addEventListener("click", () =>
      this.checkForUpdates(),
    );
    this.dom.applyUpdateBtn?.addEventListener("click", () =>
      this.applyAvailableUpdate(),
    );

    document.querySelectorAll("[data-close]").forEach((button) => {
      button.addEventListener("click", () =>
        this.closeModal(button.dataset.close),
      );
    });
    document.querySelectorAll(".modal-overlay").forEach((overlay) => {
      overlay.addEventListener("click", (event) => {
        if (event.target === overlay && overlay.id !== "conflict-modal") {
          this.closeModal(overlay.id);
        }
      });
    });

    document
      .getElementById("conflict-local-btn")
      .addEventListener("click", () => this.resolveConflict("local"));
    document
      .getElementById("conflict-cloud-btn")
      .addEventListener("click", () => this.resolveConflict("remote"));
    document
      .getElementById("conflict-cancel-btn")
      .addEventListener("click", () => this.closeConflictModal());
  },

  bindExternalRefreshEvents() {
    const refreshWhenSafe = () => {
      if (document.hidden || this.hasActiveModal()) {
        return;
      }
      if (Date.now() - (this.state.lastSnapshotRefreshAt || 0) < 5000) {
        return;
      }
      void this.refreshSnapshot("已同步最新数据", { silentError: true });
    };

    window.addEventListener("focus", refreshWhenSafe);
    document.addEventListener("visibilitychange", refreshWhenSafe);
    window.setInterval(refreshWhenSafe, 15000);
  },

  bindRuntimeEvents() {
    if (!window.runtime?.EventsOn) {
      return;
    }

    EventsOn("game:started", (gameId) => {
      this.state.runtimeStatus = this.state.runtimeStatus || {};
      this.state.runtimeStatus[gameId] = {
        text: "游戏中",
        icon: "gamepad-2",
        statusClass: "is-playing",
      };
      this.showToast("游戏已启动", "rocket");
      this.updateGameCardStatusDOM(gameId);
    });
    EventsOn("game:ended", (payload) => {
      const gid = this.eventGameId(payload);
      if (!gid) {
        return;
      }
      this.state.runtimeStatus = this.state.runtimeStatus || {};
      this.state.runtimeStatus[gid] = {
        text: "打包保存中",
        icon: "cloud-upload",
        statusClass: "is-syncing",
      };
      this.showToast("游戏进程已结束，时长已记录", "success");
      this.updateGameCardStatusDOM(gid);
    });
    EventsOn("game:backup_starting", (gameId) => {
      this.showToast("游戏结束，正在上传备份", "success");
    });
    EventsOn("game:backup_success", (gameId) => {
      this.state.runtimeStatus = this.state.runtimeStatus || {};
      this.state.runtimeStatus[gameId] = {
        text: "同步完成",
        icon: "check",
        statusClass: "is-success",
      };
      this.showToast("自动游戏保存完成", "success");
      this.updateGameCardStatusDOM(gameId);

      setTimeout(() => {
        if (this.state.runtimeStatus[gameId]?.statusClass === "is-success") {
          delete this.state.runtimeStatus[gameId];
          this.updateGameCardStatusDOM(gameId);
        }
      }, 3000);

      if (
        document.getElementById("backup-modal").classList.contains("active") &&
        this.state.currentGameId === gameId
      ) {
        this.openBackupModal(gameId);
      }
    });
    EventsOn("game:backup_error", (payload) => {
      const gid = this.eventGameId(payload);
      this.state.runtimeStatus = this.state.runtimeStatus || {};
      if (gid) delete this.state.runtimeStatus[gid];
      this.showToast("自动存档发生错误: " + payload?.error, "error");
    });
    EventsOn("game:backup_upload_failed", (payload) => {
      if (payload?.id === this.state.currentGameId) {
        this.syncOpenBackupModal(payload.id);
      }
    });
    EventsOn("game:backup_upload_succeeded", (payload) => {
      if (payload?.id === this.state.currentGameId) {
        this.syncOpenBackupModal(payload.id);
      }
    });
    EventsOn("game:backup_delete_failed", (payload) => {
      this.clearPendingDeletedBackup(payload?.id, payload?.filename);
      if (payload?.id === this.state.currentGameId) {
        this.syncOpenBackupModal(payload.id);
      }
      if (payload?.error) {
        this.showToast(`备份删除失败: ${payload.error}`, "error");
      }
    });
    EventsOn("game:backup_delete_succeeded", (payload) => {
      this.clearPendingDeletedBackup(payload?.id, payload?.filename);
      if (payload?.id === this.state.currentGameId) {
        this.syncOpenBackupModal(payload.id);
      }
    });

    EventsOn("sync:progress", (payload) => {
      if (!payload?.message) {
        return;
      }
      this.updateNetworkStatus("syncing", payload.message);
      if (payload.gameId) {
        this.state.runtimeStatus = this.state.runtimeStatus || {};
        this.state.runtimeStatus[payload.gameId] = {
          text: payload.message,
          icon: "refresh-cw",
          statusClass: "is-syncing",
        };
        // We do a fast-render of just the card to avoid heavy full-page re-renders on progress events
        this.updateGameCardStatusDOM(payload.gameId);
      }
    });

    EventsOn("cover:warning", (payload) => {
      if (!payload?.message) {
        return;
      }
      this.showToast(payload.message, "warning");
      this.updateNetworkStatus("offline", payload.message);
    });

    EventsOn("game:delete_queued", (payload) => {
      if (!payload?.id) {
        return;
      }
      this.updateNetworkStatus(
        "syncing",
        payload?.name
          ? `已加入删除队列：${payload.name}`
          : "已加入删除队列",
      );
    });

    EventsOn("game:delete_succeeded", (payload) => {
      const gameId = this.eventGameId(payload);
      if (!gameId) {
        return;
      }
      this.clearPendingDeletedGame(gameId);
      this.updateNetworkStatus("online", "游戏删除完成");
    });

    EventsOn("game:delete_failed", (payload) => {
      const gameId = this.eventGameId(payload);
      if (!gameId) {
        return;
      }
      if (payload?.stage === "remote_cleanup") {
        this.clearPendingDeletedGame(gameId);
        const message =
          payload?.error || "游戏已从本机移除，但云端清理失败，请稍后重试";
        this.showToast(message, "warning");
        this.updateNetworkStatus("offline", message);
        return;
      }
      this.restorePendingDeletedGame(gameId);
      const message = payload?.error || "删除游戏失败";
      this.showToast(message, "error");
      this.updateNetworkStatus("offline", message);
    });

    EventsOn("catalog:sync_state", (payload) => {
      this.setCatalogNetworkState(
        payload?.status || "checking",
        payload?.message || "",
      );
    });

    EventsOn("state:updated", (state) => {
      if (!this.state.snapshot) {
        return;
      }
      if (
        this.shouldApplyRuntimeStateSilently() ||
        this.isInStartupQuietPeriod()
      ) {
        this.applySnapshotSilently({
          ...this.state.snapshot,
          state,
        });
        if (this.isInStartupQuietPeriod()) {
          this.state.pendingStartupRender = true;
        }
        return;
      }
      this.applyStateUpdate(state);
    });

    EventsOn("catalog:sync_failed", (payload) => {
      const message = payload?.message || "后台上传数据库失败";
      this.setCatalogNetworkState("offline", "后台上传失败");
      this.showToast(`后台上传数据库失败：${message}`, "error");
    });
  },

  eventGameId(payload) {
    if (!payload || typeof payload === "string") {
      return payload || "";
    }
    return payload.gameId || payload.id || "";
  },

  bindDragAndDrop() {
    const animDuration = 220;
    let potentialDragTarget = null;
    let draggedElement = null;
    let draggedPlaceholder = null;
    let dragContainer = null;
    let dragType = "";
    let dragStartX = 0;
    let dragStartY = 0;
    let currentPointerX = 0;
    let currentPointerY = 0;
    let pointerOffsetX = 0;
    let pointerOffsetY = 0;
    let floatingLeft = 0;
    let floatingTop = 0;
    let dragging = false;
    let rafId = 0;
    let pointerId = null;
    const dropDurationForType = (type) => (type === "game" ? 240 : 220);
    const dragLiftScaleForType = (type) => (type === "game" ? 1 : 1.03);
    const dropEasingForType = (type) =>
      type === "game"
        ? "cubic-bezier(0.08, 0.82, 0.17, 1)"
        : "cubic-bezier(0.22, 1, 0.36, 1)";

    const selectorForType = (type) =>
      type === "game"
        ? ".game-card:not(.drag-placeholder)"
        : ".tag-card:not(.drag-placeholder)";
    const targetSelectorForType = (type) =>
      type === "game"
        ? ".game-card:not(.drag-placeholder)"
        : ".tag-card:not(.drag-placeholder)";
    const containerForType = (type) =>
      type === "game" ? "#games-grid, #favorite-games-grid" : "#tags-grid";
    const keyForElement = (element) =>
      element.dataset.gameId || element.dataset.tag || "";
    const isInteractiveTarget = (element) =>
      Boolean(element.closest("button, a, input, textarea, select"));

    const clearDragMarkers = () => {
      document.querySelectorAll(".game-card, .tag-card").forEach((card) => {
        card.classList.remove("dragging", "drag-over");
        card.style.transition = "";
        card.style.transform = "";
        card.style.willChange = "";
      });
    };

    const cancelPendingFrame = () => {
      if (rafId) {
        window.cancelAnimationFrame(rafId);
        rafId = 0;
      }
    };

    const updateDraggedTransform = () => {
      rafId = 0;
      if (!draggedElement) {
        return;
      }
      const tilt = getDragTilt();
      const liftScale = dragLiftScaleForType(dragType);
      draggedElement.style.left = `${floatingLeft}px`;
      draggedElement.style.top = `${floatingTop}px`;
      draggedElement.style.transform = `scale(${liftScale}) rotate(${tilt}deg)`;
    };

    const getDragTilt = () =>
      Math.max(
        Math.min(
          (currentPointerX - dragStartX) / (dragType === "game" ? 56 : 42),
          dragType === "game" ? 1.6 : 2.5,
        ),
        dragType === "game" ? -1.6 : -2.5,
      );

    const queueDraggedTransform = () => {
      if (rafId || !draggedElement) {
        return;
      }
      rafId = window.requestAnimationFrame(updateDraggedTransform);
    };

    const capturePositions = (container, selector) => {
      const positions = new Map();
      Array.from(container.querySelectorAll(selector)).forEach((element) => {
        if (element === draggedElement || element === draggedPlaceholder) {
          return;
        }
        positions.set(keyForElement(element), element.getBoundingClientRect());
      });
      return positions;
    };

    const playFlip = (container, selector, previousPositions) => {
      const cards = Array.from(container.querySelectorAll(selector));
      cards.forEach((card) => {
        if (card === draggedElement || card === draggedPlaceholder) {
          return;
        }
        const previous = previousPositions.get(keyForElement(card));
        if (!previous) {
          return;
        }
        const next = card.getBoundingClientRect();
        const dx = previous.left - next.left;
        const dy = previous.top - next.top;
        if (!dx && !dy) {
          return;
        }
        card.style.transition = "none";
        card.style.willChange = "transform";
        card.style.transform = `translate3d(${dx}px, ${dy}px, 0)`;
      });

      window.requestAnimationFrame(() => {
        cards.forEach((card) => {
          if (card === draggedElement || card === draggedPlaceholder) {
            return;
          }
          card.style.transition = `transform ${animDuration}ms cubic-bezier(0.22, 1, 0.36, 1)`;
          card.style.transform = "";
        });

        window.setTimeout(() => {
          cards.forEach((card) => {
            if (card === draggedElement || card === draggedPlaceholder) {
              return;
            }
            card.style.transition = "";
            card.style.transform = "";
            card.style.willChange = "";
          });
        }, animDuration);
      });
    };

    const commitGameOrder = (container) => {
      const visibleOrderedIds = Array.from(
        container.querySelectorAll(".game-card:not(.drag-placeholder)"),
      ).map((card) => card.dataset.gameId);
      if (visibleOrderedIds.length < 2) {
        return;
      }

      const games = [...this.getState().games];
      const globalIndices = visibleOrderedIds
        .map((id) => games.findIndex((game) => game.id === id))
        .filter((index) => index !== -1)
        .sort((left, right) => left - right);
      const gameObjects = visibleOrderedIds
        .map((id) => games.find((game) => game.id === id))
        .filter(Boolean);

      for (let index = 0; index < globalIndices.length; index++) {
        games[globalIndices[index]] = gameObjects[index];
      }

      if (this.state.snapshot) {
        this.state.snapshot.state.games = games;
        this.markLocalSnapshotEchoCandidate();
      }
      this.bridge.ReorderGames(games.map((game) => game.id)).catch((error) =>
        console.error(error),
      );
    };

    const commitTagOrder = (container) => {
      const tagNames = Array.from(
        container.querySelectorAll(".tag-card:not(.drag-placeholder)"),
      ).map((card) => card.dataset.tag);
      if (this.state.snapshot) {
        this.state.snapshot.state.preferences =
          this.state.snapshot.state.preferences || {};
        this.state.snapshot.state.preferences.tagOrder = tagNames;
        this.markLocalSnapshotEchoCandidate();
      }
      this.bridge.UpdateTagOrder(tagNames).catch((error) =>
        console.error(error),
      );
    };

    const isPointerAfterTarget = (event, target) => {
      const rect = target.getBoundingClientRect();
      const splitY = rect.top + (rect.height * 2) / 3;
      const upperBand = rect.top + rect.height / 3;

      if (event.clientY < upperBand) {
        return false;
      }
      if (event.clientY > splitY) {
        return true;
      }
      return event.clientX > rect.left + (rect.width * 2) / 3;
    };

    const reorderWithinContainer = (target, event) => {
      if (!dragging || !draggedElement || !dragContainer || !target) {
        return;
      }
      if (target === draggedElement || target === draggedPlaceholder) {
        return;
      }
      if (!dragContainer.contains(target)) {
        return;
      }

      document.querySelectorAll(".drag-over").forEach((element) => {
        if (element !== target) {
          element.classList.remove("drag-over");
        }
      });
      target.classList.add("drag-over");

      const insertAfter = isPointerAfterTarget(event, target);
      const referenceNode = insertAfter ? target.nextElementSibling : target;
      if (
        referenceNode === draggedPlaceholder ||
        (!insertAfter && target.previousElementSibling === draggedPlaceholder) ||
        (insertAfter && target.nextElementSibling === draggedPlaceholder)
      ) {
        return;
      }

      const selector = selectorForType(dragType);
      const previousPositions = capturePositions(dragContainer, selector);
      dragContainer.insertBefore(draggedPlaceholder, referenceNode);
      playFlip(dragContainer, selector, previousPositions);
    };

    const restoreDraggedElementToPlaceholder = () => {
      if (!draggedElement || !draggedPlaceholder || !dragContainer) {
        return;
      }
      draggedElement.style.position = "";
      draggedElement.style.left = "";
      draggedElement.style.top = "";
      draggedElement.style.width = "";
      draggedElement.style.height = "";
      draggedElement.style.margin = "";
      draggedElement.style.transform = "";
      dragContainer.insertBefore(draggedElement, draggedPlaceholder);
      draggedPlaceholder.remove();
      draggedPlaceholder = null;
    };

    const animateDropToPlaceholder = () =>
      new Promise((resolve) => {
        if (!draggedElement || !draggedPlaceholder) {
          resolve();
          return;
        }

        const targetRect = draggedPlaceholder.getBoundingClientRect();
        const preview = draggedElement.cloneNode(true);
        const sourceLeft = floatingLeft;
        const sourceTop = floatingTop;
        const sourceWidth =
          parseFloat(draggedElement.style.width) || draggedElement.offsetWidth;
        const sourceHeight =
          parseFloat(draggedElement.style.height) || draggedElement.offsetHeight;
        const deltaX = targetRect.left - sourceLeft;
        const deltaY = targetRect.top - sourceTop;
        const dropDuration = dropDurationForType(dragType);
        const dropEasing = dropEasingForType(dragType);
        const liftScale = dragLiftScaleForType(dragType);
        const tilt = getDragTilt();
        const startTransform = `translate3d(0, 0, 0) scale(${liftScale}) rotate(${tilt}deg)`;

        preview.classList.remove("drag-over");
        preview.classList.add("dragging", "drop-animating");
        preview.style.position = "fixed";
        preview.style.left = `${sourceLeft}px`;
        preview.style.top = `${sourceTop}px`;
        preview.style.width = `${sourceWidth}px`;
        preview.style.height = `${sourceHeight}px`;
        preview.style.margin = "0";
        preview.style.pointerEvents = "none";
        preview.style.zIndex = "1000";
        preview.style.transform = startTransform;
        document.body.appendChild(preview);

        void preview.offsetWidth;

        const animation = preview.animate(
          [
            { transform: startTransform },
            {
              transform: `translate3d(${deltaX}px, ${deltaY}px, 0) scale(1) rotate(0deg)`,
            },
          ],
          {
            duration: dropDuration,
            easing: dropEasing,
            fill: "forwards",
          },
        );

        animation.finished
          .catch(() => {})
          .finally(() => {
            preview.remove();
            resolve();
          });
      });

    const cleanup = () => {
      cancelPendingFrame();
      if (draggedElement && draggedPlaceholder && dragContainer) {
        restoreDraggedElementToPlaceholder();
      }
      if (draggedElement) {
        draggedElement.classList.remove("dragging", "drop-animating");
        draggedElement.style.position = "";
        draggedElement.style.left = "";
        draggedElement.style.top = "";
        draggedElement.style.width = "";
        draggedElement.style.height = "";
        draggedElement.style.margin = "";
        draggedElement.style.transform = "";
        draggedElement.style.willChange = "";
      }
      if (draggedPlaceholder) {
        draggedPlaceholder.remove();
        draggedPlaceholder = null;
      }
      document.querySelectorAll(".drag-over").forEach((element) =>
        element.classList.remove("drag-over"),
      );
      if (potentialDragTarget && pointerId != null) {
        try {
          potentialDragTarget.releasePointerCapture(pointerId);
        } catch (_) {}
      }
      if (draggedElement && pointerId != null) {
        try {
          draggedElement.releasePointerCapture(pointerId);
        } catch (_) {}
      }
      document.body.style.userSelect = "";
      potentialDragTarget = null;
      draggedElement = null;
      dragContainer = null;
      dragType = "";
      dragStartX = 0;
      dragStartY = 0;
      currentPointerX = 0;
      currentPointerY = 0;
      pointerOffsetX = 0;
      pointerOffsetY = 0;
      floatingLeft = 0;
      floatingTop = 0;
      dragging = false;
      pointerId = null;
    };

    const handlePointerDown = (event) => {
      if (event.button !== 0) {
        return;
      }

      const card = event.target.closest(".game-card, .tag-card");
      if (!card || isInteractiveTarget(event.target)) {
        return;
      }

      potentialDragTarget = card;
      dragContainer = card.closest(
        card.classList.contains("game-card")
          ? "#games-grid, #favorite-games-grid"
          : "#tags-grid",
      );
      dragType = card.classList.contains("game-card") ? "game" : "tag";
      dragStartX = event.clientX;
      dragStartY = event.clientY;
      currentPointerX = event.clientX;
      currentPointerY = event.clientY;
      pointerId = event.pointerId;

      if (card.setPointerCapture) {
        try {
          card.setPointerCapture(event.pointerId);
        } catch (_) {}
      }
    };

    const handlePointerMove = (event) => {
      if (!potentialDragTarget && !dragging) {
        return;
      }

      if (!dragging && potentialDragTarget) {
        const moveX = event.clientX - dragStartX;
        const moveY = event.clientY - dragStartY;
        if (Math.hypot(moveX, moveY) < 6) {
          return;
        }

        draggedElement = potentialDragTarget;
        potentialDragTarget = null;
        dragging = true;
        this.state.dragJustHappenedUntil = Date.now() + 500;
        const rect = draggedElement.getBoundingClientRect();
        pointerOffsetX = event.clientX - rect.left;
        pointerOffsetY = event.clientY - rect.top;
        floatingLeft = rect.left;
        floatingTop = rect.top;
        draggedPlaceholder = document.createElement(draggedElement.tagName);
        draggedPlaceholder.className = `${draggedElement.className} drag-placeholder`;
        if (draggedElement.dataset.renderKey) {
          draggedPlaceholder.dataset.renderKey = draggedElement.dataset.renderKey;
        }
        if (draggedElement.dataset.renderSignature) {
          draggedPlaceholder.dataset.renderSignature =
            draggedElement.dataset.renderSignature;
        }
        draggedPlaceholder.style.width = `${rect.width}px`;
        draggedPlaceholder.style.height = `${rect.height}px`;
        draggedPlaceholder.style.visibility = "hidden";
        draggedPlaceholder.style.pointerEvents = "none";
        dragContainer.insertBefore(draggedPlaceholder, draggedElement);
        document.body.appendChild(draggedElement);

        draggedElement.classList.add("dragging");
        draggedElement.style.transition = "none";
        draggedElement.style.willChange = "transform";
        draggedElement.style.position = "fixed";
        draggedElement.style.left = `${floatingLeft}px`;
        draggedElement.style.top = `${floatingTop}px`;
        draggedElement.style.width = `${rect.width}px`;
        draggedElement.style.height = `${rect.height}px`;
        draggedElement.style.margin = "0";
        document.body.style.userSelect = "none";
        queueDraggedTransform();
      }

      if (!dragging || !draggedElement) {
        return;
      }

      event.preventDefault();
      currentPointerX = event.clientX;
      currentPointerY = event.clientY;
      floatingLeft = event.clientX - pointerOffsetX;
      floatingTop = event.clientY - pointerOffsetY;
      queueDraggedTransform();

      const hovered = document.elementFromPoint(event.clientX, event.clientY);
      const target = hovered?.closest(targetSelectorForType(dragType));
      reorderWithinContainer(target, event);
    };

    const handlePointerUp = async () => {
      if (!dragging || !draggedElement || !dragContainer) {
        cleanup();
        return;
      }

      document.querySelectorAll(".drag-over").forEach((element) =>
        element.classList.remove("drag-over"),
      );
      const dropPromise = animateDropToPlaceholder();
      restoreDraggedElementToPlaceholder();
      if (dragType === "game") {
        commitGameOrder(dragContainer);
      } else if (dragType === "tag") {
        commitTagOrder(dragContainer);
      }
      this.state.dragJustHappenedUntil = Date.now() + 500;
      cleanup();
      await dropPromise;
    };

    document.addEventListener("pointerdown", handlePointerDown);
    document.addEventListener("pointermove", handlePointerMove);
    document.addEventListener("pointerup", handlePointerUp);
    document.addEventListener("pointercancel", cleanup);
    window.addEventListener("blur", cleanup);
  },

  restoreSidebar() {
    const stored = window.localStorage.getItem(STORAGE_KEYS.sidebar);
    this.state.sidebarExpanded = stored !== "false";
    this.dom.sidebar.classList.toggle("expanded", this.state.sidebarExpanded);
  },

  toggleSidebar() {
    this.state.sidebarExpanded = !this.state.sidebarExpanded;
    this.dom.sidebar.classList.toggle("expanded", this.state.sidebarExpanded);
    window.localStorage.setItem(
      STORAGE_KEYS.sidebar,
      String(this.state.sidebarExpanded),
    );
  },

  normalizeStringList(values = []) {
    const seen = new Set();
    const normalized = [];
    (Array.isArray(values) ? values : []).forEach((value) => {
      const item = String(value || "").trim();
      if (!item || seen.has(item)) {
        return;
      }
      seen.add(item);
      normalized.push(item);
    });
    return normalized;
  },

  loadFavoriteGames() {
    try {
      this.state.favoriteGames = JSON.parse(
        window.localStorage.getItem(STORAGE_KEYS.favorites) || "[]",
      );
      if (!Array.isArray(this.state.favoriteGames)) {
        this.state.favoriteGames = [];
      }
    } catch {
      this.state.favoriteGames = [];
    }
  },

  saveFavoriteGames() {
    window.localStorage.setItem(
      STORAGE_KEYS.favorites,
      JSON.stringify(this.state.favoriteGames),
    );
  },

  syncFavoriteGamesWithState() {
    const preferenceFavorites = this.getState().preferences?.favoriteGames;
    if (Array.isArray(preferenceFavorites)) {
      this.state.favoriteGames = [...preferenceFavorites];
    }
    const ids = new Set((this.getState().games || []).map((game) => game.id));
    const filtered = this.state.favoriteGames.filter((id) => ids.has(id));
    if (filtered.length !== this.state.favoriteGames.length) {
      this.state.favoriteGames = filtered;
    }
    this.saveFavoriteGames();
  },

  getPinnedTags() {
    return this.normalizeStringList(this.getState().preferences?.pinnedTags || []);
  },

  isPinnedTag(tag) {
    return this.getPinnedTags().includes(String(tag || "").trim());
  },

  getAllTagSummaries() {
    return this.collectTagSummaries(this.getState().games || []);
  },

  rememberPendingDeletedGame(game) {
    if (!game || !this.state.snapshot) {
      return;
    }
    const games = this.getState().games || [];
    const preferences = this.getState().preferences || {};
    this.state.pendingDeletedGameIds.add(game.id);
    this.state.pendingDeletedGamesById[game.id] = {
      game: JSON.parse(JSON.stringify(game)),
      gameIndex: games.findIndex((item) => item.id === game.id),
      favoriteIndex: (preferences.favoriteGames || []).indexOf(game.id),
    };
  },

  clearPendingDeletedGame(gameId) {
    this.state.pendingDeletedGameIds.delete(gameId);
    delete this.state.pendingDeletedGamesById[gameId];
  },

  rememberPendingDeletedBackup(gameId, filename) {
    gameId = String(gameId || "").trim();
    filename = String(filename || "").trim();
    if (!gameId || !filename) {
      return;
    }
    const current = new Set(this.state.pendingDeletedBackupsByGame[gameId] || []);
    current.add(filename);
    this.state.pendingDeletedBackupsByGame = {
      ...(this.state.pendingDeletedBackupsByGame || {}),
      [gameId]: current,
    };
  },

  clearPendingDeletedBackup(gameId, filename) {
    gameId = String(gameId || "").trim();
    filename = String(filename || "").trim();
    if (!gameId || !filename) {
      return;
    }
    const current = new Set(this.state.pendingDeletedBackupsByGame[gameId] || []);
    current.delete(filename);
    const next = { ...(this.state.pendingDeletedBackupsByGame || {}) };
    if (current.size) {
      next[gameId] = current;
    } else {
      delete next[gameId];
    }
    this.state.pendingDeletedBackupsByGame = next;
  },

  isPendingDeletedBackup(gameId, filename) {
    gameId = String(gameId || "").trim();
    filename = String(filename || "").trim();
    if (!gameId || !filename) {
      return false;
    }
    return Boolean(this.state.pendingDeletedBackupsByGame?.[gameId]?.has(filename));
  },

  reconcilePendingDeletedBackupsWithState() {
    const games = new Map((this.getState().games || []).map((game) => [game.id, game]));
    const next = {};
    Object.entries(this.state.pendingDeletedBackupsByGame || {}).forEach(
      ([gameId, filenameSet]) => {
        const game = games.get(gameId);
        if (!game) {
          return;
        }
        const stillPending = new Set();
        Array.from(filenameSet || []).forEach((filename) => {
          const record = (game.backupRegistry || []).find(
            (item) => item?.filename === filename,
          );
          if (record?.status === "pending_delete") {
            stillPending.add(filename);
          }
        });
        if (stillPending.size) {
          next[gameId] = stillPending;
        }
      },
    );
    this.state.pendingDeletedBackupsByGame = next;
  },

  applyOptimisticGameDelete(gameId) {
    if (!this.state.snapshot) {
      return;
    }
    const currentState = this.getState();
    const nextGames = (currentState.games || []).filter(
      (game) => game.id !== gameId,
    );
    const nextFavorites = ((currentState.preferences || {}).favoriteGames || [])
      .filter((id) => id !== gameId);
    this.state.favoriteGames = this.state.favoriteGames.filter(
      (id) => id !== gameId,
    );
    this.saveFavoriteGames();
    this.applySnapshotSilently({
      ...this.state.snapshot,
      state: {
        ...currentState,
        games: nextGames,
        preferences: {
          ...(currentState.preferences || {}),
          favoriteGames: nextFavorites,
        },
      },
    });
    this.renderDataViews({ renderSettings: this.state.page === "settings" });
    this.markLocalSnapshotEchoCandidate(this.state.snapshot);
  },

  restorePendingDeletedGame(gameId) {
    const pending = this.state.pendingDeletedGamesById[gameId];
    if (!pending || !this.state.snapshot?.state) {
      this.clearPendingDeletedGame(gameId);
      return;
    }

    this.clearPendingDeletedGame(gameId);
    const currentState = this.getState();
    const nextGames = [...(currentState.games || [])];
    const restoredGame = JSON.parse(JSON.stringify(pending.game));
    const insertIndex =
      pending.gameIndex >= 0 && pending.gameIndex <= nextGames.length
        ? pending.gameIndex
        : nextGames.length;
    nextGames.splice(insertIndex, 0, restoredGame);

    const currentFavorites = [
      ...(((currentState.preferences || {}).favoriteGames || []).filter(Boolean)),
    ];
    if (pending.favoriteIndex >= 0) {
      const favoriteInsertIndex = Math.min(
        Math.max(pending.favoriteIndex, 0),
        currentFavorites.length,
      );
      currentFavorites.splice(favoriteInsertIndex, 0, restoredGame.id);
      this.state.favoriteGames = [...currentFavorites];
      this.saveFavoriteGames();
    }

    this.applySnapshotSilently({
      ...this.state.snapshot,
      state: {
        ...currentState,
        games: nextGames,
        preferences: {
          ...(currentState.preferences || {}),
          favoriteGames: currentFavorites,
        },
      },
    });
    this.renderDataViews({ renderSettings: this.state.page === "settings" });
  },

  sameStringSlices(left = [], right = []) {
    if (left.length !== right.length) {
      return false;
    }
    return left.every((value, index) => value === right[index]);
  },

  async persistFavoriteGames(nextFavoriteGames, messages = {}) {
    const previousFavoriteGames = [...this.state.favoriteGames];
    this.state.favoriteGames = [...nextFavoriteGames];
    this.saveFavoriteGames();
    this.renderFavoriteGames();
    this.renderTagsPage();
    if (this.state.page === "settings") {
      this.renderSettings();
    }
    this.refreshIcons();

    try {
      const snapshot = await this.bridge.SavePreferences(
        this.buildPreferencesPayload({
          favoriteGames: [...nextFavoriteGames],
        }),
      );
      this.applySnapshot(snapshot, { renderSettings: true });
      if (messages.success) {
        this.showToast(messages.success, "success");
      }
      this.updateNetworkStatus("online", messages.online || "常玩游戏已同步");
    } catch (error) {
      this.state.favoriteGames = previousFavoriteGames;
      this.saveFavoriteGames();
      this.renderFavoriteGames();
      this.renderTagsPage();
      if (this.state.page === "settings") {
        this.renderSettings();
      }
      this.refreshIcons();
      console.error(error);
      this.showToast(
        error.message || messages.error || "同步常玩游戏失败",
        "error",
      );
      this.updateNetworkStatus(
        "offline",
        messages.offline || error.message || "同步常玩游戏失败",
      );
    }
  },

  async refreshSnapshot(message = "", options = {}) {
    this.state.lastSnapshotRefreshAt = Date.now();
    try {
      this.applySnapshot(await this.bridge.Bootstrap(), {
        full: !this.state.snapshot,
        origin: "refresh",
      });
      this.setCatalogNetworkState("online", message || "已连接");
      this.updateNetworkStatus("online", message || "已连接");
    } catch (error) {
      console.error(error);
      this.setCatalogNetworkState("offline", error.message || "加载失败");
      this.updateNetworkStatus("offline", error.message || "加载失败");
      if (!options.silentError) {
        this.showToast(error.message || "加载失败", "error");
      }
    }
  },

  applySnapshot(snapshot, options = {}) {
    const sanitizedSnapshot = this.sanitizeSnapshotForPendingDeletes(snapshot);
    const hadSnapshot = Boolean(this.state.snapshot);
    const previousSnapshot = this.state.snapshot;
    const previousSignature = this.snapshotRenderSignature(previousSnapshot);
    const nextSignature = this.snapshotRenderSignature(sanitizedSnapshot);
    const sameRenderableSnapshot =
      hadSnapshot && previousSignature === nextSignature;

    if (
      sameRenderableSnapshot &&
      options.origin === "refresh" &&
      nextSignature &&
      nextSignature === this.state.lastLocalSnapshotEchoSignature &&
      Date.now() - (this.state.lastLocalSnapshotEchoAt || 0) < 60000
    ) {
      this.applySnapshotSilently(sanitizedSnapshot, {
        preserveCoverCache: true,
      });
      return false;
    }

    if (
      hadSnapshot &&
      !options.full &&
      options.origin === "refresh" &&
      this.didSnapshotOnlyChangeGamePlaytime(
        previousSnapshot,
        sanitizedSnapshot,
      )
    ) {
      const changedGameIds = this.collectPlaytimeChangedGameIds(
        previousSnapshot.state || {},
        sanitizedSnapshot.state || {},
      );
      this.applySnapshotSilently(sanitizedSnapshot, {
        preserveCoverCache: true,
      });
      changedGameIds.forEach((gameId) => this.updateGamePlaytimeDOM(gameId));
      return true;
    }

    this.coverSourceCache.clear();
    this.coverSourceInflight.clear();
    this.state.snapshot = sanitizedSnapshot;
    this.syncBackupCountsWithState();
    this.syncFavoriteGamesWithState();
    this.reconcilePendingDeletedBackupsWithState();
    if (options.full || !hadSnapshot) {
      this.render();
      if (options.origin !== "refresh") {
        this.markLocalSnapshotEchoCandidate(sanitizedSnapshot);
      }
      return true;
    }
    this.renderDataViews(options);
    if (options.origin !== "refresh") {
      this.markLocalSnapshotEchoCandidate(sanitizedSnapshot);
    }
    return true;
  },

  applySnapshotSilently(snapshot, options = {}) {
    const sanitizedSnapshot = this.sanitizeSnapshotForPendingDeletes(snapshot);
    if (!options.preserveCoverCache) {
      this.coverSourceCache.clear();
      this.coverSourceInflight.clear();
    }
    this.state.snapshot = sanitizedSnapshot;
    this.syncBackupCountsWithState();
    this.syncFavoriteGamesWithState();
    this.reconcilePendingDeletedBackupsWithState();
  },

  syncBackupCountsWithState() {
    const games = this.state.snapshot?.state?.games || [];
    const nextCounts = {};
    games.forEach((game) => {
      if (!game?.id) {
        return;
      }
      nextCounts[game.id] = Math.max(
        Number.isFinite(Number(game.backupCount)) ? Number(game.backupCount) : 0,
        0,
      );
    });
    this.state.backupCounts = nextCounts;
  },

  beginSilentRuntimeStateUpdates() {
    this.state.silentRuntimeStateUpdates =
      (this.state.silentRuntimeStateUpdates || 0) + 1;
  },

  endSilentRuntimeStateUpdates() {
    this.state.silentRuntimeStateUpdates = Math.max(
      (this.state.silentRuntimeStateUpdates || 1) - 1,
      0,
    );
    this.state.silentRuntimeStateUntil = Date.now() + 800;
  },

  shouldApplyRuntimeStateSilently() {
    return Boolean(
      this.state.savingGameForm ||
      this.state.silentRuntimeStateUpdates > 0 ||
      Date.now() < (this.state.silentRuntimeStateUntil || 0),
    );
  },

  applyStateUpdate(state, options = {}) {
    if (!this.state.snapshot) {
      return;
    }
    const previousState = this.state.snapshot.state || {};
    const nextState = this.sanitizeStateForPendingDeletes(state);
    if (
      this.stableStateSignature(previousState) ===
      this.stableStateSignature(nextState)
    ) {
      this.applySnapshotSilently({
        ...this.state.snapshot,
        state: nextState,
      });
      return;
    }
    this.applySnapshotSilently({
      ...this.state.snapshot,
      state: nextState,
    });
    this.renderStateDelta(previousState, nextState, options);
  },

  renderStateDelta(previousState, nextState, options = {}) {
    const previousGames = new Map(
      (previousState.games || []).map((game) => [game.id, game]),
    );
    const nextGames = new Map(
      (nextState.games || []).map((game) => [game.id, game]),
    );
    const previousBackupCounts = Object.fromEntries(
      (previousState.games || []).map((game) => [
        game.id,
        Math.max(Number(game.backupCount || 0), 0),
      ]),
    );
    const nextBackupCounts = Object.fromEntries(
      (nextState.games || []).map((game) => [
        game.id,
        Math.max(Number(game.backupCount || 0), 0),
      ]),
    );
    const changedGameIds = new Set();
    const playtimeOnlyChangedGameIds = new Set();
    const backupCountOnlyChangedGameIds = new Set();
    let didRender = false;

    nextGames.forEach((game, gameId) => {
      const previousGame = previousGames.get(gameId) || null;
      if ((previousBackupCounts[gameId] || 0) !== (nextBackupCounts[gameId] || 0)) {
        backupCountOnlyChangedGameIds.add(gameId);
      }
      if (this.didOnlyGamePlaytimeChange(previousGame, game)) {
        playtimeOnlyChangedGameIds.add(gameId);
        return;
      }
      if (this.gameRenderSignature(previousGame) !== this.gameRenderSignature(game)) {
        changedGameIds.add(gameId);
      }
    });
    previousGames.forEach((game, gameId) => {
      if (!nextGames.has(gameId)) {
        changedGameIds.add(gameId);
      }
    });

    changedGameIds.forEach((gameId) => {
      if (nextGames.has(gameId)) {
        this.updateSavedGameDOM(gameId);
        didRender = true;
        if (
          this.state.currentGameId === gameId &&
          this.dom.gameModal?.classList.contains("active")
        ) {
          this.renderGameDetail(gameId);
        }
        return;
      }
      document
        .querySelectorAll(
          `.game-card[data-game-id="${this.escapeCssValue(gameId)}"]`,
        )
        .forEach((card) => {
          this.scheduleAnimatedRemoval(card.parentElement, card, 220);
      });
      didRender = true;
    });

    playtimeOnlyChangedGameIds.forEach((gameId) => {
      this.updateGamePlaytimeDOM(gameId);
    });
    backupCountOnlyChangedGameIds.forEach((gameId) => {
      this.updateGameBackupCountDOM(gameId);
    });
    if (
      document.getElementById("backup-modal")?.classList.contains("active") &&
      this.state.currentGameId &&
      JSON.stringify(
        (previousGames.get(this.state.currentGameId)?.backupRegistry || []).map((item) => ({
          filename: item.filename,
          status: item.status,
          accountId: item.accountId,
          lastError: item.lastError,
        })),
      ) !==
        JSON.stringify(
          (nextGames.get(this.state.currentGameId)?.backupRegistry || []).map((item) => ({
            filename: item.filename,
            status: item.status,
            accountId: item.accountId,
            lastError: item.lastError,
          })),
        )
    ) {
      this.syncOpenBackupModal(this.state.currentGameId);
    }

    if (
      this.state.page === "activities" &&
      JSON.stringify(previousState.activities || []) !==
        JSON.stringify(nextState.activities || [])
    ) {
      this.renderActivities();
      didRender = true;
    }
    if (
      JSON.stringify(previousState.preferences?.pinnedTags || []) !==
        JSON.stringify(nextState.preferences?.pinnedTags || []) ||
      JSON.stringify(this.collectTagSummaries(previousState.games || []).map((tag) => tag.name)) !==
        JSON.stringify(this.collectTagSummaries(nextState.games || []).map((tag) => tag.name))
    ) {
      this.renderPinnedTagsNav();
      if (this.state.page === "all-tags") {
        this.renderTagsPage();
      }
      didRender = true;
    }
    if (
      this.state.page === "accounts" &&
      JSON.stringify(previousState.accounts || []) !==
        JSON.stringify(nextState.accounts || [])
    ) {
      this.renderAccounts();
      didRender = true;
    }
    if (
      (this.state.page === "settings" || options.renderSettings) &&
      (JSON.stringify(previousState.preferences || {}) !==
        JSON.stringify(nextState.preferences || {}) ||
        JSON.stringify(previousState.accounts || []) !==
          JSON.stringify(nextState.accounts || []) ||
        JSON.stringify((previousState.games || []).map((game) => this.gameRenderModel(game))) !==
          JSON.stringify((nextState.games || []).map((game) => this.gameRenderModel(game))))
    ) {
      this.renderSettings();
      didRender = true;
    }
    if (changedGameIds.size && this.state.page === "all-tags") {
      this.renderTagsPage();
      didRender = true;
    }
    if (didRender) {
      this.refreshIcons();
    }
  },

  stableStateSignature(state = {}) {
    return JSON.stringify({
      games: (state.games || []).map((game) => this.gameStateRenderModel(game)),
      accounts: (state.accounts || []).map((account) =>
        this.accountRenderModel(account),
      ),
      preferences: this.preferencesRenderModel(state.preferences || {}),
      activities: (state.activities || []).map((activity) => ({
        id: activity.id,
        gameId: activity.gameId,
        status: activity.status,
        message: activity.message,
        uploaded: activity.uploaded,
        downloaded: activity.downloaded,
        conflicts: activity.conflicts,
        startedAt: activity.startedAt,
        endedAt: activity.endedAt,
      })),
    });
  },

  markLocalSnapshotEchoCandidate(snapshot = this.state.snapshot) {
    const signature = this.snapshotRenderSignature(snapshot);
    this.state.lastLocalSnapshotEchoSignature = signature;
    this.state.lastLocalSnapshotEchoAt = Date.now();
  },

  sanitizeSnapshotForPendingDeletes(snapshot) {
    if (!snapshot) {
      return snapshot;
    }
    return {
      ...snapshot,
      state: this.sanitizeStateForPendingDeletes(snapshot.state || {}),
    };
  },

  sanitizeStateForPendingDeletes(state = {}) {
    const pending = this.state.pendingDeletedGameIds;
    if (!pending || pending.size === 0) {
      return state;
    }
    const preferences = state.preferences || {};
    return {
      ...state,
      games: (state.games || []).filter((game) => !pending.has(game.id)),
      preferences: {
        ...preferences,
        favoriteGames: (preferences.favoriteGames || []).filter(
          (gameId) => !pending.has(gameId),
        ),
      },
    };
  },

  snapshotRenderSignature(snapshot) {
    if (!snapshot) {
      return "";
    }
    return JSON.stringify({
      schemaVersion: snapshot.schemaVersion || 0,
      dataDir: snapshot.dataDir || "",
      state: {
        games: ((snapshot.state && snapshot.state.games) || []).map((game) =>
          this.gameStateRenderModel(game),
        ),
        accounts: ((snapshot.state && snapshot.state.accounts) || []).map(
          (account) => this.accountRenderModel(account),
        ),
        preferences: this.preferencesRenderModel(
          (snapshot.state && snapshot.state.preferences) || {},
        ),
        activities: ((snapshot.state && snapshot.state.activities) || []).map(
          (activity) => ({
            id: activity.id,
            gameId: activity.gameId,
            status: activity.status,
            message: activity.message,
            uploaded: activity.uploaded,
            downloaded: activity.downloaded,
            conflicts: activity.conflicts,
            startedAt: activity.startedAt,
            endedAt: activity.endedAt,
          }),
        ),
      },
    });
  },

  gameRenderSignature(game) {
    return JSON.stringify(this.gameRenderModel(game));
  },

  gameStateRenderModel(game) {
    const renderModel = this.gameRenderModel(game);
    if (!renderModel) {
      return null;
    }
    return {
      ...renderModel,
      playTime: game.playTime || 0,
      lastPlayed: game.lastPlayed || null,
    };
  },

  gameRenderModel(game) {
    if (!game) return null;
    return {
      id: game.id,
      name: game.name,
      installPath: game.installPath,
      savePath: game.savePath,
      coverPath: game.coverPath,
      coverSourceType: game.coverSourceType || "",
      coverSource: game.coverSource || "",
      coverLocalPath: game.coverLocalPath || "",
      coverCloudAccountId: game.coverCloudAccountId || "",
      coverCloudKey: game.coverCloudKey || "",
      coverMimeType: game.coverMimeType || "",
      coverUpdatedAt: game.coverUpdatedAt || null,
      description: game.description,
      released: game.released,
      rating: game.rating,
      ratingTop: game.ratingTop,
      metacritic: game.metacritic,
      genres: game.genres || [],
      platforms: game.platforms || [],
      isSteam: Boolean(game.isSteam),
      developers: game.developers || [],
      publishers: game.publishers || [],
      website: game.website,
      rawgId: game.rawgId,
      rawgSlug: game.rawgSlug,
      rawgUrl: game.rawgUrl,
      rawgTags: game.rawgTags || [],
      tags: game.tags || [],
      storageAccountId: game.storageAccountId,
      sync: game.sync || {},
      lastSync: game.lastSync || null,
    };
  },

  didOnlyGamePlaytimeChange(previousGame, nextGame) {
    if (!previousGame || !nextGame) {
      return false;
    }
    if (this.gameRenderSignature(previousGame) !== this.gameRenderSignature(nextGame)) {
      return false;
    }
    return (
      (previousGame.playTime || 0) !== (nextGame.playTime || 0) ||
      (previousGame.lastPlayed || null) !== (nextGame.lastPlayed || null)
    );
  },

  collectPlaytimeChangedGameIds(previousState = {}, nextState = {}) {
    const previousGames = new Map(
      (previousState.games || []).map((game) => [game.id, game]),
    );
    const changedGameIds = [];
    (nextState.games || []).forEach((game) => {
      if (this.didOnlyGamePlaytimeChange(previousGames.get(game.id), game)) {
        changedGameIds.push(game.id);
      }
    });
    return changedGameIds;
  },

  didSnapshotOnlyChangeGamePlaytime(previousSnapshot, nextSnapshot) {
    if (!previousSnapshot || !nextSnapshot) {
      return false;
    }
    if (
      (previousSnapshot.schemaVersion || 0) !== (nextSnapshot.schemaVersion || 0) ||
      (previousSnapshot.dataDir || "") !== (nextSnapshot.dataDir || "")
    ) {
      return false;
    }

    const previousState = previousSnapshot.state || {};
    const nextState = nextSnapshot.state || {};

    if (
      JSON.stringify((previousState.accounts || []).map((account) => this.accountRenderModel(account))) !==
        JSON.stringify((nextState.accounts || []).map((account) => this.accountRenderModel(account))) ||
      JSON.stringify(this.preferencesRenderModel(previousState.preferences || {})) !==
        JSON.stringify(this.preferencesRenderModel(nextState.preferences || {})) ||
      JSON.stringify(previousState.activities || []) !==
        JSON.stringify(nextState.activities || [])
    ) {
      return false;
    }

    if ((previousState.games || []).length !== (nextState.games || []).length) {
      return false;
    }

    const previousGames = new Map(
      (previousState.games || []).map((game) => [game.id, game]),
    );
    let hasPlaytimeChange = false;

    for (const game of nextState.games || []) {
      const previousGame = previousGames.get(game.id);
      if (!previousGame) {
        return false;
      }
      if (this.didOnlyGamePlaytimeChange(previousGame, game)) {
        hasPlaytimeChange = true;
        continue;
      }
      if (
        JSON.stringify(this.gameStateRenderModel(previousGame)) !==
        JSON.stringify(this.gameStateRenderModel(game))
      ) {
        return false;
      }
    }

    return hasPlaytimeChange;
  },

  updateGamePlaytimeDOM(gameId) {
    const game = (this.getState().games || []).find((item) => item.id === gameId);
    if (!game) {
      return;
    }

    const lastPlayedText = game.lastPlayed
      ? this.formatRelativeTime(game.lastPlayed)
      : "未游玩";
    const lastPlayedTitle = `上次游玩：${game.lastPlayed ? this.formatTime(game.lastPlayed) : "从未"}`;

    document
      .querySelectorAll(
        `.game-card[data-game-id="${this.escapeCssValue(gameId)}"] .game-card-overlay-last-played`,
      )
      .forEach((element) => {
        element.textContent = lastPlayedText;
        element.title = lastPlayedTitle;
      });

    if (
      this.dom.gameModal?.classList.contains("active") &&
      this.state.currentGameId === gameId
    ) {
      this.updateGameModalHeaderStats(game);
    }
  },

  updateGameBackupCountDOM(gameId) {
    const game = (this.getState().games || []).find((item) => item.id === gameId);
    const count = this.getGameBackupCount(game || { id: gameId });
    document
      .querySelectorAll(
        `.game-card[data-game-id="${this.escapeCssValue(gameId)}"] .game-card-overlay-count span`,
      )
      .forEach((element) => {
        element.textContent = String(count);
      });
  },

  accountRenderModel(account) {
    return {
      id: account.id,
      name: account.name,
      accountId: account.accountId,
      d1DatabaseId: account.d1DatabaseId,
      r2Bucket: account.r2Bucket,
      isPrimary: Boolean(account.isPrimary),
      enabled: Boolean(account.enabled),
      usedBytes: account.usedBytes || 0,
      lastVerifiedAt: account.lastVerifiedAt || null,
      tokenExpiresAt: account.tokenExpiresAt || null,
      lastError: account.lastError || "",
      usageWarning: account.usageWarning || "",
      verificationState: account.verificationState || "",
      credentialsBackedUp: Boolean(account.credentialsBackedUp),
    };
  },

  preferencesRenderModel(preferences) {
    return {
      autoSyncOnLaunch: Boolean(preferences.autoSyncOnLaunch),
      startupSyncMode: preferences.startupSyncMode || "",
      conflictPolicy: preferences.conflictPolicy || "",
      defaultInstallDir: preferences.defaultInstallDir || "",
      defaultSaveDir: preferences.defaultSaveDir || "",
      defaultSteamInstallDir: preferences.defaultSteamInstallDir || "",
      defaultSteamSaveDir: preferences.defaultSteamSaveDir || "",
      defaultThirdInstallDir: preferences.defaultThirdInstallDir || "",
      defaultThirdSaveDir: preferences.defaultThirdSaveDir || "",
      rawgApiKey: preferences.rawgApiKey || "",
      steamGridDbApiKey: preferences.steamGridDbApiKey || "",
      favoriteGames: preferences.favoriteGames || [],
      tagOrder: preferences.tagOrder || [],
      pinnedTags: preferences.pinnedTags || [],
    };
  },

  renderDataViews(options = {}) {
    this.renderPageState();
    this.renderPinnedTagsNav();
    this.renderGames();
    this.renderFavoriteGames();
    this.renderTagsPage();
    this.renderAccounts();
    this.renderActivities();
    if (this.state.page === "settings" || options.renderSettings) {
      this.renderSettings();
    }
    if (this.dom.gameModal?.classList.contains("active")) {
      const currentGame = (this.getState().games || []).find(
        (game) => game.id === this.state.currentGameId,
      );
      this.updateGameModalHeaderStats(currentGame || null);
    }
    if (
      this.state.currentGameId &&
      this.dom.gameModal?.classList.contains("active")
    ) {
      this.renderGameDetail(this.state.currentGameId);
    }
    this.refreshIcons();
  },

  render() {
    this.syncFavoriteGamesWithState();
    this.renderPageState();
    this.renderPinnedTagsNav();
    this.renderGames();
    this.renderFavoriteGames();
    this.renderTagsPage();
    this.renderAccounts();
    this.renderActivities();
    this.renderSettings();
    if (this.dom.gameModal?.classList.contains("active")) {
      const currentGame = (this.getState().games || []).find(
        (game) => game.id === this.state.currentGameId,
      );
      this.updateGameModalHeaderStats(currentGame || null);
    }
    if (
      this.state.currentGameId &&
      this.dom.gameModal?.classList.contains("active")
    ) {
      this.renderGameDetail(this.state.currentGameId);
    }
    this.refreshIcons();
  },

  refreshIcons() {
    createIcons({ icons: LUCIDE_ICONS, attrs: ICON_ATTRS });
    this.hydrateCoverImages();
  },

  setButtonBusy(button, busy, busyText = "处理中...") {
    if (!button) {
      return;
    }

    if (busy) {
      if (!button.dataset.idleText) {
        button.dataset.idleText = button.textContent;
      }
      button.textContent = busyText;
      button.disabled = true;
      button.classList.add("is-loading");
      button.setAttribute("aria-busy", "true");
      return;
    }

    button.textContent = button.dataset.idleText || button.textContent;
    delete button.dataset.idleText;
    button.disabled = false;
    button.classList.remove("is-loading");
    button.removeAttribute("aria-busy");
  },

  icon(name, className = "") {
    const classAttr = className
      ? ` class="${this.escapeHtmlAttribute(className)}"`
      : "";
    return `<span data-lucide="${this.escapeHtmlAttribute(name)}"${classAttr}></span>`;
  },

  renderCoverPlaceholder(className = "game-card-cover-placeholder") {
    return `<div class="${this.escapeHtmlAttribute(className)}">${this.icon("image", "cover-placeholder-icon")}</div>`;
  },

  getCurrentPageMeta() {
    if (this.state.page === "all-games" && this.state.filterTag) {
      return {
        title: this.state.filterTag,
        searchPlaceholder: `搜索 ${this.state.filterTag} 标签下的游戏...`,
        primaryText: "添加游戏",
        secondaryText: "返回全部游戏",
      };
    }
    return PAGE_META[this.state.page];
  },

  renderPageState() {
    const meta = this.getCurrentPageMeta();
    this.dom.pageTitle.textContent = meta.title;
    this.dom.searchInput.placeholder = meta.searchPlaceholder;
    this.dom.topbarPrimaryBtn.textContent = meta.primaryText;
    this.dom.topbarSecondaryBtn.textContent = meta.secondaryText;
    this.dom.topbarPrimaryBtn.style.display = meta.primaryText
      ? "inline-flex"
      : "none";
    this.dom.topbarSecondaryBtn.style.display = meta.secondaryText
      ? "inline-flex"
      : "none";

    document.querySelectorAll(".nav-btn").forEach((button) => {
      button.classList.toggle(
        "active",
        button.dataset.page === this.state.page,
      );
    });
    document.querySelectorAll(".pinned-tag-nav-btn").forEach((button) => {
      button.classList.toggle(
        "active",
        this.state.page === "all-games" &&
          this.state.filterTag === button.dataset.tag,
      );
    });

    Object.entries(this.dom.pages).forEach(([page, element]) => {
      element.style.display = page === this.state.page ? "block" : "none";
    });
  },

  renderPinnedTagsNav() {
    if (!this.dom.pinnedTagsNav) {
      return;
    }
    const availableTags = new Set(this.getAllTagSummaries().map((tag) => tag.name));
    const pinnedTags = this.getPinnedTags().filter((tag) => availableTags.has(tag));
    if (!pinnedTags.length) {
      this.dom.pinnedTagsNav.innerHTML = "";
      this.dom.pinnedTagsNav.style.display = "none";
      return;
    }
    this.dom.pinnedTagsNav.style.display = "";
    this.dom.pinnedTagsNav.innerHTML = pinnedTags
      .map(
        (tag) => `
          <button
            type="button"
            class="icon-nav-btn pinned-tag-nav-btn${this.state.page === "all-games" && this.state.filterTag === tag ? " active" : ""}"
            data-tag="${this.escapeHtmlAttribute(tag)}"
            title="${this.escapeHtmlAttribute(tag)}"
          >
            <span class="icon-nav-icon" data-lucide="pin"></span>
            <span class="icon-nav-text">${this.escapeHtml(tag)}</span>
          </button>
        `,
      )
      .join("");
  },

  renderGames() {
    const { games = [], accounts = [] } = this.getState();
    const visibleGames = this.filterGames(
      games.filter((game) => {
        if (!this.state.filterTag) return true;
        if (this.state.filterTag === "Steam 游戏" && game.isSteam) return true;
        if (
          this.state.filterTag === "第三方游戏" &&
          (!game.isSteam || game.isSteam === false)
        )
          return true;
        return (game.tags || []).includes(this.state.filterTag);
      }),
    );
    const syncEnabledCount = games.filter((game) => game.sync?.enabled).length;
    const conflictCount = games.filter(
      (game) => game.lastSync?.status === "conflict",
    ).length;
    const lastSuccess = games
      .map((game) => game.lastSync?.syncedAt)
      .filter(Boolean)
      .sort()
      .at(-1);

    const showSummary = this.state.page === "activities";
    this.dom.syncSummaryCard.style.display = showSummary ? "block" : "none";
    if (showSummary) {
      this.dom.syncSummaryCard.innerHTML = `
        <div class="sync-summary-head">
          <div>
            <div class="account-card-title">同步总览</div>
            <div class="sync-summary-meta">这里汇总当前 Wails 状态中的同步数据。</div>
          </div>
          <span class="badge ${conflictCount ? "warning" : "success"}">${conflictCount ? `${conflictCount} 个冲突待处理` : "同步状态正常"}</span>
        </div>
        <div class="sync-summary-grid">
          <div class="sync-stat"><div class="sync-stat-label">游戏总数</div><div class="sync-stat-value">${games.length}</div></div>
          <div class="sync-stat"><div class="sync-stat-label">已启用同步</div><div class="sync-stat-value">${syncEnabledCount}</div></div>
          <div class="sync-stat"><div class="sync-stat-label">常玩游戏</div><div class="sync-stat-value">${this.state.favoriteGames.length}</div></div>
          <div class="sync-stat"><div class="sync-stat-label">最近同步</div><div class="sync-stat-value" style="font-size:14px; font-weight:600;">${lastSuccess ? this.formatTime(lastSuccess) : "从未"}</div></div>
        </div>
      `;
    }

    if (!visibleGames.length) {
      this.dom.gamesGrid.innerHTML = "";
      this.dom.gamesEmpty.innerHTML = this.state.searchQuery
        ? `${this.icon("search", "empty-state-icon")}<div class="empty-state-text">未找到匹配的游戏</div><div class="section-desc">试试别的关键词，或先回到全部游戏再搜索。</div>`
        : this.state.filterTag
          ? `${this.icon("tags", "empty-state-icon")}<div class="empty-state-text">标签“${this.escapeHtml(this.state.filterTag)}”下还没有游戏</div><div class="section-desc">你可以给已有游戏补上这个标签，或先添加新游戏。</div>`
          : `${this.icon("gamepad-2", "empty-state-icon")}<div class="empty-state-text">还没有添加任何游戏</div><button class="btn btn-primary" id="empty-add-game-btn">添加第一个游戏</button>`;
      this.dom.gamesEmpty.style.display = "flex";

      const emptyAddButton = document.getElementById("empty-add-game-btn");
      if (emptyAddButton) {
        emptyAddButton.addEventListener("click", () => this.openGameModal());
      }
      return;
    }

    this.dom.gamesEmpty.style.display = "none";
    this.reconcileCards(
      this.dom.gamesGrid,
      visibleGames,
      (game) => this.renderGameCard(game, accounts),
      (game) => game.id,
    );
    this.ensureBackupCounts(visibleGames);
  },

  renderFavoriteGames() {
    const { games = [], accounts = [] } = this.getState();
    const favoriteSet = new Set(this.state.favoriteGames);
    const visibleGames = this.filterGames(
      games.filter((game) => favoriteSet.has(game.id)),
    ).sort(
      (a, b) =>
        this.state.favoriteGames.indexOf(a.id) -
        this.state.favoriteGames.indexOf(b.id),
    );

    if (!visibleGames.length) {
      this.dom.favoriteGamesGrid.innerHTML = "";
      this.dom.favoriteGamesEmpty.innerHTML = this.state.searchQuery
        ? `${this.icon("search", "empty-state-icon")}<div class="empty-state-text">未找到匹配的常玩游戏</div><div class="section-desc">可以切回全部游戏后换个关键词再试。</div>`
        : `${this.icon("heart", "empty-state-icon")}<div class="empty-state-text">还没有添加常玩游戏</div><div class="section-desc">点一下游戏卡片右上角的心形按钮，它就会出现在这里。</div>`;
      this.dom.favoriteGamesEmpty.style.display = "flex";
      return;
    }

    this.dom.favoriteGamesEmpty.style.display = "none";
    this.reconcileCards(
      this.dom.favoriteGamesGrid,
      visibleGames,
      (game) => this.renderGameCard(game, accounts),
      (game) => game.id,
    );
    this.ensureBackupCounts(visibleGames);
  },

  renderTagsPage() {
    const summaries = this.filterTags(
      this.collectTagSummaries(this.getState().games || []),
    );
    if (!summaries.length) {
      this.dom.tagsGrid.innerHTML = "";
      this.dom.tagsEmpty.innerHTML = this.state.searchQuery
        ? `${this.icon("search", "empty-state-icon")}<div class="empty-state-text">未找到匹配的标签</div>`
        : `${this.icon("tags", "empty-state-icon")}<div class="empty-state-text">还没有可用标签</div><div class="section-desc">先给游戏写几个标签，这里就会出现标签入口。</div>`;
      this.dom.tagsEmpty.style.display = "flex";
      return;
    }

    this.dom.tagsEmpty.style.display = "none";
    this.reconcileCards(
      this.dom.tagsGrid,
      summaries,
      (summary) => this.renderTagCard(summary),
      (summary) => summary.name,
    );
  },

  renderAccounts() {
    const { accounts = [] } = this.getState();
    const filteredAccounts = this.filterAccounts(accounts);

    if (!filteredAccounts.length) {
      this.dom.accountsGrid.innerHTML = "";
      this.dom.accountsEmpty.innerHTML = this.state.searchQuery
        ? `${this.icon("search", "empty-state-icon")}<div class="empty-state-text">未找到匹配的账号</div>`
        : `${this.icon("cloud", "empty-state-icon")}<div class="empty-state-text">还没有配置 Cloudflare 账号</div><div class="section-desc">先添加一个账号，游戏才能分配到 D1 / R2 存储空间。</div>`;
      this.dom.accountsEmpty.style.display = "flex";
      return;
    }

    this.dom.accountsEmpty.style.display = "none";
    this.reconcileCards(
      this.dom.accountsGrid,
      filteredAccounts,
      (account) => this.renderAccountCard(account),
      (account) => account.id,
    );
  },

  renderActivities() {
    const { activities = [] } = this.getState();
    const filteredActivities = this.filterActivities(activities);

    if (!filteredActivities.length) {
      this.dom.activityList.innerHTML = "";
      this.dom.activityEmpty.innerHTML = this.state.searchQuery
        ? `${this.icon("search", "empty-state-icon")}<div class="empty-state-text">没有匹配的活动记录</div>`
        : `${this.icon("history", "empty-state-icon")}<div class="empty-state-text">还没有同步活动</div><div class="section-desc">执行一次同步后，这里会显示最近的上传、下载与冲突信息。</div>`;
      this.dom.activityEmpty.style.display = "flex";
      return;
    }

    this.dom.activityEmpty.style.display = "none";
    this.reconcileCards(
      this.dom.activityList,
      filteredActivities,
      (activity) => this.renderActivityCard(activity),
      (activity) => activity.id,
    );
  },

  renderSettings() {
    const snapshot = this.state.snapshot;
    if (!snapshot) {
      return;
    }
    if (
      !this.dom.localSettingsList ||
      !this.dom.deviceInfoList ||
      !this.dom.architectureInfo ||
      !this.dom.syncOverviewList ||
      !this.dom.appUpdateInfo
    ) {
      return;
    }

    const { device, preferences, accounts, games, activities } = snapshot.state;
    const exampleInstall =
      games.map((game) => game.installPath).find(Boolean) || "";
    const exampleSave = games.map((game) => game.savePath).find(Boolean) || "";
    const lastSuccess = games
      .map((game) => game.lastSync?.syncedAt)
      .filter(Boolean)
      .sort()
      .at(-1);

    this.dom.localSettingsList.innerHTML = this.renderLocalSettings(
      snapshot.dataDir,
      preferences,
      exampleInstall,
      exampleSave,
    );

    this.dom.deviceInfoList.innerHTML = this.renderInfoRows([
      ["设备 ID", device.id],
      ["设备名称", device.name],
      ["平台", device.platform],
      ["最近启动", this.formatTime(device.lastStartedAt)],
    ]);
    this.renderUpdateSettings();

    document.getElementById("pref-auto-sync").checked = Boolean(
      preferences.autoSyncOnLaunch,
    );
    document.getElementById("pref-startup-mode").value =
      preferences.startupSyncMode || "smart";
    document.getElementById("pref-conflict-policy").value =
      preferences.conflictPolicy || "manual";
    this.syncCustomSelect("pref-startup-mode");
    this.syncCustomSelect("pref-conflict-policy");
    if (this.dom.prefRawgApiKey) {
      this.dom.prefRawgApiKey.value = preferences.rawgApiKey || "";
    }
    if (this.dom.prefSgdbApiKey) {
      this.dom.prefSgdbApiKey.value = preferences.steamGridDbApiKey || "";
    }

    this.dom.architectureInfo.innerHTML = this.renderInfoRows([
      ["元数据", "主账号 D1 保存游戏目录、版本与修订记录"],
      ["文件对象", "R2 按游戏绑定账号保存实际存档文件"],
      ["账号扩容", `当前已配置 ${accounts.length} 个账号，可按游戏分配存储池`],
      [
        "同步模型",
        `${games.filter((game) => game.sync?.enabled).length} 个游戏启用同步`,
      ],
    ]);

    this.dom.syncOverviewList.innerHTML = this.renderInfoRows([
      [
        "已启用同步",
        `${games.filter((game) => game.sync?.enabled).length} / ${games.length}`,
      ],
      [
        "待处理冲突",
        `${games.filter((game) => game.lastSync?.status === "conflict").length} 个`,
      ],
      ["最近同步", lastSuccess ? this.formatTime(lastSuccess) : "从未"],
      ["活动记录", `${activities.length} 条`],
    ]);
  },

  async refreshAppInfo() {
    try {
      this.state.appInfo = await this.bridge.GetAppInfo();
      if (this.state.page === "settings") {
        this.renderUpdateSettings();
      }
    } catch (error) {
      console.warn("读取版本信息失败:", error);
    }
  },

  renderUpdateSettings() {
    if (!this.dom.appUpdateInfo) {
      return;
    }
    const appInfo = this.state.appInfo || {};
    const updateCheck = this.state.updateCheck;
    const updateDownload = this.state.updateDownload;
    this.dom.appUpdateInfo.innerHTML = this.renderInfoRows([
      ["当前版本", appInfo.version || "0.1.0"],
      ["更新通道", appInfo.updateChannel || "stable"],
      ["平台", appInfo.platform || "windows-amd64"],
      ["构建提交", appInfo.commit || "dev"],
    ]);
    if (this.dom.appUpdateStatus) {
      this.dom.appUpdateStatus.textContent = this.updateStatusText(
        updateCheck,
        updateDownload,
      );
    }
    if (this.dom.checkUpdateBtn) {
      this.dom.checkUpdateBtn.disabled = Boolean(this.state.updating);
    }
    if (this.dom.applyUpdateBtn) {
      const canApply =
        Boolean(updateDownload?.archivePath) ||
        this.state.updateCheck?.status === "available";
      this.dom.applyUpdateBtn.style.display = canApply ? "inline-flex" : "none";
      this.dom.applyUpdateBtn.disabled = Boolean(this.state.updating);
    }
    this.refreshIcons();
  },

  updateStatusText(updateCheck, updateDownload) {
    if (this.state.updating) {
      return "正在处理更新，请稍候...";
    }
    if (updateDownload?.archivePath) {
      return `新版本 ${updateDownload.version} 已下载，点击更新并重启完成安装。`;
    }
    if (!updateCheck) {
      return "当前未检查更新。";
    }
    if (updateCheck.status === "available") {
      return `发现新版本 ${updateCheck.latestVersion}。${updateCheck.notes || ""}`.trim();
    }
    return updateCheck.message || "当前已是最新版本。";
  },

  async checkForUpdates() {
    this.state.updating = true;
    this.state.updateDownload = null;
    this.renderUpdateSettings();
    try {
      const result = await this.bridge.CheckForUpdates();
      this.state.updateCheck = result;
      if (result.status === "available") {
        this.showToast(`发现新版本 ${result.latestVersion}`, "success");
      } else if (result.status === "blocked") {
        this.showToast(result.message || "需要手动更新", "warning");
      } else {
        this.showToast(result.message || "当前已是最新版本", "success");
      }
    } catch (error) {
      console.error(error);
      this.showToast(error.message || "检查更新失败", "error");
      this.state.updateCheck = {
        status: "failed",
        message: error.message || "检查更新失败",
      };
    } finally {
      this.state.updating = false;
      this.renderUpdateSettings();
    }
  },

  async applyAvailableUpdate() {
    const check = this.state.updateCheck;
    if (!check || check.status !== "available") {
      return;
    }
    this.state.updating = true;
    this.renderUpdateSettings();
    try {
      const download = await this.bridge.DownloadUpdate({
        version: check.latestVersion,
        asset: check.asset,
      });
      this.state.updateDownload = download;
      this.renderUpdateSettings();
      await this.bridge.ApplyUpdateAndRestart(download);
      this.showToast("更新已开始，程序即将重启", "success");
    } catch (error) {
      console.error(error);
      this.showToast(error.message || "更新失败", "error");
      this.state.updateDownload = null;
    } finally {
      this.state.updating = false;
      this.renderUpdateSettings();
    }
  },

  renderInfoRows(rows) {
    return rows
      .map(
        ([label, value]) => `
      <div class="info-row">
        <div class="settings-item-label">${this.escapeHtml(label)}</div>
        <div class="info-row-value">${this.escapeHtml(value || "未设置")}</div>
      </div>
    `,
      )
      .join("");
  },

  renderLocalSettings(dataDir, preferences, exampleInstall, exampleSave) {
    const defaultSteamInstallDir =
      preferences.defaultSteamInstallDir || preferences.defaultInstallDir || "";
    const defaultSteamSaveDir =
      preferences.defaultSteamSaveDir || preferences.defaultSaveDir || "";
    const defaultThirdInstallDir =
      preferences.defaultThirdInstallDir || preferences.defaultInstallDir || "";
    const defaultThirdSaveDir =
      preferences.defaultThirdSaveDir || preferences.defaultSaveDir || "";
    return `
      <div class="info-row">
        <div class="settings-item-label">本地数据目录</div>
        <div class="info-row-value">${this.escapeHtml(dataDir || "未设置")}</div>
      </div>
      <div class="info-row">
        <div class="settings-item-label">常玩游戏</div>
        <div class="info-row-value">${this.escapeHtml(`${this.state.favoriteGames.length} 个`)}</div>
      </div>
      <div class="info-row">
        <div class="settings-item-label">Steam 游戏路径</div>
        <div class="settings-path-actions">
          <div class="info-row-value" title="${this.escapeHtmlAttribute(defaultSteamInstallDir || exampleInstall || "未设置")}">${this.escapeHtml(defaultSteamInstallDir || "未设置")}</div>
          <button type="button" class="btn btn-secondary btn-sm" data-action="pick-default-steam-install-dir">选择</button>
        </div>
      </div>
      <div class="info-row">
        <div class="settings-item-label">Steam 存档路径</div>
        <div class="settings-path-actions">
          <div class="info-row-value" title="${this.escapeHtmlAttribute(defaultSteamSaveDir || exampleSave || "未设置")}">${this.escapeHtml(defaultSteamSaveDir || "未设置")}</div>
          <button type="button" class="btn btn-secondary btn-sm" data-action="pick-default-steam-save-dir">选择</button>
        </div>
      </div>
      <div class="info-row">
        <div class="settings-item-label">第三方游戏路径</div>
        <div class="settings-path-actions">
          <div class="info-row-value" title="${this.escapeHtmlAttribute(defaultThirdInstallDir || exampleInstall || "未设置")}">${this.escapeHtml(defaultThirdInstallDir || "未设置")}</div>
          <button type="button" class="btn btn-secondary btn-sm" data-action="pick-default-third-install-dir">选择</button>
        </div>
      </div>
      <div class="info-row">
        <div class="settings-item-label">第三方存档路径</div>
        <div class="settings-path-actions">
          <div class="info-row-value" title="${this.escapeHtmlAttribute(defaultThirdSaveDir || exampleSave || "未设置")}">${this.escapeHtml(defaultThirdSaveDir || "未设置")}</div>
          <button type="button" class="btn btn-secondary btn-sm" data-action="pick-default-third-save-dir">选择</button>
        </div>
      </div>
    `;
  },

  updateGameCardStatusDOM(gameId) {
    const card = document.querySelector(`.game-card[data-game-id="${gameId}"]`);
    if (!card) return;
    const statusContainer = card.querySelector(".game-card-overlay-status");
    if (!statusContainer) return;

    const game = this.getState().games.find((g) => g.id === gameId);
    if (!game) return;

    const countEl = card.querySelector(".game-card-overlay-info span");
    if (countEl) {
      countEl.textContent = String(this.getGameBackupCount(game));
    }

    const lastSync = game.lastSync || null;
    let statusText = this.syncStatusText(lastSync?.status, game.sync?.enabled);
    let badgeClass = "";
    let badgeIcon = "";

    if (this.state.runtimeStatus && this.state.runtimeStatus[gameId]) {
      const st = this.state.runtimeStatus[gameId];
      statusText = st.text;
      badgeIcon = st.icon || "refresh-cw";
      badgeClass = "is-visible " + (st.statusClass || "is-syncing");
    }

    statusContainer.className = `game-card-overlay-status ${badgeClass}`;

    if (badgeClass) {
      statusContainer.innerHTML = `
        <span>${this.escapeHtml(statusText)}</span>
        <span style="display:flex; align-items:center; width:12px; height:12px;">${this.icon(badgeIcon)}</span>
      `;
    } else {
      statusContainer.innerHTML = `<span>${this.escapeHtml(statusText)}</span>`;
    }

    this.refreshIcons();
  },

  getGameBackupCount(game) {
    if (!game?.id) {
      return 0;
    }
    if (Number.isFinite(Number(game.backupCount))) {
      return Math.max(Number(game.backupCount), 0);
    }
    return Number(this.state.backupCounts?.[game.id] || 0);
  },

  setGameBackupCount(gameId, count) {
    if (!gameId) {
      return;
    }
    this.state.backupCounts = {
      ...(this.state.backupCounts || {}),
      [gameId]: Math.max(Number(count || 0), 0),
    };
    document
      .querySelectorAll(
        `.game-card[data-game-id="${this.escapeCssValue(gameId)}"] .game-card-overlay-info span`,
      )
      .forEach((element) => {
        element.textContent = String(this.state.backupCounts[gameId]);
      });
    if (this.state.snapshot?.state?.games) {
      this.state.snapshot.state.games = this.state.snapshot.state.games.map((game) =>
        game.id === gameId
          ? { ...game, backupCount: this.state.backupCounts[gameId] }
          : game,
      );
    }
  },

  hasActiveModal() {
    return Boolean(document.querySelector(".modal-overlay.active"));
  },

  ensureBackupCounts(_games = []) {},

  createElementFromHTML(html) {
    const template = document.createElement("template");
    template.innerHTML = html.trim();
    return template.content.firstElementChild;
  },

  renderKeySelector(key) {
    if (window.CSS && typeof window.CSS.escape === "function") {
      return `[data-render-key="${window.CSS.escape(String(key))}"]`;
    }
    return `[data-render-key="${String(key).replace(/"/g, '\\"')}"]`;
  },

  escapeCssValue(value) {
    if (window.CSS && typeof window.CSS.escape === "function") {
      return window.CSS.escape(String(value));
    }
    return String(value).replace(/["\\]/g, "\\$&");
  },

  captureFlowPositions(container) {
    const positions = new Map();
    if (!container) {
      return positions;
    }
    Array.from(container.querySelectorAll("[data-render-key]"))
      .filter((element) => !element.classList.contains("is-removing"))
      .forEach((element) => {
        positions.set(
          element.dataset.renderKey || "",
          element.getBoundingClientRect(),
        );
      });
    return positions;
  },

  playFlowFlip(container, previousPositions, duration = 220) {
    if (!container || !previousPositions?.size) {
      return;
    }
    const elements = Array.from(container.querySelectorAll("[data-render-key]"))
      .filter((element) => !element.classList.contains("is-removing"));

    elements.forEach((element) => {
      const key = element.dataset.renderKey || "";
      const previous = previousPositions.get(key);
      if (!previous) {
        return;
      }
      const next = element.getBoundingClientRect();
      const dx = previous.left - next.left;
      const dy = previous.top - next.top;
      if (!dx && !dy) {
        return;
      }
      element.style.transition = "none";
      element.style.willChange = "transform";
      element.style.transform = `translate3d(${dx}px, ${dy}px, 0)`;
    });

    window.requestAnimationFrame(() => {
      elements.forEach((element) => {
        if (!element.style.transform) {
          return;
        }
        element.style.transition = `transform ${duration}ms cubic-bezier(0.22, 1, 0.36, 1)`;
        element.style.transform = "";
      });
      window.setTimeout(() => {
        elements.forEach((element) => {
          element.style.transition = "";
          element.style.transform = "";
          element.style.willChange = "";
        });
      }, duration);
    });
  },

  scheduleAnimatedRemoval(container, element, duration = 220) {
    if (!element || element.classList.contains("is-removing")) {
      return;
    }
    element.classList.add("is-removing");
    window.setTimeout(() => {
      if (!element.isConnected) {
        return;
      }
      const positions = this.captureFlowPositions(container);
      element.remove();
      this.playFlowFlip(container, positions, duration);
    }, duration);
  },

  hashString(value) {
    let hash = 0;
    for (let index = 0; index < value.length; index++) {
      hash = (hash << 5) - hash + value.charCodeAt(index);
      hash |= 0;
    }
    return String(hash);
  },

  reconcileCards(container, items, renderItem, getKey, options = {}) {
    if (!container) {
      return;
    }

    const animate = options.animate !== false;
    const activeChildren = () =>
      Array.from(container.querySelectorAll("[data-render-key]")).filter(
        (element) => !element.classList.contains("is-removing"),
      );
    const nextKeys = new Set(items.map((item) => String(getKey(item))));

    activeChildren().forEach((element) => {
      if (!nextKeys.has(element.dataset.renderKey || "")) {
        if (animate) {
          this.scheduleAnimatedRemoval(container, element, 220);
        } else {
          element.remove();
        }
      }
    });

    items.forEach((item, index) => {
      const key = String(getKey(item));
      const html = renderItem(item);
      const nextElement = this.createElementFromHTML(html);
      if (!nextElement) {
        return;
      }
      const signature = this.hashString(html);
      nextElement.dataset.renderKey = key;
      nextElement.dataset.renderSignature = signature;

      const existing = container.querySelector(this.renderKeySelector(key));
      const reference = activeChildren()[index] || null;
      if (!existing) {
        if (animate) {
          nextElement.classList.add("is-entering");
          window.setTimeout(
            () => nextElement.classList.remove("is-entering"),
            280,
          );
        }
        container.insertBefore(nextElement, reference);
        return;
      }

      let current = existing;
      if (existing.dataset.renderSignature !== signature) {
        if (animate) {
          nextElement.classList.add("is-updating");
          window.setTimeout(
            () => nextElement.classList.remove("is-updating"),
            360,
          );
        }
        existing.replaceWith(nextElement);
        current = nextElement;
      }

      const orderedReference = activeChildren()[index] || null;
      if (orderedReference && orderedReference !== current) {
        container.insertBefore(current, orderedReference);
      } else if (!orderedReference) {
        container.appendChild(current);
      }
    });
  },

  updateGameCardDOM(gameId) {
    const { games = [], accounts = [] } = this.getState();
    const game = games.find((item) => item.id === gameId);
    if (!game) return;

    document
      .querySelectorAll(`.game-card[data-game-id="${gameId}"]`)
      .forEach((card) => {
        const html = this.renderGameCard(game, accounts);
        const nextCard = this.createElementFromHTML(html);
        if (nextCard) {
          nextCard.dataset.renderKey = game.id;
          nextCard.dataset.renderSignature = this.hashString(html);
          this.morphGameCard(card, nextCard, nextCard.dataset.renderSignature);
        }
      });
    this.refreshIcons();
  },

  morphGameCard(existing, nextCard, signature) {
    if (!existing || !nextCard) {
      return;
    }
    if (existing.dataset.renderSignature === signature) {
      return;
    }

    existing.className = nextCard.className;
    existing.title = nextCard.title;
    existing.dataset.renderSignature = signature;
    existing.dataset.renderKey =
      nextCard.dataset.renderKey || existing.dataset.renderKey;

    const currentCover = existing.querySelector(".game-card-cover");
    const nextCover = nextCard.querySelector(".game-card-cover");
    if (!currentCover || !nextCover) {
      existing.replaceChildren(...Array.from(nextCard.childNodes));
      return;
    }

    const currentImg = currentCover.querySelector("img");
    const nextImg = nextCover.querySelector("img");
    const currentSrc = currentImg?.getAttribute("src") || "";
    const nextSrc = nextImg?.getAttribute("src") || "";
    const currentPath = currentImg?.getAttribute("data-cover-path") || "";
    const nextPath = nextImg?.getAttribute("data-cover-path") || "";
    const currentVersion =
      currentImg?.getAttribute("data-cover-version") || "";
    const nextVersion = nextImg?.getAttribute("data-cover-version") || "";
    const coverShapeChanged =
      Boolean(currentImg) !== Boolean(nextImg) ||
      currentSrc !== nextSrc ||
      currentPath !== nextPath ||
      currentVersion !== nextVersion;
    if (coverShapeChanged) {
      currentCover.replaceChildren(...Array.from(nextCover.childNodes));
      return;
    }

    [
      ".game-card-overlay-platform",
      ".game-card-overlay-info",
      ".game-card-overlay-name",
      ".game-card-overlay-status",
      ".game-card-overlay-last-played",
      ".game-card-overlay-meta",
    ].forEach((selector) => {
      const current = currentCover.querySelector(selector);
      const next = nextCover.querySelector(selector);
      if (!current || !next) {
        return;
      }
      current.className = next.className;
      current.title = next.title;
      current.innerHTML = next.innerHTML;
    });
  },

  isGameVisibleInAllGames(game) {
    if (!game) {
      return false;
    }
    if (this.state.filterTag) {
      if (this.state.filterTag === "Steam 游戏" && !game.isSteam) {
        return false;
      }
      if (this.state.filterTag === "第三方游戏" && game.isSteam) {
        return false;
      }
      if (
        !["Steam 游戏", "第三方游戏"].includes(this.state.filterTag) &&
        !(game.tags || []).includes(this.state.filterTag)
      ) {
        return false;
      }
    }
    return this.filterGames([game]).length > 0;
  },

  syncGameCardInContainer(container, emptyState, game, accounts, shouldShow) {
    if (!container || !game) {
      return;
    }

    const selector = `.game-card[data-game-id="${this.escapeCssValue(game.id)}"]`;
    const existing = container.querySelector(selector);
    if (!shouldShow) {
      if (existing) {
        this.scheduleAnimatedRemoval(container, existing, 220);
      }
      return;
    }

    const html = this.renderGameCard(game, accounts);
    const nextCard = this.createElementFromHTML(html);
    if (!nextCard) {
      return;
    }

    nextCard.dataset.renderKey = game.id;
    nextCard.dataset.renderSignature = this.hashString(html);
    if (existing) {
      this.morphGameCard(existing, nextCard, nextCard.dataset.renderSignature);
    } else {
      nextCard.classList.add("is-entering");
      container.appendChild(nextCard);
      window.setTimeout(() => nextCard.classList.remove("is-entering"), 280);
    }
    if (emptyState) {
      emptyState.style.display = "none";
    }
  },

  updateSavedGameDOM(gameId) {
    const { games = [], accounts = [] } = this.getState();
    const game = games.find((item) => item.id === gameId);
    if (!game) {
      return;
    }

    this.syncGameCardInContainer(
      this.dom.gamesGrid,
      this.dom.gamesEmpty,
      game,
      accounts,
      this.isGameVisibleInAllGames(game),
    );

    this.syncGameCardInContainer(
      this.dom.favoriteGamesGrid,
      this.dom.favoriteGamesEmpty,
      game,
      accounts,
      this.isFavoriteGame(game.id) && this.filterGames([game]).length > 0,
    );

    if (this.state.page === "all-tags") {
      this.renderTagsPage();
    }
    if (this.state.page === "accounts") {
      this.renderAccounts();
    }
    this.refreshIcons();
  },

  renderGameCard(game, accounts) {
    const lastSync = game.lastSync || null;
    const favorite = this.isFavoriteGame(game.id);
    const hasLocalPaths = Boolean(game.installPath && game.savePath);
    const visibleGames = this.filterGames(
      this.state.page === "favorite-games"
        ? (this.getState().games || []).filter((item) =>
            this.isFavoriteGame(item.id),
          )
        : this.getState().games || [],
    );
    const cardIndex = visibleGames.findIndex((item) => item.id === game.id);
    let statusText = this.syncStatusText(lastSync?.status, game.sync?.enabled);
    const backupCount = this.getGameBackupCount(game);
    const lastPlayedText = game.lastPlayed
      ? this.formatRelativeTime(game.lastPlayed)
      : "未游玩";

    let badgeClass = "";
    let badgeIcon = "refresh-cw";

    if (this.state.runtimeStatus && this.state.runtimeStatus[game.id]) {
      const st = this.state.runtimeStatus[game.id];
      statusText = st.text;
      badgeIcon = st.icon || "refresh-cw";
      badgeClass = "is-visible " + (st.statusClass || "is-syncing");
    }

    const platformOverlayClass = game.isSteam ? "is-steam" : "is-thirdparty";
    const platformIconHtml = game.isSteam
      ? `<svg viewBox="0 0 24 24"><path d="M11.967 1.635c-5.748 0-10.407 4.66-10.407 10.408 0 4.258 2.56 7.925 6.224 9.569l2.793-4.043c-.046-.226-.076-.456-.076-.695 0-2.062 1.674-3.738 3.738-3.738.56 0 1.085.127 1.564.343l3.66-5.32c-.015-.098-.027-.197-.027-.297 0-1.859 1.507-3.366 3.366-3.366s3.365 1.507 3.365 3.366-1.506 3.365-3.365 3.365c-.714 0-1.378-.225-1.933-.61l-3.565 5.178c.08.318.125.648.125.992 0 2.062-1.674 3.738-3.738 3.738-1.748 0-3.21-1.2-3.618-2.825l-4.475 1.303c .177.067 .364.103 .555.103 1.094 0 1.983-.889 1.983-1.984s-.889-1.983-1.983-1.983c-1.018 0-1.852.766-1.966 1.751l-3.66-1.077c1.376-4.667 5.67-8.118 10.742-8.118 6.182 0 11.2 5.018 11.2 11.2S16.982 22.4 10.8 22.4c-6.183 0-11.2-5.018-11.2-11.2S4.618 0 10.8 0c.394 0 .783.02 1.167.06l-.043 1.575zM22.8 7.37c0-1.428-1.157-2.585-2.586-2.585-1.428 0-2.585 1.157-2.585 2.585 0 1.429 1.157 2.586 2.585 2.586 1.429 0 2.586-1.157 2.586-2.586zm-1.852 0c0-1.024-.83-1.853-1.853-1.853s-1.853.829-1.853 1.853 .83 1.853 1.853 1.853c1.024 0 1.853-.829 1.853-1.853zM14.242 16.924c0 1.636-1.326 2.962-2.962 2.962-1.635 0-2.96-1.326-2.96-2.962 0-1.635 1.325-2.96 2.96-2.96 1.636 0 2.962 1.325 2.962 2.96zm-2.228 0c0-1.23-1-2.23-2.23-2.23-1.231 0-2.23 1-2.23 2.23 0 1.23 1 2.23 2.23 2.23 1.23 0 2.23-1 2.23-2.23z" fill-rule="evenodd" clip-rule="evenodd" /></svg>`
      : `<svg viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2C6.48 2 2 6.48 2 12C2 17.52 6.48 22 12 22C17.52 22 22 17.52 22 12C22 6.48 17.52 2 12 2Z"></path><path d="M12 22C17.52 22 22 17.52 22 12C22 6.48 17.52 2 12 2C6.48 2 2 6.48 2 12C2 14.76 3.12 17.26 4.93 19.07"></path><path d="M11 11H8V14M11 14H8M11 11V14"></path></svg>`;

    return `
      <article class="game-card${hasLocalPaths ? "" : " is-translucent"}" data-render-key="${this.escapeHtmlAttribute(game.id)}" data-game-id="${this.escapeHtmlAttribute(game.id)}" data-index="${cardIndex}">
        <div class="game-card-cover">
          ${this.renderCover(game.id, game.coverPath)}
          <div class="game-card-overlay-platform ${platformOverlayClass}" aria-label="${game.isSteam ? "Steam 游戏" : "第三方游戏"}">
            ${platformIconHtml}
          </div>
          <div class="game-card-overlay">
            <div class="game-card-overlay-top" style="justify-content: flex-end;">
              <button
                type="button"
                class="game-card-overlay-info"
                data-action="open-backups"
                data-game-id="${this.escapeHtmlAttribute(game.id)}"
                aria-label="查看 ${this.escapeHtmlAttribute(game.name)} 的存档备份"
              >
                ${this.icon("archive", "overlay-icon")}<span>${backupCount}</span>
              </button>
            </div>
            <div class="game-card-overlay-bottom">
              <div class="game-card-overlay-name" title="${this.escapeHtml(game.name)}">${this.escapeHtml(game.name)}</div>
              ${hasLocalPaths ? "" : '<div class="game-card-path-warning">路径未配置</div>'}
              <div class="game-card-overlay-meta">
                <div class="game-card-overlay-status ${badgeClass}" style="display:flex; align-items:center; gap:4px; flex-shrink: 0;">
                  <span>${this.escapeHtml(statusText)}</span>
                  ${badgeClass ? `<span style="display:flex; align-items:center; width:12px; height:12px;">${this.icon(badgeIcon)}</span>` : ""}
                </div>
                <div class="game-card-overlay-last-played" title="上次游玩：${this.escapeHtml(game.lastPlayed ? this.formatTime(game.lastPlayed) : "从未")}">
                  ${this.escapeHtml(lastPlayedText)}
                </div>
              </div>
            </div>
          </div>
        </div>
      </article>
    `;
  },

  renderTagCard(summary) {
    const tagIndex = this.filterTags(
      this.collectTagSummaries(this.getState().games || []),
    ).findIndex((item) => item.name === summary.name);
    const isPinned = this.isPinnedTag(summary.name);
    return `
      <article class="tag-card${isPinned ? " is-pinned" : ""}" data-render-key="${this.escapeHtmlAttribute(summary.name)}" data-tag="${this.escapeHtmlAttribute(summary.name)}" data-index="${tagIndex}">
        <div class="tag-card-head">
          <div style="display:flex; align-items:center; gap:12px; min-width:0;">
            <div class="tag-card-icon">${this.icon("tags", "tag-card-icon-svg")}</div>
            <div style="min-width:0;">
              <div class="tag-card-title">${this.escapeHtml(summary.name)}${isPinned ? `<span class="tag-card-pin" title="已固定到侧栏">${this.icon("pin")}</span>` : ""}</div>
              <div class="tag-card-meta">${summary.syncedCount} 个游戏有最近同步记录</div>
            </div>
          </div>
          <div class="tag-card-count">${summary.count}</div>
        </div>
      </article>
    `;
  },

  renderAccountCard(account) {
    const isVerifying = this.state.verifyingAccountId === account.id;
    const isValid = account.verificationState === "valid" && !account.lastError;
    const verified = isVerifying
      ? "验证中..."
      : account.lastVerifiedAt
        ? this.formatTime(account.lastVerifiedAt)
        : "等待验证";
    const tokenExpires = isVerifying
      ? "验证中..."
      : this.formatAccountTokenExpiry(account);
    const usageSummary = account.usageWarning
      ? `-- / ${this.formatBytes(R2_FREE_TIER_STORAGE_BYTES)}`
      : this.formatUsageQuota(
          account.usedBytes || 0,
          R2_FREE_TIER_STORAGE_BYTES,
        );
    const verifyStatus = isVerifying
      ? "正在验证 Cloudflare 凭据..."
      : account.lastError || (isValid ? "验证通过" : "待验证或需要更新凭据");
    const badgeClass = isVerifying
      ? "warning"
      : account.lastError
        ? "error"
        : isValid
          ? "success"
          : "warning";
    const verifyButtonClass = `btn btn-secondary btn-sm${isVerifying ? " animate-pulse" : ""}`;
    const roleTitle = account.isPrimary
      ? "主账号 / D1 索引中心"
      : "副账号 / R2 存档池";
    const roleLabel = account.isPrimary ? "主账号" : "副账号";
    const backupText = account.isPrimary
      ? "主账号验证后自动同步副账号"
      : "副账号凭据保存在主账号 catalog";
    const statusTone = isVerifying
      ? "warning"
      : account.lastError
        ? "error"
        : isValid
          ? "success"
          : "warning";
    const enabledText = account.enabled ? "启用" : "停用";
    const d1Text =
      account.d1DatabaseId || (account.isPrimary ? "未配置" : "由主账号管理");
    const cardClass = `account-card account-card-${statusTone}${isValid ? "" : " is-translucent"}`;
    return `
      <article class="${cardClass}" data-render-key="${this.escapeHtmlAttribute(account.id)}" data-account-id="${this.escapeHtmlAttribute(account.id)}">
        <div class="account-card-accent"></div>
        <div class="account-card-header">
          <div class="account-card-title-block">
            <div class="account-card-icon">${this.icon(account.isPrimary ? "cloud" : "hard-drive")}</div>
            <div class="account-card-heading">
              <div class="account-card-title-row">
                <div class="account-card-title">${this.escapeHtml(account.name)}</div>
                <span class="account-role-badge">${this.escapeHtml(roleLabel)}</span>
              </div>
              <div class="account-card-meta">${this.escapeHtml(roleTitle)} · ${this.escapeHtml(enabledText)}</div>
              <div class="account-card-id">${this.escapeHtml(account.accountId || "未填写 Account ID")}</div>
            </div>
          </div>
          <div class="account-card-usage-pill">
            <span class="badge ${badgeClass}">${this.escapeHtml(usageSummary)}</span>
            <div class="account-card-meta">${this.escapeHtml(backupText)}</div>
          </div>
        </div>
        <div class="account-card-body">
          <div class="account-card-field"><span class="settings-item-label">D1 Database</span><span class="account-card-value">${this.escapeHtml(d1Text)}</span></div>
          <div class="account-card-field"><span class="settings-item-label">R2 Bucket</span><span class="account-card-value">${this.escapeHtml(account.r2Bucket || "未配置")}</span></div>
          <div class="account-card-field"><span class="settings-item-label">最近验证</span><span class="account-card-value">${this.escapeHtml(verified)}</span></div>
          <div class="account-card-field"><span class="settings-item-label">Token 过期</span><span class="account-card-value">${this.escapeHtml(tokenExpires)}</span></div>
          <div class="account-card-field"><span class="settings-item-label">状态</span><span class="account-card-value">${this.escapeHtml(verifyStatus)}</span></div>
        </div>
        <div class="account-card-actions">
          <button class="${verifyButtonClass}" data-action="verify-account" data-account-id="${this.escapeHtmlAttribute(account.id)}" ${isVerifying ? "disabled" : ""}>${this.icon("refresh-cw")}<span>${isVerifying ? "验证中" : "验证"}</span></button>
          ${account.isPrimary ? `<button class="btn btn-secondary btn-sm" data-action="restore-primary" data-account-id="${this.escapeHtmlAttribute(account.id)}">${this.icon("download")}<span>恢复</span></button>` : ""}
          <button class="btn btn-secondary btn-sm" data-action="edit-account" data-account-id="${this.escapeHtmlAttribute(account.id)}">${this.icon("settings")}<span>编辑</span></button>
          <button class="btn btn-danger btn-sm" data-action="delete-account" data-account-id="${this.escapeHtmlAttribute(account.id)}">${this.icon("trash-2")}<span>删除</span></button>
        </div>
      </article>
    `;
  },

  renderActivityCard(activity) {
    const badgeClass = this.statusBadgeClass(activity.status);
    return `
      <article class="activity-card" data-render-key="${this.escapeHtmlAttribute(activity.id)}" data-activity-id="${this.escapeHtmlAttribute(activity.id)}">
        <div class="activity-card-header">
          <div>
            <div class="activity-card-title">${this.escapeHtml(activity.gameName || "未知游戏")}</div>
            <div class="activity-card-meta">${this.escapeHtml(activity.message || "无描述")}</div>
          </div>
          <span class="badge ${badgeClass || "muted"}">${this.escapeHtml(this.syncStatusText(activity.status, true))}</span>
        </div>
        <div class="activity-card-body">
          <div class="activity-card-meta">开始时间：${this.formatTime(activity.startedAt)}</div>
          <div class="activity-card-meta">结束时间：${activity.endedAt ? this.formatTime(activity.endedAt) : "进行中"}</div>
          <div class="activity-card-meta">上传 ${activity.uploaded || 0} 个文件 · 下载 ${activity.downloaded || 0} 个文件 · 冲突 ${activity.conflicts || 0} 个</div>
        </div>
      </article>
    `;
  },

  formatAccountTokenExpiry(account) {
    if (!account?.isPrimary) {
      return "不适用";
    }
    if (account.tokenExpiresAt) {
      return this.formatTime(account.tokenExpiresAt);
    }
    if (account.lastVerifiedAt && !account.lastError) {
      return "未设置过期";
    }
    return "待验证";
  },

  renderGameDetail(gameId) {
    if (!this.dom.gameDetailContent) {
      return;
    }
    const { games = [], accounts = [] } = this.getState();
    const game = games.find((item) => item.id === gameId);
    if (!game) {
      this.closeModal("game-modal");
      return;
    }

    const account = accounts.find((item) => item.id === game.storageAccountId);
    const lastSync = game.lastSync || null;
    const statusText = this.syncStatusText(
      lastSync?.status,
      game.sync?.enabled,
    );
    const statusClass = this.statusBadgeClass(lastSync?.status) || "muted";
    const favorite = this.isFavoriteGame(game.id);

    this.dom.gameDetailContent.innerHTML = `
      <div class="detail-shell">
        <div class="detail-main">
          <div class="detail-cover-panel">
            <div class="detail-cover">
              ${this.renderCover(game.id, game.coverPath).replace("game-card-cover-placeholder", "detail-cover-placeholder")}
            </div>
            <div class="detail-note-list">
              <div class="detail-note">简介和资料可通过 RAWG 更新，封面可通过 RAWG 或 SteamGridDB 选择；本地路径与同步配置仍由当前项目管理。</div>
              <div class="badge muted">${game.rawgId ? `RAWG #${this.escapeHtml(game.rawgId)}` : "未关联 RAWG"}</div>
            </div>
            <div class="detail-tags">
              <span class="badge ${statusClass}">${this.escapeHtml(statusText)}</span>
              <span class="badge muted">${this.escapeHtml(account?.name || "未分配账号")}</span>
              ${favorite ? '<span class="badge warning">常玩</span>' : ""}
            </div>
          </div>
          <div>
            <div class="detail-title-row">
              <div>
                <div class="detail-title">${this.escapeHtml(game.name)}</div>
                <div class="detail-subtitle">最近同步：${this.escapeHtml(lastSync?.syncedAt ? this.formatTime(lastSync.syncedAt) : "从未")} · 冲突策略：${this.escapeHtml(this.conflictPolicyText(game.sync?.conflictStrategy))}</div>
              </div>
            </div>
            <div class="detail-stat-grid">
              <div class="detail-stat"><div class="detail-stat-label">上传</div><div class="detail-stat-value">${lastSync?.uploaded || 0}</div></div>
              <div class="detail-stat"><div class="detail-stat-label">下载</div><div class="detail-stat-value">${lastSync?.downloaded || 0}</div></div>
              <div class="detail-stat"><div class="detail-stat-label">RAWG 评分</div><div class="detail-stat-value" style="font-size:16px;">${this.escapeHtml(this.formatRating(game.rating, game.ratingTop || 5))}</div></div>
              <div class="detail-stat"><div class="detail-stat-label">同步</div><div class="detail-stat-value" style="font-size:16px;">${game.sync?.enabled ? "开启" : "关闭"}</div></div>
            </div>
            <div class="detail-sections">
              <section class="detail-section">
                <div class="detail-section-title">游戏简介</div>
                <div class="detail-description">${game.description ? this.formatMultilineText(game.description) : "暂无简介，可点击底部 RAWG 获取资料。"}</div>
              </section>
              <section class="detail-section">
                <div class="detail-section-title">标签</div>
                <div class="detail-tags">${(game.tags || []).length ? game.tags.map((tag) => `<span class="tag">${this.escapeHtml(tag)}</span>`).join("") : '<span class="tag">未打标签</span>'}</div>
              </section>
              <section class="detail-section">
                <div class="detail-section-title">RAWG 标签建议</div>
                <div class="detail-tags">${this.renderInteractiveTagButtons(game.rawgTags || [], game.tags || [], "toggle-rawg-tag")}</div>
                <div class="field-help">点击即可加入或移除当前游戏标签。</div>
              </section>
              <section class="detail-section">
                <div class="detail-section-title">本机路径</div>
                <div class="detail-info-row"><span class="settings-item-label">启动文件</span><span class="detail-info-value">${this.escapeHtml(game.installPath || "未设置")}</span></div>
                <div class="detail-info-row"><span class="settings-item-label">存档目录</span><span class="detail-info-value">${this.escapeHtml(game.savePath || "未设置")}</span></div>
              </section>
              <section class="detail-section">
                <div class="detail-section-title">游戏详情</div>
                <div class="detail-info-row"><span class="settings-item-label">发售日期</span><span class="detail-info-value">${this.escapeHtml(game.released || "未提供")}</span></div>
                <div class="detail-info-row"><span class="settings-item-label">Metacritic</span><span class="detail-info-value">${this.escapeHtml(game.metacritic ? String(game.metacritic) : "未提供")}</span></div>
                <div class="detail-info-row"><span class="settings-item-label">类型</span><span class="detail-info-value">${this.escapeHtml(this.formatList(game.genres, "未提供"))}</span></div>
                <div class="detail-info-row"><span class="settings-item-label">平台</span><span class="detail-info-value">${this.escapeHtml(this.formatList(game.platforms, "未提供"))}</span></div>
                <div class="detail-info-row"><span class="settings-item-label">开发商</span><span class="detail-info-value">${this.escapeHtml(this.formatList(game.developers, "未提供"))}</span></div>
                <div class="detail-info-row"><span class="settings-item-label">发行商</span><span class="detail-info-value">${this.escapeHtml(this.formatList(game.publishers, "未提供"))}</span></div>
                <div class="detail-info-row"><span class="settings-item-label">官网</span><span class="detail-info-value">${this.escapeHtml(game.website || "未提供")}</span></div>
                <div class="detail-info-row"><span class="settings-item-label">RAWG 页面</span><span class="detail-info-value">${this.escapeHtml(game.rawgUrl || "未关联")}</span></div>
              </section>
              <section class="detail-section">
                <div class="detail-section-title">同步摘要</div>
                <div class="detail-info-row"><span class="settings-item-label">最近结果</span><span class="detail-info-value">${this.escapeHtml(lastSync?.message || "尚未同步")}</span></div>
                <div class="detail-info-row"><span class="settings-item-label">账号</span><span class="detail-info-value">${this.escapeHtml(account?.name || "自动选择首个可用账号")}</span></div>
                <div class="detail-info-row"><span class="settings-item-label">模式</span><span class="detail-info-value">${this.escapeHtml(game.sync?.enabled ? "Cloudflare 同步" : "仅本地管理")}</span></div>
              </section>
            </div>
          </div>
        </div>
      </div>
    `;

    this.dom.detailOpenInstallBtn.disabled = !game.installPath;
    this.dom.detailOpenSaveBtn.disabled = !game.savePath;
    this.dom.detailOpenInstallBtn.textContent = game.installPath
      ? "打开启动文件"
      : "未配置启动文件";
    this.dom.detailOpenSaveBtn.textContent = game.savePath
      ? "打开存档目录"
      : "未配置存档目录";
    this.refreshIcons();
  },

  setPage(page) {
    if (!PAGE_META[page]) {
      return;
    }
    this.state.page = page;
    this.state.filterTag = "";
    this.resetSearch();
    this.renderDataViews();
  },

  openTagFilter(tag) {
    this.state.page = "all-games";
    this.state.filterTag = tag;
    this.resetSearch();
    this.renderDataViews();
  },

  clearTagFilter() {
    this.state.filterTag = "";
    this.resetSearch();
    this.renderDataViews();
  },

  handleTopbarPrimaryAction() {
    if (this.state.page === "accounts" || this.state.page === "settings") {
      this.openAccountModal();
      return;
    }
    if (this.state.page === "activities") {
      this.refreshSnapshot("已刷新活动列表");
      return;
    }
    this.openGameModal();
  },

  handleTopbarSecondaryAction() {
    if (this.state.page === "all-games" && this.state.filterTag) {
      this.clearTagFilter();
      return;
    }
    if (this.state.page === "accounts" || this.state.page === "activities") {
      this.refreshSnapshot("已刷新数据");
      return;
    }
    this.syncAllGames();
  },

  handleGameGridClick(event) {
    if (Date.now() < (this.state.dragJustHappenedUntil || 0)) {
      event.preventDefault();
      event.stopPropagation();
      return;
    }

    const button = event.target.closest("[data-action]");
    if (button) {
      const action = button.dataset.action;
      const gameId = button.dataset.gameId;

      if (action === "toggle-favorite" && gameId) {
        event.stopPropagation();
        this.toggleFavoriteGame(gameId);
        return;
      }

      if (action === "open-backups" && gameId) {
        event.stopPropagation();
        this.openBackupModal(gameId);
      }
      return;
    }

    const overlayInfo = event.target.closest(".game-card-overlay-info");
    const article = event.target.closest(".game-card[data-game-id]");

    if (overlayInfo && article?.dataset.gameId) {
      event.stopPropagation();
      this.openBackupModal(article.dataset.gameId);
      return;
    }

    if (article?.dataset.gameId) {
      const game = this.getState().games.find(
        (g) => g.id === article.dataset.gameId,
      );
      if (game?.installPath) {
        void this.startGameWithPreSync(game.id);
      } else {
        this.openGameModal(article.dataset.gameId);
      }
    }
  },

  showGameContextMenu(event) {
    event.preventDefault();
    const article = event.target.closest(".game-card[data-game-id]");
    if (!article?.dataset.gameId) return;

    this.hideTagContextMenu();
    this.state.contextMenuGameId = article.dataset.gameId;
    const isFav = this.isFavoriteGame(article.dataset.gameId);

    // Update fav text dynamically
    const favTextNode = document.getElementById("ctx-menu-fav-text");
    if (favTextNode) {
      favTextNode.textContent = isFav ? "移出常玩" : "加入常玩";
    }

    const menu = document.getElementById("game-context-menu");
    menu.style.display = "block";
    const x = Math.min(event.clientX, window.innerWidth - 180);
    const y = Math.min(event.clientY, window.innerHeight - 220);
    menu.style.left = x + "px";
    menu.style.top = y + "px";
    this.refreshIcons();
  },

  hideGameContextMenu() {
    document.getElementById("game-context-menu").style.display = "none";
    this.state.contextMenuGameId = "";
  },

  handleContextMenuAction(event) {
    const btn = event.target.closest("[data-action]");
    if (!btn) return;
    const gameId = this.state.contextMenuGameId;
    this.hideGameContextMenu();
    if (!gameId) return;

    switch (btn.dataset.action) {
      case "ctx-favorite":
        this.toggleFavoriteGame(gameId);
        break;
      case "ctx-detail":
        this.openGameModal(gameId);
        break;
      case "ctx-sync":
        this.runSync(gameId);
        break;
      case "ctx-delete":
        this.deleteGame(gameId);
        break;
    }
  },

  showTagContextMenu(event) {
    event.preventDefault();
    const article = event.target.closest(".tag-card[data-tag]");
    if (!article?.dataset.tag) return;

    this.hideGameContextMenu();
    this.state.contextMenuTag = article.dataset.tag;
    const isPinned = this.isPinnedTag(article.dataset.tag);

    const pinTextNode = document.getElementById("ctx-menu-pin-text");
    if (pinTextNode) {
      pinTextNode.textContent = isPinned ? "取消固定" : "固定到侧栏";
    }
    const pinIconNode = document.getElementById("ctx-menu-pin-icon");
    if (pinIconNode) {
      pinIconNode.dataset.lucide = isPinned ? "pin-off" : "pin";
    }

    const menu = document.getElementById("tag-context-menu");
    menu.style.display = "block";
    const x = Math.min(event.clientX, window.innerWidth - 180);
    const y = Math.min(event.clientY, window.innerHeight - 120);
    menu.style.left = x + "px";
    menu.style.top = y + "px";
    this.refreshIcons();
  },

  hideTagContextMenu() {
    document.getElementById("tag-context-menu").style.display = "none";
    this.state.contextMenuTag = "";
  },

  handleTagContextMenuAction(event) {
    const btn = event.target.closest("[data-action]");
    if (!btn) return;
    const tag = this.state.contextMenuTag;
    this.hideTagContextMenu();
    if (!tag) return;

    switch (btn.dataset.action) {
      case "ctx-pin-tag":
        this.togglePinnedTag(tag);
        break;
      case "ctx-view-tag":
        this.openTagFilter(tag);
        break;
    }
  },

  handleTagGridClick(event) {
    const card = event.target.closest(".tag-card[data-tag]");
    if (!card?.dataset.tag) {
      return;
    }
    this.openTagFilter(card.dataset.tag);
  },

  handlePinnedTagNavClick(event) {
    const button = event.target.closest(".pinned-tag-nav-btn[data-tag]");
    if (!button?.dataset.tag) {
      return;
    }
    this.openTagFilter(button.dataset.tag);
  },

  handleAccountGridClick(event) {
    const button = event.target.closest("[data-action]");
    if (!button) {
      return;
    }
    const action = button.dataset.action;
    const accountId = button.dataset.accountId;

    if (action === "verify-account" && accountId) {
      this.verifyAccount(accountId);
      return;
    }
    if (action === "edit-account" && accountId) {
      this.openAccountModal(accountId);
      return;
    }
    if (action === "delete-account" && accountId) {
      this.deleteAccount(accountId);
      return;
    }
    if (action === "restore-primary") {
      this.restoreFromPrimary();
    }
  },

  handleLocalSettingsClick(event) {
    const button = event.target.closest("[data-action]");
    if (!button) {
      return;
    }
    const actionMap = {
      "pick-default-steam-install-dir": [
        "defaultSteamInstallDir",
        "Steam 游戏路径已更新",
      ],
      "pick-default-steam-save-dir": [
        "defaultSteamSaveDir",
        "Steam 存档路径已更新",
      ],
      "pick-default-third-install-dir": [
        "defaultThirdInstallDir",
        "第三方游戏路径已更新",
      ],
      "pick-default-third-save-dir": [
        "defaultThirdSaveDir",
        "第三方存档路径已更新",
      ],
    };
    const config = actionMap[button.dataset.action];
    if (config) {
      this.pickDefaultPreferencePath(config[0], config[1]);
    }
  },

  openGameModal(gameId = "") {
    this.state.currentGameId = gameId; // Set current game ID for context
    const game = this.getState().games.find((item) => item.id === gameId);
    this.state.gameFormMetadata = this.extractMetadataFromGame(game);
    document.getElementById("game-modal-title").textContent = game
      ? "游戏详情设定"
      : "添加游戏";
    document.getElementById("game-id").value = game?.id || "";
    document.getElementById("game-name").value = game?.name || "";
    if (document.getElementById("game-is-steam")) {
      document.getElementById("game-is-steam").checked = game?.isSteam || false;
    }

    // Description Reset
    const descText = game?.description || "";
    document.getElementById("game-description").value = descText;
    document.getElementById("game-description-read").textContent =
      descText || "暂无简介。";
    document.getElementById("game-description-read").style.display = "block";
    document.getElementById("game-description").style.display = "none";
    const editBtn = document.getElementById("game-description-edit-btn");
    if (editBtn) editBtn.textContent = "编辑";

    document.getElementById("game-install-path").value =
      game?.installPath || "";
    document.getElementById("game-save-path").value = game?.savePath || "";
    document.getElementById("game-cover-path").value = game?.coverPath || "";

    // Process tags
    this.setCurrentFormTags(game?.tags || []);
    document.getElementById("game-tags-display").style.display = "flex";

    document.getElementById("game-conflict-strategy").value =
      game?.sync?.conflictStrategy || "manual";
    this.syncCustomSelect("game-conflict-strategy");
    this.populateAccountSelect(game?.storageAccountId || "");
    this.updateCoverPreview(game?.id || game?.coverPath || "");
    this.updateGameModalHeaderStats(game || null);

    // Show/hide bottom action buttons depending on game state
    const syncBtn = document.getElementById("modal-sync-btn");

    if (game) {
      syncBtn.style.display = "inline-block";
      syncBtn.textContent = "手动同步";
      const launchBtn = document.getElementById("modal-launch-btn");
      const backupBtn = document.getElementById("modal-manage-backup-btn");
      const btnDivider = document.getElementById("modal-btn-divider");
      if (launchBtn) launchBtn.style.display = "flex";
      if (backupBtn) backupBtn.style.display = "flex";
      if (btnDivider) btnDivider.style.display = "block";
    } else {
      syncBtn.style.display = "none";
      const launchBtn = document.getElementById("modal-launch-btn");
      const backupBtn = document.getElementById("modal-manage-backup-btn");
      const btnDivider = document.getElementById("modal-btn-divider");
      if (launchBtn) launchBtn.style.display = "none";
      if (backupBtn) backupBtn.style.display = "none";
      if (btnDivider) btnDivider.style.display = "none";
    }

    this.openModal("game-modal");
  },

  updateGameModalHeaderStats(game) {
    const playTimeElement = document.getElementById("game-modal-playtime");
    const lastPlayedElement = document.getElementById("game-modal-last-played");
    const statsElement = document.getElementById("game-modal-header-stats");
    if (!playTimeElement || !lastPlayedElement || !statsElement) {
      return;
    }

    if (!game) {
      playTimeElement.textContent = "--";
      lastPlayedElement.textContent = "--";
      statsElement.style.opacity = "0.72";
      return;
    }

    playTimeElement.textContent = this.formatPlayTime(game.playTime || 0);
    lastPlayedElement.textContent = game.lastPlayed
      ? this.formatRelativeTime(game.lastPlayed)
      : "从未游玩";
    statsElement.style.opacity = "";
  },

  async handlePlatformToggleChange(event) {
    const gameId =
      document.getElementById("game-id")?.value.trim() ||
      this.state.currentGameId;
    if (!gameId) {
      return;
    }

    const game = this.getState().games.find((item) => item.id === gameId);
    if (!game) {
      return;
    }

    const nextIsSteam = Boolean(event?.target?.checked);
    if (Boolean(game.isSteam) === nextIsSteam) {
      return;
    }

    const input = event?.target;
    if (input) {
      input.disabled = true;
    }

    try {
      await this.saveGamePatch(gameId, { isSteam: nextIsSteam }, "", {
        skipSync: true,
        skipRender: true,
        skipToast: true,
        updateCard: true,
      });
      this.updateNetworkStatus("online", "所属平台已更新");
    } catch (error) {
      console.error(error);
      if (input) {
        input.checked = Boolean(game.isSteam);
      }
      this.showToast(error.message || "更新所属平台失败", "error");
      this.updateNetworkStatus("offline", error.message || "更新所属平台失败");
    } finally {
      if (input) {
        input.disabled = false;
      }
    }
  },

  openCurrentInstallPath() {
    const game = this.getState().games.find(
      (item) => item.id === this.state.currentGameId,
    );
    if (game?.installPath) {
      this.openPath(game.installPath);
    }
  },

  openCurrentSavePath() {
    const game = this.getState().games.find(
      (item) => item.id === this.state.currentGameId,
    );
    if (game?.savePath) {
      this.openPath(game.savePath);
    }
  },

  syncCurrentGame() {
    if (!this.state.currentGameId) {
      return;
    }
    this.runSync(this.state.currentGameId);
  },

  openAccountModal(accountId = "") {
    const account = this.getState().accounts.find(
      (item) => item.id === accountId,
    );
    const isPrimary =
      account?.isPrimary ?? this.getState().accounts.length === 0;
    const accountName =
      account?.name || (isPrimary ? "主账号" : "保存后自动分配");
    document.getElementById("account-modal-title").textContent = account
      ? "编辑 Cloudflare 账号"
      : "添加 Cloudflare 账号";
    document.getElementById("account-id").value = account?.id || "";
    document.getElementById("account-name").value = accountName;
    document.getElementById("account-name-display").textContent = accountName;
    document.getElementById("account-account-id").value =
      account?.accountId || "";
    document.getElementById("account-api-token").value =
      account?.apiToken || "";
    document.getElementById("account-d1-id").value =
      account?.d1DatabaseId || "";
    document.getElementById("account-r2-bucket").value =
      account?.r2Bucket || "";
    document.getElementById("account-r2-key").value =
      account?.r2AccessKeyId || "";
    document.getElementById("account-r2-secret").value =
      account?.r2SecretAccessKey || "";
    document.getElementById("account-enabled").checked =
      account?.enabled ?? true;
    document.getElementById("account-is-primary").checked = isPrimary;
    this.updateAccountD1Required();
    this.resetSecretFields(["account-api-token", "account-r2-secret"]);
    this.openModal("account-modal");
  },

  openModal(id) {
    document.getElementById(id)?.classList.add("active");
  },

  closeModal(id) {
    document.getElementById(id)?.classList.remove("active");

    if (id === "rawg-picker-modal") {
      this.state.rawgSearchResults = [];
      this.state.rawgSearching = false;
      this.state.rawgApplying = false;
      this.renderRawgSearchResults();
    }
    if (id === "sgdb-picker-modal") {
      this.state.sgdbSearchResults = [];
      this.state.sgdbSearching = false;
      this.renderSteamGridDBSearchResults();
    }
  },

  updateAccountD1Required() {
    const isPrimary =
      document.getElementById("account-is-primary")?.checked ?? true;
    const roleDisplay = document.getElementById("account-role-display");
    if (roleDisplay) {
      roleDisplay.textContent = isPrimary
        ? "主账号 / D1 索引中心"
        : "副账号 / R2 存档池";
    }
    ["account-api-token-group", "account-d1-id-group"].forEach((id) => {
      document.getElementById(id)?.classList.toggle("is-hidden", !isPrimary);
    });
    ["account-api-token", "account-d1-id"].forEach((id) => {
      const input = document.getElementById(id);
      if (input) {
        input.required = isPrimary;
      }
    });
  },

  handleAccountFormClick(event) {
    const button = event.target.closest(
      '[data-action="toggle-secret"][data-target]',
    );
    if (!button) {
      return;
    }
    this.toggleSecretField(button);
  },

  toggleSecretField(button) {
    const input = document.getElementById(button.dataset.target);
    if (!input) {
      return;
    }
    const isShowing = input.type === "text";
    input.type = isShowing ? "password" : "text";
    button.setAttribute("aria-pressed", String(!isShowing));
    button.setAttribute(
      "aria-label",
      isShowing
        ? `显示 ${this.secretFieldLabel(input.id)}`
        : `隐藏 ${this.secretFieldLabel(input.id)}`,
    );
    button.innerHTML = this.icon(isShowing ? "eye" : "eye-off");
    this.refreshIcons();
  },

  resetSecretFields(ids) {
    ids.forEach((id) => {
      const input = document.getElementById(id);
      const button = document.querySelector(
        `[data-action="toggle-secret"][data-target="${id}"]`,
      );
      if (input) {
        input.type = "password";
      }
      if (button) {
        button.setAttribute("aria-pressed", "false");
        button.setAttribute("aria-label", `显示 ${this.secretFieldLabel(id)}`);
        button.innerHTML = this.icon("eye");
      }
    });
    this.refreshIcons();
  },

  secretFieldLabel(id) {
    if (id === "account-api-token") {
      return "API Token";
    }
    if (id === "account-r2-secret") {
      return "R2 Secret Access Key";
    }
    return "敏感信息";
  },

  closeConflictModal() {
    this.state.pendingConflictGameId = "";
    this.state.pendingConflictMessage = "";
    this.state.pendingConflictAction = "";
    this.state.pendingLaunchGameId = "";
    this.closeModal("conflict-modal");
    this.updateNetworkStatus("online", "已取消本次冲突处理");
  },

  async submitGameForm(event) {
    event.preventDefault();
    const saveButton =
      this.dom.gameSaveBtn || document.getElementById("game-save-btn");
    this.setButtonBusy(saveButton, true, "保存中...");
    this.state.savingGameForm = true;
    this.beginSilentRuntimeStateUpdates();
    try {
      const gameId = document.getElementById("game-id").value.trim();
      const existing = this.getState().games.find((item) => item.id === gameId);
      const metadata = this.state.gameFormMetadata || {};
      const payload = {
        id: gameId,
        name: document.getElementById("game-name").value.trim(),
        isSteam: document.getElementById("game-is-steam")
          ? document.getElementById("game-is-steam").checked
          : false,
        installPath: document.getElementById("game-install-path").value.trim(),
        savePath: document.getElementById("game-save-path").value.trim(),
        coverPath: document.getElementById("game-cover-path").value.trim(),
        description: this.dom.gameDescription.value.trim(),
        released: metadata.released || "",
        rating: metadata.rating || 0,
        ratingTop: metadata.ratingTop || 0,
        metacritic: metadata.metacritic || 0,
        genres: metadata.genres || [],
        platforms: metadata.platforms || [],
        developers: metadata.developers || [],
        publishers: metadata.publishers || [],
        website: metadata.website || "",
        rawgId: metadata.rawgId || 0,
        rawgSlug: metadata.rawgSlug || "",
        rawgUrl: metadata.rawgUrl || "",
        rawgTags: metadata.rawgTags || [],
        tags: this.currentFormTags(),
        storageAccountId: document.getElementById("game-account-id").value,
        anchor: existing?.anchor,
        lastSync: existing?.lastSync,
        sync: {
          enabled: true,
          includePatterns: ["*"],
          excludePatterns: [],
          conflictStrategy: document.getElementById("game-conflict-strategy")
            .value,
        },
      };
      const nextSnapshot = await this.bridge.SaveGame(payload);
      const savedGame = payload.id
        ? nextSnapshot.state.games.find((item) => item.id === payload.id)
        : [...(nextSnapshot.state.games || [])]
            .reverse()
            .find(
              (item) =>
                item.name === payload.name &&
                item.installPath === payload.installPath &&
                item.savePath === payload.savePath,
            );
      const savedGameId = savedGame?.id || gameId;

      this.applySnapshotSilently(nextSnapshot);
      if (savedGameId) {
        this.state.currentGameId = savedGameId;
        this.updateSavedGameDOM(savedGameId);
      }
      if (savedGameId && payload.sync?.enabled && payload.storageAccountId) {
        void this.runSync(savedGameId, "", true);
      }
      this.closeModal("game-modal");
      this.markLocalSnapshotEchoCandidate(this.state.snapshot);
      this.showToast("游戏配置已保存", "success");
      this.updateNetworkStatus("online", "游戏配置已保存");
    } catch (error) {
      console.error(error);
      this.showToast(error.message || "保存游戏失败", "error");
      this.updateNetworkStatus("offline", error.message || "保存游戏失败");
    } finally {
      this.endSilentRuntimeStateUpdates();
      this.state.savingGameForm = false;
      this.setButtonBusy(saveButton, false);
    }
  },

  async submitAccountForm(event) {
    event.preventDefault();
    try {
      const accountId = document.getElementById("account-id").value.trim();
      const existing = this.getState().accounts.find(
        (item) => item.id === accountId,
      );
      const isPrimary =
        existing?.isPrimary ?? this.getState().accounts.length === 0;
      const shouldVerifyAndRestore =
        isPrimary && !existing && this.getState().accounts.length === 0;
      const payload = {
        id: accountId,
        name: existing?.name || (isPrimary ? "主账号" : ""),
        accountId: document.getElementById("account-account-id").value.trim(),
        apiToken: document.getElementById("account-api-token").value.trim(),
        d1DatabaseId: document.getElementById("account-d1-id").value.trim(),
        r2Bucket: document.getElementById("account-r2-bucket").value.trim(),
        r2AccessKeyId: document.getElementById("account-r2-key").value.trim(),
        r2SecretAccessKey: document
          .getElementById("account-r2-secret")
          .value.trim(),
        isPrimary,
        enabled: document.getElementById("account-enabled").checked,
        usedBytes: existing?.usedBytes || 0,
        lastVerifiedAt: existing?.lastVerifiedAt || null,
        lastError: existing?.lastError || "",
        usageWarning: existing?.usageWarning || "",
        verificationState: "pending",
        credentialsBackedUp: existing?.credentialsBackedUp || false,
      };

      const savedSnapshot = await this.bridge.SaveAccount(payload);
      this.applySnapshot(savedSnapshot);
      this.closeModal("account-modal");
      this.state.page = "accounts";
      this.renderDataViews();

      if (shouldVerifyAndRestore) {
        const savedPrimary = (savedSnapshot.state.accounts || []).find(
          (item) => item.isPrimary,
        );
        if (savedPrimary?.id) {
          this.showToast("主账号已保存，正在验证并恢复配置...", "success");
          this.updateNetworkStatus("checking", "正在验证主账号并恢复配置");
          const verifiedSnapshot = await this.bridge.VerifyAccount(
            savedPrimary.id,
          );
          this.applySnapshot(verifiedSnapshot);
          this.showToast("主账号验证通过，已自动恢复配置", "success");
          this.updateNetworkStatus("online", "主账号验证通过，已恢复配置");
          return;
        }
      }

      this.showToast("Cloudflare 账号已保存", "success");
      this.updateNetworkStatus("online", "Cloudflare 账号已保存");
    } catch (error) {
      console.error(error);
      this.showToast(error.message || "保存账号失败", "error");
      this.updateNetworkStatus("offline", error.message || "保存账号失败");
    }
  },

  async submitPreferences(event) {
    event.preventDefault();
    try {
      this.applySnapshot(
        await this.bridge.SavePreferences(this.buildPreferencesPayload()),
        { renderSettings: true },
      );
      this.showToast("同步偏好已保存", "success");
      this.updateNetworkStatus("online", "同步偏好已保存");
    } catch (error) {
      console.error(error);
      this.showToast(error.message || "保存偏好失败", "error");
      this.updateNetworkStatus("offline", error.message || "保存偏好失败");
    }
  },

  buildPreferencesPayload(overrides = {}) {
    const current = this.getState().preferences || {};
    return {
      autoSyncOnLaunch: document.getElementById("pref-auto-sync").checked,
      startupSyncMode: document.getElementById("pref-startup-mode").value,
      conflictPolicy: document.getElementById("pref-conflict-policy").value,
      defaultInstallDir: current.defaultInstallDir || "",
      defaultSaveDir: current.defaultSaveDir || "",
      defaultSteamInstallDir:
        current.defaultSteamInstallDir ||
        current.defaultInstallDir ||
        "",
      defaultSteamSaveDir:
        current.defaultSteamSaveDir || current.defaultSaveDir || "",
      defaultThirdInstallDir:
        current.defaultThirdInstallDir ||
        current.defaultInstallDir ||
        "",
      defaultThirdSaveDir:
        current.defaultThirdSaveDir || current.defaultSaveDir || "",
      rawgApiKey: this.dom.prefRawgApiKey
        ? this.dom.prefRawgApiKey.value.trim()
        : current.rawgApiKey || "",
      steamGridDbApiKey: this.dom.prefSgdbApiKey
        ? this.dom.prefSgdbApiKey.value.trim()
        : current.steamGridDbApiKey || "",
      favoriteGames: [...this.state.favoriteGames],
      tagOrder: current.tagOrder || [],
      pinnedTags: current.pinnedTags || [],
      ...overrides,
    };
  },

  isCurrentFormSteamGame() {
    return Boolean(document.getElementById("game-is-steam")?.checked);
  },

  defaultInstallDirectoryForCurrentForm() {
    const preferences = this.getState().preferences || {};
    if (this.isCurrentFormSteamGame()) {
      return (
        preferences.defaultSteamInstallDir ||
        preferences.defaultInstallDir ||
        ""
      );
    }
    return (
      preferences.defaultThirdInstallDir ||
      preferences.defaultInstallDir ||
      ""
    );
  },

  directoryFromFilePath(path) {
    const normalized = String(path || "").trim().replace(/[\\/]+$/, "");
    if (!normalized) {
      return "";
    }
    const index = Math.max(
      normalized.lastIndexOf("/"),
      normalized.lastIndexOf("\\"),
    );
    if (index <= 0) {
      return "";
    }
    return normalized.slice(0, index);
  },

  defaultSaveDirectoryForCurrentForm() {
    const preferences = this.getState().preferences || {};
    if (!this.isCurrentFormSteamGame()) {
      const installPath = document
        .getElementById("game-install-path")
        ?.value.trim();
      const installDir = this.directoryFromFilePath(installPath);
      if (installDir) {
        return installDir;
      }
    }
    if (this.isCurrentFormSteamGame()) {
      return (
        preferences.defaultSteamSaveDir || preferences.defaultSaveDir || ""
      );
    }
    return (
      preferences.defaultThirdSaveDir || preferences.defaultSaveDir || ""
    );
  },

  extractMetadataFromGame(game) {
    return {
      released: game?.released || "",
      rating: game?.rating || 0,
      ratingTop: game?.ratingTop || 0,
      metacritic: game?.metacritic || 0,
      genres: [...(game?.genres || [])],
      platforms: [...(game?.platforms || [])],
      developers: [...(game?.developers || [])],
      publishers: [...(game?.publishers || [])],
      website: game?.website || "",
      rawgId: game?.rawgId || 0,
      rawgSlug: game?.rawgSlug || "",
      rawgUrl: game?.rawgUrl || "",
      rawgTags: [...(game?.rawgTags || [])],
    };
  },

  async openRawgPicker(context, initialQuery = "") {
    this.state.rawgPickerContext = context;
    this.state.rawgSearchResults = [];
    this.state.rawgSearching = false;
    this.state.rawgApplying = false;
    this.dom.rawgSearchInput.value = initialQuery || "";
    this.renderRawgSearchResults();
    this.openModal("rawg-picker-modal");
    if (initialQuery) {
      await this.searchRawgFromPicker();
    }
  },

  async openSteamGridDBPicker(context, initialQuery = "") {
    this.state.sgdbPickerContext = context;
    this.state.sgdbSearchResults = [];
    this.state.sgdbSearching = false;
    this.dom.sgdbSearchInput.value = initialQuery || "";
    this.renderSteamGridDBSearchResults();
    this.openModal("sgdb-picker-modal");
    if (initialQuery) {
      await this.searchSteamGridDBFromPicker();
    }
  },

  async searchRawgFromPicker() {
    const query = this.dom.rawgSearchInput.value.trim();
    if (!query) {
      this.showToast("请先输入要搜索的游戏名称", "warning");
      return;
    }

    this.state.rawgSearching = true;
    this.renderRawgSearchResults();
    try {
      this.state.rawgSearchResults = await this.bridge.SearchRAWGGames(query);
      if (!this.state.rawgSearchResults.length) {
        this.showToast("RAWG 未找到匹配结果，请换个关键词再试", "warning");
      }
    } catch (error) {
      console.error(error);
      const errMsg = String(error?.message || error || "");
      const isAuthError =
        errMsg.toLowerCase().includes("apikey") ||
        errMsg.toLowerCase().includes("unauthorized") ||
        errMsg.includes("401") ||
        errMsg.toLowerCase().includes("key");
      const msg = isAuthError
        ? "RAWG API 验证失败，请前往左下角“设置 -> 同步偏好”填写有效的 API Key"
        : errMsg || "RAWG 搜索失败";
      this.showToast(msg, "error");
      this.updateNetworkStatus("offline", msg);
    } finally {
      this.state.rawgSearching = false;
      this.renderRawgSearchResults();
    }
  },

  async searchSteamGridDBFromPicker() {
    const query = this.dom.sgdbSearchInput.value.trim();
    if (!query) {
      this.showToast("请先输入要搜索的游戏名称", "warning");
      return;
    }

    this.state.sgdbSearching = true;
    this.renderSteamGridDBSearchResults();
    try {
      this.state.sgdbSearchResults =
        await this.bridge.SearchSteamGridDBGames(query);
      if (!this.state.sgdbSearchResults.length) {
        this.showToast("SteamGridDB 未找到可用封面，请换个关键词再试", "warning");
      }
    } catch (error) {
      console.error(error);
      const errMsg = String(error?.message || error || "");
      const isAuthError =
        errMsg.toLowerCase().includes("apikey") ||
        errMsg.toLowerCase().includes("bearer") ||
        errMsg.toLowerCase().includes("unauthorized") ||
        errMsg.includes("401") ||
        errMsg.toLowerCase().includes("token");
      const msg = isAuthError
        ? "SteamGridDB API 验证失败，请前往左下角“设置 -> 同步偏好”填写有效的 API Key"
        : errMsg || "SteamGridDB 搜索失败";
      this.showToast(msg, "error");
      this.updateNetworkStatus("offline", msg);
    } finally {
      this.state.sgdbSearching = false;
      this.renderSteamGridDBSearchResults();
    }
  },

  renderRawgSearchResults() {
    if (!this.dom.rawgSearchResults) {
      return;
    }
    if (this.state.rawgSearching) {
      this.dom.rawgSearchResults.innerHTML =
        '<div class="rawg-search-empty">正在从 RAWG 搜索候选项...</div>';
      return;
    }

    const query = this.dom.rawgSearchInput?.value.trim() || "";
    const results = this.state.rawgSearchResults || [];
    if (!results.length) {
      this.dom.rawgSearchResults.innerHTML = `<div class="rawg-search-empty">${query ? "未找到匹配结果，请尝试更完整的游戏名。" : "输入游戏名后即可从 RAWG 搜索候选项。"}</div>`;
      return;
    }

    this.dom.rawgSearchResults.innerHTML = results
      .map((item) => {
        const coverOptions =
          Array.isArray(item.coverOptions) && item.coverOptions.length
            ? item.coverOptions.filter(Boolean)
            : item.coverPath
              ? [item.coverPath]
              : [];
        const previewCover = coverOptions[0] || item.coverPath || "";
        const fallback = this.escapeHtmlAttribute(
          this.renderCoverPlaceholder("rawg-result-cover-placeholder"),
        );
        const cover = previewCover
          ? `<img src="${this.escapeHtmlAttribute(this.toCoverSrc(previewCover))}" alt="${this.escapeHtmlAttribute(item.name)}" onerror="this.outerHTML='${fallback}';window.App?.refreshIcons()">`
          : this.renderCoverPlaceholder("rawg-result-cover-placeholder");
        const optionButtons = coverOptions
          .slice(0, 4)
          .map(
            (coverPath, index) => `
          <button
            type="button"
            class="rawg-cover-option"
            data-action="apply-rawg-result"
            data-rawg-id="${this.escapeHtmlAttribute(item.id)}"
            data-cover-path="${this.escapeHtmlAttribute(coverPath)}"
            title="使用候选封面 ${index + 1}"
            ${this.state.rawgApplying ? "disabled" : ""}
          >
            <img src="${this.escapeHtmlAttribute(this.toCoverSrc(coverPath))}" alt="${this.escapeHtmlAttribute(item.name)} 候选封面 ${index + 1}">
          </button>
        `,
          )
          .join("");
        return `
        <article class="rawg-result-card">
          <div class="rawg-result-cover">${cover}</div>
          <div class="rawg-result-body">
            <div class="rawg-result-title-row">
              <div>
                <div class="rawg-result-title">${this.escapeHtml(item.name)}</div>
                <div class="rawg-result-meta">发售：${this.escapeHtml(item.released || "未知")} · RAWG 评分：${this.escapeHtml(this.formatRating(item.rating, 5))}</div>
                <div class="rawg-result-meta">Slug：${this.escapeHtml(item.slug || "未提供")} · Metacritic：${this.escapeHtml(item.metacritic ? String(item.metacritic) : "未提供")}</div>
              </div>
              <button type="button" class="btn btn-primary btn-sm" data-action="apply-rawg-result" data-rawg-id="${this.escapeHtmlAttribute(item.id)}" data-cover-path="${this.escapeHtmlAttribute(previewCover)}" ${this.state.rawgApplying ? "disabled" : ""}>${this.state.rawgApplying ? "应用中..." : "应用到当前游戏"}</button>
            </div>
            ${optionButtons ? `<div class="rawg-cover-options">${optionButtons}</div>` : ""}
          </div>
        </article>
      `;
      })
      .join("");
    this.refreshIcons();
  },

  renderSteamGridDBSearchResults() {
    if (!this.dom.sgdbSearchResults) {
      return;
    }
    if (this.state.sgdbSearching) {
      this.dom.sgdbSearchResults.innerHTML =
        '<div class="rawg-search-empty">正在从 SteamGridDB 搜索封面候选项...</div>';
      return;
    }

    const query = this.dom.sgdbSearchInput?.value.trim() || "";
    const results = this.state.sgdbSearchResults || [];
    if (!results.length) {
      this.dom.sgdbSearchResults.innerHTML = `<div class="rawg-search-empty">${query ? "未找到可用竖版封面，请尝试更完整的英文名。" : "输入游戏名后即可从 SteamGridDB 搜索竖版封面。"}</div>`;
      return;
    }

    this.dom.sgdbSearchResults.innerHTML = results
      .map((item) => {
        const coverOptions =
          Array.isArray(item.coverOptions) && item.coverOptions.length
            ? item.coverOptions.filter(Boolean)
            : item.coverPath
              ? [item.coverPath]
              : [];
        const previewCover = coverOptions[0] || item.coverPath || "";
        const fallback = this.escapeHtmlAttribute(
          this.renderCoverPlaceholder("rawg-result-cover-placeholder"),
        );
        const cover = previewCover
          ? `<img src="${this.escapeHtmlAttribute(this.toCoverSrc(previewCover))}" alt="${this.escapeHtmlAttribute(item.name)}" onerror="this.outerHTML='${fallback}';window.App?.refreshIcons()">`
          : this.renderCoverPlaceholder("rawg-result-cover-placeholder");
        const optionButtons = coverOptions
          .slice(0, 4)
          .map(
            (coverPath, index) => `
          <button
            type="button"
            class="rawg-cover-option"
            data-action="apply-sgdb-cover"
            data-cover-path="${this.escapeHtmlAttribute(coverPath)}"
            title="使用候选封面 ${index + 1}"
          >
            <img src="${this.escapeHtmlAttribute(this.toCoverSrc(coverPath))}" alt="${this.escapeHtmlAttribute(item.name)} 候选封面 ${index + 1}">
          </button>
        `,
          )
          .join("");
        return `
        <article class="rawg-result-card">
          <div class="rawg-result-cover">${cover}</div>
          <div class="rawg-result-body">
            <div class="rawg-result-title-row">
              <div>
                <div class="rawg-result-title">${this.escapeHtml(item.name)}</div>
                <div class="rawg-result-meta">SteamGridDB ID：${this.escapeHtml(String(item.id || ""))} · 已验证：${item.verified ? "是" : "否"}</div>
                <div class="rawg-result-meta">类型：${this.escapeHtml((item.types || []).join(", ") || "未提供")} · 候选数：${this.escapeHtml(String(coverOptions.length || 0))}</div>
              </div>
              <button type="button" class="btn btn-primary btn-sm" data-action="apply-sgdb-cover" data-cover-path="${this.escapeHtmlAttribute(previewCover)}">${previewCover ? "应用封面" : "无可用封面"}</button>
            </div>
            ${optionButtons ? `<div class="rawg-cover-options">${optionButtons}</div>` : ""}
          </div>
        </article>
      `;
      })
      .join("");
    this.refreshIcons();
  },

  handleRawgSearchResultClick(event) {
    const button = event.target.closest('[data-action="apply-rawg-result"]');
    if (!button?.dataset.rawgId) {
      return;
    }
    this.applyRawgSearchResult(
      Number(button.dataset.rawgId),
      button.dataset.coverPath || "",
    );
  },

  handleSteamGridDBSearchResultClick(event) {
    const button = event.target.closest('[data-action="apply-sgdb-cover"]');
    if (!button?.dataset.coverPath) {
      return;
    }
    this.applySteamGridDBCover(button.dataset.coverPath || "");
  },

  async applyRawgSearchResult(rawgId, coverPathOverride = "") {
    if (!rawgId || this.state.rawgApplying) {
      return;
    }

    this.state.rawgApplying = true;
    this.renderRawgSearchResults();
    try {
      const metadata = await this.bridge.GetRAWGGame(rawgId);
      if (coverPathOverride) {
        metadata.coverPath = coverPathOverride;
      } else if (
        Array.isArray(metadata.coverOptions) &&
        metadata.coverOptions.length
      ) {
        metadata.coverPath = metadata.coverOptions[0];
      }
      if (this.state.rawgPickerContext === "detail") {
        await this.applyRawgMetadataToCurrentGame(metadata);
      } else {
        this.applyRawgMetadataToForm(metadata);
      }
      this.closeModal("rawg-picker-modal");
    } catch (error) {
      console.error(error);
      const errMsg = String(error?.message || error || "");
      const isAuthError =
        errMsg.toLowerCase().includes("apikey") ||
        errMsg.toLowerCase().includes("unauthorized") ||
        errMsg.includes("401") ||
        errMsg.toLowerCase().includes("key");
      const msg = isAuthError
        ? "RAWG API 读取被拒绝，请确认 API Key 是否有效。"
        : errMsg || "RAWG 详情获取失败";
      this.showToast(msg, "error");
      this.updateNetworkStatus("offline", msg);
    } finally {
      this.state.rawgApplying = false;
      this.renderRawgSearchResults();
    }
  },

  async applySteamGridDBCover(coverPath) {
    if (!coverPath) {
      return;
    }

    if (this.state.sgdbPickerContext === "detail") {
      const game = this.getState().games.find(
        (item) => item.id === this.state.currentGameId,
      );
      if (!game) {
        return;
      }
      await this.saveGamePatch(
        game.id,
        { coverPath },
        "已从 SteamGridDB 更新游戏封面",
        { skipSync: true },
      );
    } else {
      document.getElementById("game-cover-path").value = coverPath;
      await this.updateCoverPreview(coverPath);
      this.showToast("已应用 SteamGridDB 封面，请保存游戏配置", "success");
      this.updateNetworkStatus("online", "已从 SteamGridDB 获取封面");
    }
    this.closeModal("sgdb-picker-modal");
  },

  applyRawgMetadataToForm(metadata) {
    this.state.gameFormMetadata = {
      ...this.extractMetadataFromGame(),
      ...metadata,
    };
    if (metadata.name) {
      document.getElementById("game-name").value = metadata.name;
    }
    this.dom.gameDescription.value = metadata.description || "";
    document.getElementById("game-cover-path").value = metadata.coverPath || "";
    this.updateCoverPreview(metadata.coverPath || "");
    this.renderGameTagSuggestions();
    this.showToast("已应用 RAWG 数据，请按需选择标签后保存", "success");
    this.updateNetworkStatus("online", "已从 RAWG 获取游戏资料");
  },

  async applyRawgMetadataToCurrentGame(metadata) {
    const game = this.getState().games.find(
      (item) => item.id === this.state.currentGameId,
    );
    if (!game) {
      return;
    }

    await this.saveGamePatch(
      game.id,
      {
        coverPath: metadata.coverPath || game.coverPath || "",
        description: metadata.description || "",
        released: metadata.released || "",
        rating: metadata.rating || 0,
        ratingTop: metadata.ratingTop || 0,
        metacritic: metadata.metacritic || 0,
        genres: metadata.genres || [],
        platforms: metadata.platforms || [],
        developers: metadata.developers || [],
        publishers: metadata.publishers || [],
        website: metadata.website || "",
        rawgId: metadata.rawgId || 0,
        rawgSlug: metadata.rawgSlug || "",
        rawgUrl: metadata.rawgUrl || "",
        rawgTags: metadata.rawgTags || [],
      },
      "已从 RAWG 更新游戏详情",
    );
  },

  async saveGamePatch(gameId, patch, successMessage = "", options = {}) {
    const game = this.getState().games.find((item) => item.id === gameId);
    if (!game) {
      return null;
    }

    const nextSnapshot = await this.bridge.SaveGame({
      ...game,
      ...patch,
    });
    this.applySnapshotSilently(nextSnapshot);

    const mergedSync = patch.sync || game.sync;
    const mergedAccount = patch.storageAccountId || game.storageAccountId;
    if (!options.skipSync && mergedSync?.enabled && mergedAccount) {
      void this.runSync(gameId, "", true);
    }

    if (!options.skipRender || options.updateCard) {
      this.updateSavedGameDOM(gameId);
    }
    if (
      (!options.skipRender || options.updateDetail) &&
      this.state.currentGameId === gameId &&
      this.dom.gameModal?.classList.contains("active")
    ) {
      this.renderGameDetail(gameId);
    }

    if (successMessage && !options.skipToast) {
      this.showToast(successMessage, "success");
      this.updateNetworkStatus("online", successMessage);
    }
    this.markLocalSnapshotEchoCandidate(this.state.snapshot);
    return this.getState().games.find((item) => item.id === gameId) || null;
  },

  handleGameTagSuggestionClick(event) {
    const button = event.target.closest('[data-action="toggle-form-tag"]');
    if (!button?.dataset.tag) {
      return;
    }
    this.toggleCurrentFormTag(button.dataset.tag);
  },

  handleGameDetailContentClick(event) {
    const button = event.target.closest('[data-action="toggle-rawg-tag"]');
    if (!button?.dataset.tag) {
      return;
    }
    this.toggleCurrentGameTag(button.dataset.tag);
  },

  currentFormTags() {
    return Array.from(
      new Set(
        document
          .getElementById("game-tags")
          .value.split(",")
          .map((item) => item.trim())
          .filter(Boolean),
      ),
    );
  },

  setCurrentFormTags(tags) {
    const unique = Array.from(
      new Set((tags || []).map((item) => item.trim()).filter(Boolean)),
    );
    document.getElementById("game-tags").value = unique.join(", ");
    this.renderFormTagsDisplay();
    this.renderGameTagSuggestions();
  },

  renderFormTagsDisplay() {
    if (!this.dom.gameTagsDisplay) return;
    const tags = this.currentFormTags();
    const rawgTags = new Set(this.state.gameFormMetadata?.rawgTags || []);
    const nextTags = new Set(tags);

    Array.from(
      this.dom.gameTagsDisplay.querySelectorAll("[data-form-tag]"),
    ).forEach((element) => {
      if (nextTags.has(element.dataset.formTag || "")) {
        return;
      }
      element.classList.add("is-removing");
      window.setTimeout(() => element.remove(), 160);
    });

    tags.forEach((tag) => {
      const selector = `[data-form-tag="${this.escapeCssValue(tag)}"]`;
      const existing = this.dom.gameTagsDisplay.querySelector(selector);
      const cls = rawgTags.has(tag) ? "is-selected" : "is-custom";

      if (existing) {
        existing.className = `tag tag-button form-tag-chip ${cls}`;
        return;
      }

      const button = document.createElement("button");
      button.type = "button";
      button.className = `tag tag-button form-tag-chip ${cls} is-entering`;
      button.dataset.action = "remove-form-tag";
      button.dataset.tag = tag;
      button.dataset.formTag = tag;
      button.innerHTML = `${this.escapeHtml(tag)} <span class="form-tag-remove-mark">×</span>`;
      this.dom.gameTagsDisplay.appendChild(button);
      window.setTimeout(() => button.classList.remove("is-entering"), 220);
    });
  },

  renderGameTagSuggestions() {
    if (!this.dom.gameTagSuggestionsGroup || !this.dom.gameTagSuggestions) {
      return;
    }
    const rawgSuggestions = this.state.gameFormMetadata?.rawgTags || [];
    const preferenceTags = (this.getState().preferences?.tagOrder || [])
      .map((tag) => String(tag || "").trim())
      .filter(Boolean);
    const existingTags = Array.from(
      new Set(
        preferenceTags.concat(
          (this.getState().games || []).flatMap((game) => game.tags || []),
        )
          .map((tag) => String(tag || "").trim())
          .filter(Boolean),
      ),
    ).filter(
      (tag) =>
        !rawgSuggestions.includes(tag) && !RESERVED_PLATFORM_TAGS.has(tag),
    );
    const sections = [];

    if (rawgSuggestions.length) {
      sections.push(`
        <div class="game-tag-section">
          <div class="field-help">RAWG 推荐标签</div>
          <div class="tag-suggestions">
            ${this.renderInteractiveTagButtons(
              rawgSuggestions,
              this.currentFormTags(),
              "toggle-form-tag",
            )}
          </div>
        </div>
      `);
    }

    if (existingTags.length) {
      sections.push(`
        <div class="game-tag-section">
          <div class="field-help">已有标签卡片</div>
          <div class="tag-suggestions">
            ${this.renderInteractiveTagButtons(
              existingTags,
              this.currentFormTags(),
              "toggle-form-tag",
            )}
          </div>
        </div>
      `);
    }

    if (!sections.length) {
      this.dom.gameTagSuggestionsGroup.style.display = "none";
      this.dom.gameTagSuggestions.innerHTML = "";
      return;
    }

    this.dom.gameTagSuggestionsGroup.style.display = "block";
    this.dom.gameTagSuggestions.innerHTML = sections.join("");
    this.refreshIcons();
  },

  toggleCurrentFormTag(tag) {
    const currentTags = this.currentFormTags();
    const selected = currentTags.includes(tag);
    const nextTags = selected
      ? currentTags.filter((item) => item !== tag)
      : currentTags.concat(tag);
    this.setCurrentFormTags(nextTags);
  },

  async toggleCurrentGameTag(tag) {
    const game = this.getState().games.find(
      (item) => item.id === this.state.currentGameId,
    );
    if (!game) {
      return;
    }

    const currentTags = game.tags || [];
    const selected = currentTags.includes(tag);
    const nextTags = selected
      ? currentTags.filter((item) => item !== tag)
      : currentTags.concat(tag);
    try {
      await this.saveGamePatch(game.id, { tags: nextTags });
    } catch (error) {
      console.error(error);
      this.showToast(error.message || "更新游戏标签失败", "error");
    }
  },

  async pickDefaultPreferencePath(field, successMessage) {
    const preferences = this.getState().preferences || {};
    const currentDir = preferences[field] || "";
    try {
      const path = await this.bridge.PickFolder(currentDir);
      if (!path) {
        return;
      }
      this.applySnapshot(
        await this.bridge.SavePreferences(
          this.buildPreferencesPayload({
            [field]: path,
          }),
        ),
        { renderSettings: true },
      );
      this.showToast(successMessage, "success");
      this.updateNetworkStatus("online", successMessage);
    } catch (error) {
      console.error(error);
      this.showToast(error.message || "保存默认路径失败", "error");
      this.updateNetworkStatus("offline", error.message || "保存默认路径失败");
    }
  },

  async deleteGame(gameId) {
    const game = this.getState().games.find((item) => item.id === gameId);
    if (!game) {
      return;
    }
    const confirmed = await this.showConfirm(
      `确定要删除游戏「${game.name}」吗？\n警告：此操作不可逆，会同时删除云端存档数据。`,
      "danger",
    );
    if (!confirmed) {
      return;
    }
    if (this.state.pendingDeletedGameIds.has(gameId)) {
      this.showToast("该游戏已在删除队列中", "warning");
      return;
    }

    this.rememberPendingDeletedGame(game);
    if (this.state.currentGameId === gameId) {
      this.state.currentGameId = "";
      this.closeModal("game-modal");
    }
    this.applyOptimisticGameDelete(gameId);

    try {
      await this.bridge.RequestDeleteGame(gameId);
      this.showToast("已加入删除队列", "success");
      this.updateNetworkStatus("syncing", "已加入删除队列");
    } catch (error) {
      console.error(error);
      this.restorePendingDeletedGame(gameId);
      this.showToast(error.message || "删除游戏失败", "error");
      this.updateNetworkStatus("offline", error.message || "删除游戏失败");
    }
  },

  async deleteAccount(accountId) {
    const account = this.getState().accounts.find(
      (item) => item.id === accountId,
    );
    if (!account) {
      return;
    }
    if (
      !window.confirm(
        `确定要删除账号「${account.name}」吗？绑定到该账号的游戏会自动切换到其他可用账号。`,
      )
    ) {
      return;
    }
    try {
      this.applySnapshot(await this.bridge.DeleteAccount(accountId));
      this.showToast("Cloudflare 账号已删除", "success");
      this.updateNetworkStatus("online", "Cloudflare 账号已删除");
    } catch (error) {
      console.error(error);
      this.showToast(error.message || "删除账号失败", "error");
      this.updateNetworkStatus("offline", error.message || "删除账号失败");
    }
  },

  async verifyAccount(accountId) {
    if (!accountId || this.state.verifyingAccountId) {
      return;
    }

    this.state.verifyingAccountId = accountId;
    this.renderAccounts();
    try {
      this.updateNetworkStatus("syncing", "正在验证 Cloudflare 账号...");
      this.applySnapshot(await this.bridge.VerifyAccount(accountId));
      this.state.verifyingAccountId = "";
      this.renderAccounts();

      const account = this.getState().accounts.find(
        (item) => item.id === accountId,
      );
      if (account?.lastError) {
        this.showToast(account.lastError, "error");
        this.updateNetworkStatus("offline", "账号验证失败");
        return;
      }

      const successMessage = account.isPrimary
        ? "账号验证通过，已自动同步副账号"
        : "账号验证通过，已刷新 R2 用量";
      this.showToast(successMessage, "success");
      this.updateNetworkStatus("online", successMessage);
    } catch (error) {
      console.error(error);
      this.state.verifyingAccountId = "";
      this.renderAccounts();
      this.showToast(error.message || "验证账号失败", "error");
      this.updateNetworkStatus("offline", error.message || "验证账号失败");
    }
  },

  async restoreFromPrimary() {
    try {
      this.applySnapshot(await this.bridge.RestoreFromPrimary(""));
      this.showToast("已从主账号恢复账号目录和游戏映射", "success");
    } catch (error) {
      console.error(error);
      this.showToast(error.message || "恢复操作失败", "error");
      this.updateNetworkStatus("offline", error.message || "恢复操作失败");
    }
  },

  async startGameWithPreSync(gameId, conflictChoice = "") {
    const game = this.getState().games.find((item) => item.id === gameId);
    if (!game?.installPath) {
      this.showToast("未配置启动文件", "warning");
      return;
    }

    this.state.pendingLaunchGameId = gameId;
    try {
      this.updateNetworkStatus(
        "syncing",
        `正在检查 ${game.name} 的启动前存档状态...`,
      );
      const result = await this.bridge.PrepareGameLaunch(
        gameId,
        conflictChoice,
      );
      const snapshot = result?.snapshot;
      if (snapshot) {
        this.applySnapshot(snapshot, {
          renderSettings: this.state.page === "settings",
        });
      }

      if (result?.status === "needs_choice") {
        this.state.pendingConflictGameId = gameId;
        this.state.pendingConflictAction = "launch";
        this.state.pendingConflictMessage =
          result?.message || "检测到启动前存档冲突，请选择保留哪一侧。";
        document.getElementById("conflict-message").textContent =
          this.state.pendingConflictMessage;
        this.openModal("conflict-modal");
        this.showToast(this.state.pendingConflictMessage, "warning");
        this.updateNetworkStatus("offline", "检测到启动前存档冲突，等待处理");
        return;
      }

      if (result?.status === "failed") {
        throw new Error(result?.message || "启动前同步失败");
      }

      const prepMessage = result?.message || "启动前存档检查完成";
      this.showToast(prepMessage, "success");
      this.updateNetworkStatus("online", prepMessage);
      await this.launchPreparedGame(gameId);
    } catch (error) {
      console.error(error);
      const confirmed = await this.showBinaryConfirm(
        `${error?.message || "当前无法确认本地与云端存档谁更新。"}\n继续启动将跳过本次启动前同步，是否继续？`,
        {
          mode: "warning",
          confirmText: "继续启动",
          cancelText: "取消",
        },
      );
      if (confirmed) {
        await this.launchPreparedGame(gameId);
        return;
      }
      this.state.pendingLaunchGameId = "";
      this.updateNetworkStatus("offline", error?.message || "启动前同步失败");
    }
  },

  async launchPreparedGame(gameId) {
    const game = this.getState().games.find((item) => item.id === gameId);
    if (!game) {
      return;
    }
    this.showToast("准备启动游戏...", "rocket");
    this.state.runtimeStatus = this.state.runtimeStatus || {};
    this.state.runtimeStatus[game.id] = {
      text: "启动中",
      icon: "rocket",
      statusClass: "is-launching",
    };
    this.updateGameCardStatusDOM(game.id);
    this.state.pendingLaunchGameId = "";
    await this.bridge.LaunchAndMonitorGame(game.id);
  },

  async runSync(gameId, conflictChoice = "", silent = false) {
    if (silent) {
      this.beginSilentRuntimeStateUpdates();
    }
    try {
      const game = this.getState().games.find((item) => item.id === gameId);
      if (!game) {
        return;
      }
      this.updateNetworkStatus("syncing", `正在同步 ${game.name}...`);
      this.applySnapshotSilently(
        await this.bridge.RunSync({ gameId, conflictChoice }),
      );
      if (silent) {
        this.updateGameCardStatusDOM(gameId);
      } else {
        this.updateSavedGameDOM(gameId);
      }
      if (!silent && this.state.page === "activities") {
        this.renderActivities();
      }
      const updatedGame = this.getState().games.find(
        (item) => item.id === gameId,
      );
      if (updatedGame?.lastSync?.status === "conflict" && !conflictChoice) {
        this.state.pendingConflictGameId = gameId;
        this.state.pendingConflictMessage =
          updatedGame.lastSync.message || "检测到冲突，请选择保留哪一侧";
        document.getElementById("conflict-message").textContent =
          this.state.pendingConflictMessage;
        this.openModal("conflict-modal");
        this.showToast(
          updatedGame.lastSync.message || "检测到同步冲突",
          "warning",
        );
        this.updateNetworkStatus("offline", "检测到同步冲突，等待处理");
        return;
      }
      this.closeConflictModal();
      const message = updatedGame?.lastSync?.message || "同步完成";

      this.state.runtimeStatus = this.state.runtimeStatus || {};
      if (updatedGame?.lastSync?.status !== "failed") {
        this.state.runtimeStatus[gameId] = { text: "同步成功", icon: "check" };
      } else {
        delete this.state.runtimeStatus[gameId];
      }
      this.updateGameCardStatusDOM(gameId);
      if (
        this.state.currentGameId === gameId &&
        this.dom.gameModal?.classList.contains("active")
      ) {
        this.renderGameDetail(gameId);
      }

      setTimeout(() => {
        if (
          this.state.runtimeStatus &&
          this.state.runtimeStatus[gameId]?.icon === "check"
        ) {
          delete this.state.runtimeStatus[gameId];
          this.updateGameCardStatusDOM(gameId);
        }
      }, 3000);

      if (!silent) {
        this.showToast(
          message,
          updatedGame?.lastSync?.status === "failed" ? "error" : "success",
        );
      }
      this.updateNetworkStatus(
        updatedGame?.lastSync?.status === "failed" ? "offline" : "online",
        message,
      );
      this.markLocalSnapshotEchoCandidate(this.state.snapshot);
    } catch (error) {
      console.error(error);
      if (!silent) {
        this.showToast(error.message || "同步失败", "error");
      }
      this.updateNetworkStatus("offline", error.message || "同步失败");
    } finally {
      if (silent) {
        this.endSilentRuntimeStateUpdates();
      }
    }
  },

  async syncAllGames() {
    const { games, preferences } = this.getState();
    const enabledGames = games.filter((game) => game.sync?.enabled);
    if (!enabledGames.length) {
      this.showToast("没有可同步的游戏，请先添加游戏并启用同步", "warning");
      return;
    }

    for (const game of enabledGames) {
      const choice = this.resolveDefaultConflictChoice(game, preferences);
      await this.runSync(game.id, choice);
      const updatedGame = this.getState().games.find(
        (item) => item.id === game.id,
      );
      if (updatedGame?.lastSync?.status === "conflict" && !choice) {
        this.state.page = "all-games";
        this.state.filterTag = "";
        this.renderDataViews();
        return;
      }
    }

    this.showToast("已完成全部游戏同步", "success");
  },

  resolveDefaultConflictChoice(game, preferences) {
    const strategy = (
      game.sync?.conflictStrategy ||
      preferences.conflictPolicy ||
      "manual"
    ).toLowerCase();
    if (strategy === "local") {
      return "local";
    }
    if (strategy === "remote") {
      return "remote";
    }
    return "";
  },

  async resolveConflict(choice) {
    if (!this.state.pendingConflictGameId) {
      return;
    }
    const gameId = this.state.pendingConflictGameId;
    this.state.pendingConflictGameId = "";
    this.closeModal("conflict-modal");
    const action = this.state.pendingConflictAction || "sync";
    this.state.pendingConflictAction = "";
    if (action === "launch") {
      await this.startGameWithPreSync(gameId, choice);
      return;
    }
    await this.runSync(gameId, choice);
  },

  async openPath(path) {
    if (!path) {
      this.showToast("路径为空", "warning");
      return;
    }
    try {
      await this.bridge.OpenPath(path);
    } catch (error) {
      console.error(error);
      this.showToast(error.message || "打开路径失败", "error");
    }
  },

  async openDataDir() {
    if (!this.state.snapshot?.dataDir) {
      return;
    }
    await this.openPath(this.state.snapshot.dataDir);
  },

  async exportAppBackup() {
    try {
      await this.bridge.ExportAppBackup();
      this.showToast("软件配置已备份", "success");
    } catch (e) {
      this.showToast("备份失败: " + e, "error");
    }
  },

  async importAppBackup(isFirstLaunch = false) {
    if (!isFirstLaunch) {
      const confirmed = await this.showConfirm(
        "恢复备份将覆盖当前所有游戏、账号和偏好设置，确定继续吗？",
        "danger",
      );
      if (!confirmed) return;
    }

    try {
      await this.bridge.ImportAppBackup();
      this.showToast("备份已恢复，正在刷新...", "success");
      await this.refreshSnapshot("备份已恢复");
    } catch (e) {
      this.showToast("恢复失败: " + e, "error");
    }
  },

  async checkFirstLaunchRestore() {
    try {
      const isFirst = await this.bridge.IsFirstLaunch();
      if (!isFirst) return;

      const choice = await this.showWelcomeDialog();
      if (choice === "restore") {
        await this.importAppBackup(true);
      } else if (choice === "manual") {
        this.state.page = "accounts";
        this.renderDataViews();
        this.openAccountModal();
      }
    } catch (e) {
      // 首次启动检测失败不影响正常使用。
      console.warn("首次启动检测失败:", e);
    }
  },

  showWelcomeDialog() {
    return new Promise((resolve) => {
      const overlay = document.getElementById("welcome-dialog");
      const restoreBtn = document.getElementById("welcome-restore-btn");
      const manualBtn = document.getElementById("welcome-manual-btn");
      const skipBtn = document.getElementById("welcome-skip-btn");

      overlay.classList.add("active");
      this.refreshIcons();

      const cleanup = () => {
        overlay.classList.remove("active");
        restoreBtn.removeEventListener("click", onRestore);
        manualBtn.removeEventListener("click", onManual);
        skipBtn.removeEventListener("click", onSkip);
      };

      const onRestore = () => {
        cleanup();
        resolve("restore");
      };
      const onManual = () => {
        cleanup();
        resolve("manual");
      };
      const onSkip = () => {
        cleanup();
        resolve("skip");
      };

      restoreBtn.addEventListener("click", onRestore);
      manualBtn.addEventListener("click", onManual);
      skipBtn.addEventListener("click", onSkip);
    });
  },

  populateAccountSelect(selectedId = "") {
    if (!this.dom.gameAccountSelect) {
      return;
    }
    const accounts = this.getState().accounts || [];
    this.dom.gameAccountSelect.innerHTML = [
      '<option value="">自动选择第一个可用存档存储账号</option>',
    ]
      .concat(
        accounts.map(
          (account) =>
            `<option value="${account.id}">${this.escapeHtml(account.name)}${account.isPrimary ? " (主账号)" : " (副账号)"}</option>`,
        ),
      )
      .join("");
    this.dom.gameAccountSelect.value = selectedId || "";
    this.syncCustomSelect("game-account-id");
  },

  async updateCoverPreview(path) {
    if (!path) {
      this.dom.coverPreviewImage.style.display = "none";
      this.dom.coverPreviewImage.removeAttribute("src");
      this.dom.coverPreviewPlaceholder.style.display = "block";
      return;
    }
    const src = await this.resolveCoverSrc(path);
    if (!src) {
      this.dom.coverPreviewImage.style.display = "none";
      this.dom.coverPreviewImage.removeAttribute("src");
      this.dom.coverPreviewPlaceholder.style.display = "block";
      return;
    }
    this.dom.coverPreviewImage.src = src;
    this.dom.coverPreviewImage.style.display = "block";
    this.dom.coverPreviewPlaceholder.style.display = "none";
  },

  async toggleFavoriteGame(gameId) {
    const isFavorite = this.isFavoriteGame(gameId);
    let nextFavoriteGames;
    if (isFavorite) {
      nextFavoriteGames = this.state.favoriteGames.filter(
        (id) => id !== gameId,
      );
    } else {
      nextFavoriteGames = [gameId, ...this.state.favoriteGames];
    }
    if (this.sameStringSlices(nextFavoriteGames, this.state.favoriteGames)) {
      return;
    }
    await this.persistFavoriteGames(nextFavoriteGames, {
      success: isFavorite ? "已从常玩游戏移除" : "已添加到常玩游戏",
      online: "常玩游戏已同步",
      error: isFavorite ? "移出常玩失败" : "加入常玩失败",
      offline: "常玩游戏同步失败",
    });
  },

  isFavoriteGame(gameId) {
    return this.state.favoriteGames.includes(gameId);
  },

  async togglePinnedTag(tag) {
    const tagName = String(tag || "").trim();
    if (!tagName) {
      return;
    }
    const currentPinnedTags = this.getPinnedTags();
    const isPinned = currentPinnedTags.includes(tagName);
    const nextPinnedTags = isPinned
      ? currentPinnedTags.filter((item) => item !== tagName)
      : [tagName, ...currentPinnedTags];

    if (this.sameStringSlices(nextPinnedTags, currentPinnedTags)) {
      return;
    }

    const previousSnapshot = this.state.snapshot
      ? JSON.parse(JSON.stringify(this.state.snapshot))
      : null;
    const currentState = this.getState();
    if (this.state.snapshot?.state) {
      this.state.snapshot.state.preferences = {
        ...(currentState.preferences || {}),
        pinnedTags: nextPinnedTags,
      };
      this.markLocalSnapshotEchoCandidate();
    }
    this.renderPinnedTagsNav();
    this.renderTagsPage();
    this.refreshIcons();

    try {
      const snapshot = await this.bridge.SavePreferences(
        this.buildPreferencesPayload({
          pinnedTags: nextPinnedTags,
        }),
      );
      this.applySnapshot(snapshot, { renderSettings: true });
      this.showToast(isPinned ? "已取消固定标签" : "已固定到侧栏", "success");
      this.updateNetworkStatus("online", "固定标签已同步");
    } catch (error) {
      if (previousSnapshot) {
        this.applySnapshot(previousSnapshot, { renderSettings: true });
      }
      console.error(error);
      this.showToast(error.message || "固定标签同步失败", "error");
      this.updateNetworkStatus(
        "offline",
        error.message || "固定标签同步失败",
      );
    }
  },

  onSearchInput(event) {
    this.state.searchQuery = event.target.value.trim();
    this.dom.searchClear.style.display = this.state.searchQuery
      ? "flex"
      : "none";
    clearTimeout(this.state.searchDebounce);
    this.state.searchDebounce = window.setTimeout(
      () => this.renderDataViews(),
      150,
    );
  },

  clearSearch() {
    this.resetSearch();
    this.renderDataViews();
  },

  resetSearch() {
    clearTimeout(this.state.searchDebounce);
    this.state.searchQuery = "";
    this.dom.searchInput.value = "";
    this.dom.searchClear.style.display = "none";
  },

  filterGames(games) {
    const query = this.state.searchQuery.toLowerCase();
    if (!query) {
      return games;
    }
    return this.filterGamesBySearch(games, query);
  },

  filterGamesBySearch(games, query) {
    if (!query) {
      return games;
    }

    return games
      .filter((game) => {
        const name = (game.name || "").toLowerCase();
        const tags = (game.tags || []).join(" ").toLowerCase();
        const savePath = (game.savePath || "").toLowerCase();
        const installPath = (game.installPath || "").toLowerCase();
        const target = `${name} ${tags} ${savePath} ${installPath}`;

        if (target.includes(query)) {
          return true;
        }

        const keywords = query.split(/\s+/).filter(Boolean);
        if (keywords.length > 1) {
          return keywords.every((keyword) => target.includes(keyword));
        }

        return this.fuzzyMatch(target, query);
      })
      .sort((a, b) => {
        const scoreA = this.getMatchScore(a, query);
        const scoreB = this.getMatchScore(b, query);
        return scoreB - scoreA;
      });
  },

  fuzzyMatch(text, pattern) {
    let patternIndex = 0;
    for (let i = 0; i < text.length && patternIndex < pattern.length; i += 1) {
      if (text[i] === pattern[patternIndex]) {
        patternIndex += 1;
      }
    }
    return patternIndex === pattern.length;
  },

  getMatchScore(game, query) {
    const name = (game.name || "").toLowerCase();
    const tags = (game.tags || []).join(" ").toLowerCase();
    if (name === query) return 100;
    if (name.startsWith(query)) return 80;
    if (name.includes(query)) return 60;
    if (tags.includes(query)) return 40;
    if (this.fuzzyMatch(name, query)) return 20;
    return 0;
  },

  filterTags(tags) {
    const query = this.state.searchQuery.toLowerCase();
    if (!query) {
      return tags;
    }
    return tags.filter((tag) => tag.name.toLowerCase().includes(query));
  },

  filterAccounts(accounts) {
    const query = this.state.searchQuery.toLowerCase();
    if (!query) {
      return accounts;
    }
    return accounts.filter((account) => {
      const target = [
        account.name,
        account.accountId,
        account.d1DatabaseId,
        account.r2Bucket,
      ]
        .join(" ")
        .toLowerCase();
      return target.includes(query);
    });
  },

  filterActivities(activities) {
    const query = this.state.searchQuery.toLowerCase();
    if (!query) {
      return activities;
    }
    return activities.filter((activity) => {
      const target = [activity.gameName, activity.message, activity.status]
        .join(" ")
        .toLowerCase();
      return target.includes(query);
    });
  },

  collectTagSummaries(games) {
    const map = new Map();
    games.forEach((game) => {
      (game.tags || []).forEach((tag) => {
        if (!map.has(tag)) {
          map.set(tag, { name: tag, count: 0, syncedCount: 0 });
        }
        const current = map.get(tag);
        current.count += 1;
        if (game.lastSync?.syncedAt) {
          current.syncedCount += 1;
        }
      });

      // Auto-collect Platform tags
      const platformTag = game.isSteam ? "Steam 游戏" : "第三方游戏";
      if (!map.has(platformTag)) {
        map.set(platformTag, {
          name: platformTag,
          count: 0,
          syncedCount: 0,
          isPlatform: true,
        });
      }
      const pCurrent = map.get(platformTag);
      pCurrent.count += 1;
      if (game.lastSync?.syncedAt) {
        pCurrent.syncedCount += 1;
      }
    });

    const tagOrder = this.getState().preferences?.tagOrder || [];
    const orderMap = new Map();
    tagOrder.forEach((tag, index) => orderMap.set(tag, index));

    return Array.from(map.values()).sort((a, b) => {
      const aIndex = orderMap.has(a.name) ? orderMap.get(a.name) : Infinity;
      const bIndex = orderMap.has(b.name) ? orderMap.get(b.name) : Infinity;
      if (aIndex !== bIndex) return aIndex - bIndex;
      return b.count - a.count || a.name.localeCompare(b.name, "zh-CN");
    });
  },

  getState() {
    return (
      this.state.snapshot?.state || {
        device: {},
        accounts: [],
        games: [],
        preferences: {},
        activities: [],
      }
    );
  },

  primaryPathAction(game) {
    if (game.installPath) {
      return { label: "打开启动文件", path: game.installPath };
    }
    if (game.savePath) {
      return { label: "打开存档目录", path: game.savePath };
    }
    return { label: "未配置路径", path: "" };
  },

  syncStatusText(status, enabled) {
    switch ((status || "").toLowerCase()) {
      case "success":
        return "同步成功";
      case "conflict":
      case "warning":
        return "冲突待处理";
      case "failed":
      case "error":
        return "同步失败";
      default:
        return enabled ? "等待同步" : "仅本地";
    }
  },

  conflictPolicyText(policy) {
    switch ((policy || "manual").toLowerCase()) {
      case "local":
        return "优先本地";
      case "remote":
        return "优先云端";
      default:
        return "手动选择";
    }
  },

  renderCover(identifier, coverPath = "") {
    if (!identifier && !coverPath) {
      return this.renderCoverPlaceholder();
    }
    const fallback = this.escapeHtmlAttribute(this.renderCoverPlaceholder());
    const version = this.escapeHtmlAttribute(coverPath || identifier || "");
    if (!identifier && this.isDirectCoverSrc(coverPath)) {
      const src = this.toCoverSrc(coverPath);
      return `<img src="${this.escapeHtmlAttribute(src)}" alt="cover" onerror="this.outerHTML='${fallback}';window.App?.refreshIcons()">`;
    }
    const ref = identifier || coverPath;
    return `<img src="" data-cover-path="${this.escapeHtmlAttribute(ref)}" data-cover-version="${version}" data-cover-fallback="${fallback}" alt="cover">`;
  },

  toCoverSrc(path) {
    if (!path) {
      return "";
    }
    if (/^(https?:|data:)/i.test(path)) {
      return path;
    }
    return "";
  },

  isDirectCoverSrc(path) {
    return /^(https?:|data:)/i.test(path || "");
  },

  async resolveCoverSrc(path) {
    if (!path) {
      return "";
    }
    if (this.isDirectCoverSrc(path)) {
      return this.toCoverSrc(path);
    }
    if (this.coverSourceCache.has(path)) {
      return this.coverSourceCache.get(path);
    }
    if (this.coverSourceInflight.has(path)) {
      return this.coverSourceInflight.get(path);
    }
    const request = Promise.resolve(this.bridge.ResolveCoverSource(path))
      .then((src) => {
        const normalized = src || "";
        this.coverSourceCache.set(path, normalized);
        this.coverSourceInflight.delete(path);
        return normalized;
      })
      .catch((error) => {
        this.coverSourceInflight.delete(path);
        throw error;
      });
    this.coverSourceInflight.set(path, request);
    return request;
  },

  hydrateCoverImages(root = document) {
    const images = root.querySelectorAll("img[data-cover-path]");
    images.forEach((img) => {
      const path = img.dataset.coverPath || "";
      if (!path || img.dataset.coverHydrated === "true") {
        return;
      }
      img.dataset.coverHydrated = "pending";
      this.resolveCoverSrc(path)
        .then((src) => {
          if (!src) {
            throw new Error("empty cover src");
          }
          img.src = src;
          img.dataset.coverHydrated = "true";
        })
        .catch(() => {
          const retries = Number.parseInt(img.dataset.coverRetries || "0", 10) || 0;
          if (retries < 4) {
            img.dataset.coverRetries = String(retries + 1);
            img.dataset.coverHydrated = "retry";
            window.setTimeout(() => {
              if (!img.isConnected) {
                return;
              }
              img.dataset.coverHydrated = "";
              this.hydrateCoverImages(img.parentElement || root);
            }, 800 + retries * 400);
            return;
          }
          const fallback = img.dataset.coverFallback || "";
          if (fallback) {
            img.outerHTML = fallback;
            this.refreshIcons();
          } else {
            img.remove();
          }
        });
    });
  },

  renderInteractiveTagButtons(tags, selectedTags = [], action = "") {
    const uniqueTags = Array.from(
      new Set(
        (tags || []).map((item) => String(item || "").trim()).filter(Boolean),
      ),
    );
    if (!uniqueTags.length) {
      return '<span class="detail-info-value">暂无</span>';
    }

    const selected = new Set(
      (selectedTags || []).map((item) => String(item || "").trim()),
    );
    return uniqueTags
      .map(
        (tag) => `
      <button type="button" class="tag tag-button ${selected.has(tag) ? "is-selected" : ""}" data-action="${this.escapeHtmlAttribute(action)}" data-tag="${this.escapeHtmlAttribute(tag)}">${this.escapeHtml(tag)}</button>
    `,
      )
      .join("");
  },

  formatList(values, fallback = "未提供") {
    const items = (values || [])
      .map((item) => String(item || "").trim())
      .filter(Boolean);
    return items.length ? items.join(" / ") : fallback;
  },

  formatRating(value, total = 5) {
    const score = Number(value || 0);
    if (!score) {
      return "未提供";
    }
    return `${score.toFixed(score >= 10 ? 0 : 1)} / ${total || 5}`;
  },

  formatMultilineText(text) {
    return this.escapeHtml(text || "").replace(/\n/g, "<br>");
  },

  updateNetworkStatus(mode, text = "") {
    const duration = mode === "syncing" || mode === "checking" ? 8000 : 4000;
    this.state.globalNetworkState = this.state.globalNetworkState || {
      catalog: { status: "checking", message: "检测中" },
      foreground: { status: "", message: "", expiresAt: 0 },
    };
    this.state.globalNetworkState.foreground = {
      status: mode,
      message: text,
      expiresAt: Date.now() + duration,
    };
    if (this.state.networkStatusTimer) {
      window.clearTimeout(this.state.networkStatusTimer);
      this.state.networkStatusTimer = null;
    }
    this.state.networkStatusTimer = window.setTimeout(() => {
      this.state.networkStatusTimer = null;
      this.renderNetworkStatus();
    }, duration + 20);
    this.renderNetworkStatus();
  },

  setCatalogNetworkState(status, message = "") {
    this.state.globalNetworkState = this.state.globalNetworkState || {
      catalog: { status: "checking", message: "检测中" },
      foreground: { status: "", message: "", expiresAt: 0 },
    };
    const fallbackText = {
      checking: "检测中",
      online: "已连接",
      offline: "离线",
      syncing: "同步中",
      retrying: "重试中",
      queued: "已排队",
      succeeded: "已连接",
    };
    const mappedStatus =
      status === "queued" || status === "retrying"
        ? "syncing"
        : status === "succeeded"
          ? "online"
          : status;
    if (mappedStatus === "offline") {
      this.state.globalNetworkState.foreground = {
        status: "",
        message: "",
        expiresAt: 0,
      };
      if (this.state.networkStatusTimer) {
        window.clearTimeout(this.state.networkStatusTimer);
        this.state.networkStatusTimer = null;
      }
    }
    this.state.globalNetworkState.catalog = {
      status: mappedStatus || "checking",
      message:
        message ||
        fallbackText[status] ||
        fallbackText[mappedStatus] ||
        "检测中",
    };
    this.renderNetworkStatus();
  },

  renderNetworkStatus() {
    const fallbackText = {
      checking: "检测中",
      online: "已连接",
      offline: "离线",
      syncing: "同步中",
    };
    const globalState = this.state.globalNetworkState || {};
    const foreground = globalState.foreground || {};
    const catalog = globalState.catalog || {
      status: "checking",
      message: "检测中",
    };
    const foregroundActive =
      foreground.status && Date.now() < (foreground.expiresAt || 0);
    const useForeground = catalog.status !== "offline" && foregroundActive;
    const active = useForeground ? foreground : catalog;

    this.dom.networkStatus.classList.remove("online", "offline", "syncing");
    if (
      active.status === "online" ||
      active.status === "offline" ||
      active.status === "syncing"
    ) {
      this.dom.networkStatus.classList.add(active.status);
    }
    const content = active.message || fallbackText[active.status] || "检测中";
    this.dom.networkStatusText.textContent = content;
    this.dom.networkStatus.title = content;
  },

  showToast(message, type = "success") {
    const toast = document.createElement("div");
    const closeButton = document.createElement("button");
    closeButton.type = "button";
    closeButton.className = "btn btn-ghost btn-sm toast-close-btn";
    closeButton.setAttribute("aria-label", "关闭提示");
    closeButton.innerHTML = this.icon("x", "toast-close-icon");
    closeButton.addEventListener("click", () => toast.remove());

    let typeIcon = "info";
    if (type === "success") typeIcon = "check-circle";
    if (type === "error") typeIcon = "x-circle";
    if (type === "warning") typeIcon = "triangle-alert";
    if (type === "rocket") typeIcon = "rocket";

    let actualType = type;
    if (type === "rocket") actualType = "success"; // for css styling bg color

    toast.className = `toast ${actualType}`;
    toast.innerHTML = `<div style="display:flex; align-items:center; gap:8px;">${this.icon(typeIcon, "toast-type-icon")} <span>${this.escapeHtml(message)}</span></div>`;
    toast.appendChild(closeButton);
    this.dom.toastContainer.appendChild(toast);
    this.refreshIcons();
    window.setTimeout(() => toast.remove(), 3200);
  },

  formatTime(value) {
    if (!value) {
      return "从未";
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
      return "未知时间";
    }
    return date.toLocaleString("zh-CN", {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
    });
  },

  formatPlayTime(value) {
    const minutes = Math.max(Number(value) || 0, 0);
    if (minutes < 1) {
      return "不足 1 分钟";
    }
    if (minutes < 60) {
      return `${Math.round(minutes)} 分钟`;
    }
    const totalHours = minutes / 60;
    if (totalHours < 10) {
      return `${totalHours.toFixed(1)} 小时`;
    }
    return `${Math.round(totalHours)} 小时`;
  },

  formatRelativeTime(value) {
    if (!value) {
      return "从未";
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
      return "未知时间";
    }
    const diff = Date.now() - date.getTime();
    const minute = 60 * 1000;
    const hour = 60 * minute;
    const day = 24 * hour;
    if (diff < minute) return "刚刚";
    if (diff < hour) return `${Math.floor(diff / minute)} 分钟前`;
    if (diff < day) return `${Math.floor(diff / hour)} 小时前`;
    if (diff < 30 * day) return `${Math.floor(diff / day)} 天前`;
    return date.toLocaleDateString("zh-CN");
  },

  formatBytes(bytes) {
    if (!bytes) {
      return "0 B";
    }
    const units = ["B", "KB", "MB", "GB", "TB"];
    let value = bytes;
    let index = 0;
    while (value >= 1024 && index < units.length - 1) {
      value /= 1024;
      index += 1;
    }
    const fixed = value >= 100 || index === 0 ? 0 : 1;
    return `${value.toFixed(fixed)} ${units[index]}`;
  },

  formatUsageQuota(usedBytes, totalBytes) {
    return `${this.formatBytes(usedBytes || 0)} / ${this.formatBytes(totalBytes || 0)}`;
  },

  accountNameById(accountId) {
    return (
      this.getState().accounts.find((item) => item.id === accountId)?.name || ""
    );
  },

  backupLocationText(backup) {
    const accountName = this.accountNameById(backup?.storageAccountId || "");
    if (backup?.localExists && accountName) {
      return `本地 + 云端 · ${accountName}`;
    }
    if (accountName) {
      return `云端待同步 · ${accountName}`;
    }
    return "仅本地";
  },

  backupStatusMeta(backup) {
    switch ((backup?.status || "").toLowerCase()) {
      case "pending_upload":
        return { badgeClass: "warning", badgeText: "待上传" };
      case "pending_delete":
        return { badgeClass: "warning", badgeText: "删除中" };
      case "upload_failed":
        return { badgeClass: "warning", badgeText: "上传失败" };
      case "delete_failed":
        return { badgeClass: "warning", badgeText: "删除失败" };
      default:
        return null;
    }
  },

  normalizeBackupListResult(result) {
    if (Array.isArray(result)) {
      return {
        backups: result.filter(
          (backup) =>
            !this.isPendingDeletedBackup(this.state.currentGameId, backup?.filename),
        ),
        partial: false,
        message: "",
        failedAccounts: [],
      };
    }
    return {
      backups: (Array.isArray(result?.backups) ? result.backups : []).filter(
        (backup) =>
          !this.isPendingDeletedBackup(this.state.currentGameId, backup?.filename),
      ),
      partial: Boolean(result?.partial),
      message: result?.message || "",
      failedAccounts: Array.isArray(result?.failedAccounts)
        ? result.failedAccounts
        : [],
    };
  },

  async syncOpenBackupModal(gameId = this.state.currentGameId) {
    if (
      !gameId ||
      !document.getElementById("backup-modal")?.classList.contains("active")
    ) {
      return;
    }
    try {
      const backupResult = this.normalizeBackupListResult(
        await this.bridge.GetGameBackups(gameId),
      );
      this.renderBackupList(backupResult);
    } catch (error) {
      console.error("同步备份弹窗失败:", error);
    }
  },

  statusBadgeClass(status) {
    switch ((status || "").toLowerCase()) {
      case "success":
        return "success";
      case "conflict":
      case "warning":
        return "warning";
      case "failed":
      case "error":
        return "error";
      default:
        return "";
    }
  },

  basename(path) {
    if (!path) {
      return "";
    }
    return path.split(/[\\/]/).filter(Boolean).pop() || path;
  },

  filenameWithoutExt(path) {
    const filename = this.basename(path);
    return filename.replace(/\.[^.]+$/, "");
  },

  autofillGameNameFromInstallPath(path) {
    const nameInput = document.getElementById("game-name");
    if (!nameInput || nameInput.value.trim()) {
      return;
    }
    const guessedName = this.filenameWithoutExt(path).trim();
    if (guessedName) {
      nameInput.value = guessedName;
    }
  },

  escapeHtml(text) {
    return String(text ?? "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#39;");
  },

  escapeHtmlAttribute(text) {
    return this.escapeHtml(text).replace(/`/g, "&#96;");
  },

  async launchGameFromModal() {
    const gameId = this.state.currentGameId;
    if (!gameId) return;
    await this.startGameWithPreSync(gameId);
  },

  async openBackupModal(gameId) {
    if (!gameId) return;
    this.state.currentGameId = gameId;
    const game = this.getState().games.find((g) => g.id === gameId);
    document.getElementById("backup-modal-title").textContent = game
      ? `${game.name} - 存档备份`
      : "存档备份";

    const container = document.getElementById("backup-list-container");
    const modalElement = document.querySelector("#backup-modal .modal");

    // 先重置内联样式，避免上次动画残留。
    modalElement.style.transition = "transform 0.15s ease, opacity 0.15s ease";
    modalElement.style.height = "auto";

    // 1. 先显示“正在加载存档”的占位态。
    container.innerHTML = `
      <div class="backup-empty-state">
        <div class="backup-empty-icon animate-pulse">${this.icon("archive")}</div>
        <div>正在加载存档记录...</div>
      </div>
    `;
    this.refreshIcons();

    // 立即打开弹窗展示加载态。
    this.openModal("backup-modal");

    try {
      // 2. 从云端或本地拉取数据。
      const backupResult = this.normalizeBackupListResult(
        await this.bridge.GetGameBackups(gameId),
      );
      const backups = backupResult.backups;
      // 3. 准备执行平滑高度动画。
      const oldHeight = modalElement.offsetHeight;
      modalElement.style.height = oldHeight + "px";

      // 动画期间隐藏内部滚动条，避免闪动。
      const modalBody = modalElement.querySelector(".modal-body");
      if (modalBody) modalBody.style.overflowY = "hidden";
      const backupList = document.getElementById("backup-list-container");
      if (backupList) backupList.style.overflowY = "hidden";

      // 强制插入最新 DOM 结构。
      this.renderBackupList(backupResult);

      // 解锁高度，获取最终应有的自适应高度。
      modalElement.style.height = "auto";
      const newHeight = modalElement.offsetHeight;

      // 先回到旧高度并触发重绘。
      modalElement.style.height = oldHeight + "px";
      modalElement.offsetHeight; // force reflow

      // 激活高度补间动画。
      modalElement.style.transition =
        "height 0.35s cubic-bezier(0.25, 1, 0.5, 1), transform 0.15s ease, opacity 0.15s ease";
      modalElement.style.height = newHeight + "px";

      // 动画完成后解除高度限制，恢复自适应。
      setTimeout(() => {
        modalElement.style.height = "auto";
        // 恢复基础过渡，不再带 height，避免后续变形。
        modalElement.style.transition =
          "transform 0.15s ease, opacity 0.15s ease";
        if (modalBody) modalBody.style.overflowY = "";
        if (backupList) backupList.style.overflowY = "";
      }, 400);
    } catch (e) {
      container.innerHTML = `<div class="backup-empty-state"><div class="backup-empty-icon">${this.icon("triangle-alert")}</div><div>加载失败: ${this.escapeHtml(String(e))}</div></div>`;
      this.refreshIcons();
    }
  },

  renderBackupList(result) {
    const container = document.getElementById("backup-list-container");
    const backupResult = this.normalizeBackupListResult(result);
    const backups = backupResult.backups || [];

    if (backups.length === 0) {
      container.innerHTML = `
        <div class="backup-empty-state">
          <div class="backup-empty-icon">${this.icon("archive")}</div>
          <div>${backupResult.partial ? "未能完整读取备份记录" : "暂无备份记录"}</div>
          ${backupResult.message ? `<div class="section-desc">${this.escapeHtml(backupResult.message)}</div>` : ""}
        </div>
      `;
      this.refreshIcons();
      return;
    }

    const warningBanner = backupResult.partial
      ? `<div class="backup-warning-banner">${this.icon("triangle-alert")}<span>${this.escapeHtml(backupResult.message || "部分备份桶读取失败，列表可能不完整")}</span></div>`
      : "";

    container.innerHTML = warningBanner + backups
      .map((b, index) => {
        const isAuto = b.type === "auto";
        const displayName = b.name || (isAuto ? "自动存档" : "手动存档");
        const iconName = isAuto ? "cloud-upload" : "archive";
        const iconClass = isAuto ? "auto-icon" : "";
        const badgeClass = isAuto ? "auto" : "manual";
        const badgeText = isAuto ? "AUTO" : "MANUAL";
        const statusMeta = this.backupStatusMeta(b);
        const deleteBadge = statusMeta
          ? `<span class="backup-type-badge ${statusMeta.badgeClass}">${this.escapeHtml(statusMeta.badgeText)}</span>`
          : "";
        const sizeMB = b.size
          ? (b.size / 1024 / 1024).toFixed(2) + " MB"
          : "未知";
        const dateStr = b.createdAt
          ? new Date(b.createdAt).toLocaleString("zh-CN")
          : "未知时间";
        const locationText = this.backupLocationText(b);
        const actionDisabled =
          b.status === "pending_upload" || b.status === "pending_delete";
        const errorText = b.lastError || b.lastDeleteError || "";

        return `
        <div class="backup-item animate-slide-up" style="animation-delay: ${index * 0.04}s; animation-fill-mode: both;" data-backup-id="${this.escapeHtmlAttribute(b.filename)}">
          <div class="backup-item-icon ${iconClass}">${this.icon(iconName)}</div>
          <div class="backup-item-info">
            <div class="backup-item-name">
              ${this.escapeHtml(displayName)}
              <span class="backup-type-badge ${badgeClass}">${badgeText}</span>
              ${deleteBadge}
            </div>
            <div class="backup-item-meta">${sizeMB} · ${dateStr} · ${this.escapeHtml(locationText)}${errorText ? ` · ${this.escapeHtml(errorText)}` : ""}</div>
          </div>
          <div class="backup-item-actions">
            <button class="btn btn-secondary btn-sm" data-action="restore-backup" data-filename="${this.escapeHtmlAttribute(b.filename)}" ${actionDisabled ? "disabled" : ""}>恢复</button>
            <button class="btn btn-ghost btn-sm" data-action="delete-backup" data-filename="${this.escapeHtmlAttribute(b.filename)}" ${actionDisabled ? "disabled" : ""}>删除</button>
          </div>
        </div>
      `;
      })
      .join("");
    this.refreshIcons();
  },

  handleBackupListClick(event) {
    const button = event.target.closest("[data-action][data-filename]");
    if (!button) {
      return;
    }
    const filename = button.dataset.filename;
    if (button.dataset.action === "restore-backup") {
      this.restoreBackup(filename);
      return;
    }
    if (button.dataset.action === "delete-backup") {
      this.deleteBackup(filename);
    }
  },

  async createManualBackup() {
    const gameId = this.state.currentGameId;
    if (!gameId) return;
    const nameInput = document.getElementById("backup-custom-name");
    const bName = nameInput.value.trim() || "手动存档";
    const container = document.getElementById("backup-list-container");

    // 1. 创建带进度条的占位元素。
    const loadingItem = document.createElement("div");
    loadingItem.className = "backup-item backup-item-loading";
    loadingItem.innerHTML = `
      <div class="backup-item-icon">${this.icon("archive")}</div>
      <div class="backup-item-info" style="flex: 1;">
        <div class="backup-item-name">正在创建备份...</div>
        <div class="backup-progress-bar">
          <div class="backup-progress-fill"></div>
        </div>
      </div>
    `;
    loadingItem.style.opacity = "0";
    loadingItem.style.transform = "translateX(-20px)";

    // 清除空状态。
    const emptyState = container.querySelector(".backup-empty-state");
    if (emptyState) emptyState.remove();

    // 插入到列表最前面。
    if (container.firstChild) {
      container.insertBefore(loadingItem, container.firstChild);
    } else {
      container.appendChild(loadingItem);
    }
    this.refreshIcons();

    // 2. 播放淡入动画。
    void loadingItem.offsetWidth;
    loadingItem.style.transition = "opacity 0.3s ease, transform 0.3s ease";
    loadingItem.style.opacity = "1";
    loadingItem.style.transform = "translateX(0)";

    try {
      // 3. 调用后端。
      const createdBackup = await this.bridge.CreateGameBackup(
        gameId,
        "manual",
        bName,
      );
      nameInput.value = "";

      // 4. 完成后更新为成功状态。
      loadingItem.classList.remove("backup-item-loading");
      loadingItem.classList.add("backup-item-success");
      const nameEl = loadingItem.querySelector(".backup-item-name");
      if (nameEl) {
        nameEl.textContent =
          createdBackup?.status === "pending_upload" ? "备份已加入上传队列" : "备份已创建";
      }
      const fillEl = loadingItem.querySelector(".backup-progress-fill");
      if (fillEl) fillEl.style.width = "100%";

      if (createdBackup?.status === "upload_failed") {
        this.showToast(
          createdBackup?.lastError || "备份已保存在本地，但云端上传失败",
          "warning",
        );
      } else {
        this.showToast("存档备份创建成功", "success");
      }

      // 5. 延迟后把占位项替换成真实备份项，避免整个列表重排。
      setTimeout(() => this.syncOpenBackupModal(gameId), 300);
    } catch (e) {
      loadingItem.style.opacity = "0";
      setTimeout(() => loadingItem.remove(), 300);
      this.showToast("创建失败: " + e, "error");
    }
  },

  async restoreBackup(filename) {
    const confirmed = await this.showConfirm(
      "确定要恢复此备份吗？当前存档将被覆盖。",
    );
    if (!confirmed) return;
    const gameId = this.state.currentGameId;

    // 找到对应卡片并加上加载状态。
    const backupItem = document.querySelector(`[data-backup-id="${filename}"]`);
    if (backupItem) {
      const nameEl = backupItem.querySelector(".backup-item-name");
      if (nameEl) nameEl.innerHTML = "正在恢复中...";
      backupItem.classList.add("backup-item-loading");
    }

    try {
      await this.bridge.RestoreGameBackup(gameId, filename);
      this.showToast("备份已恢复", "success");

      if (backupItem) {
        backupItem.classList.remove("backup-item-loading");
        const nameEl = backupItem.querySelector(".backup-item-name");
        if (nameEl) nameEl.innerHTML = "恢复完成";
      }

      // 短暂延迟后刷新列表。
      setTimeout(() => this.openBackupModal(gameId), 600);
    } catch (e) {
      if (backupItem) {
        backupItem.classList.remove("backup-item-loading");
      }
      this.showToast("恢复失败: " + e, "error");
      this.openBackupModal(gameId);
    }
  },

  async deleteBackup(filename) {
    const confirmed = await this.showConfirm(
      "确定要删除此备份吗？此操作不可逆。",
      "danger",
    );
    if (!confirmed) return;
    const gameId = this.state.currentGameId;
    this.rememberPendingDeletedBackup(gameId, filename);

    // 先找到对应的 DOM 元素并播放平滑折叠动画。
    // 这里改用 dataset 匹配，避免属性选择器转义问题。
    const backupItem = Array.from(
      document.querySelectorAll(".backup-item"),
    ).find((el) => el.dataset.backupId === filename);

    if (backupItem) {
      // 锁定当前高度。
      backupItem.style.height = backupItem.offsetHeight + "px";
      backupItem.style.overflow = "hidden";
      backupItem.offsetHeight; // 触发重绘。

      backupItem.style.transition =
        "opacity 0.25s ease, transform 0.25s cubic-bezier(0.4, 0, 0.2, 1), height 0.35s cubic-bezier(0.25, 1, 0.5, 1), padding 0.35s ease, margin 0.35s ease, opacity 0.25s ease";
      backupItem.style.opacity = "0";
      backupItem.style.transform = "translateY(-10px) scale(0.98)";
      backupItem.style.height = "0px";
      backupItem.style.paddingTop = "0px";
      backupItem.style.paddingBottom = "0px";
      backupItem.style.marginTop = "0px";
      backupItem.style.marginBottom = "0px";
      backupItem.style.borderWidth = "0px";
    }

    try {
      // 等待动画结束后再调用 API。
      await new Promise((resolve) => setTimeout(resolve, 350));

      // 先从 DOM 中移除元素。
      if (backupItem) {
        backupItem.remove();
      }

      // 调用后端删除。
      await this.bridge.DeleteGameBackup(gameId, filename);
      this.showToast("备份已加入删除队列", "success");

      // 检查列表是否已空。
      const container = document.getElementById("backup-list-container");
      if (!container.querySelector(".backup-item")) {
        this.renderBackupList({ backups: [] });
      }
    } catch (e) {
      this.clearPendingDeletedBackup(gameId, filename);
      this.showToast("删除失败: " + e, "error");
      this.openBackupModal(gameId);
    }
  },

  /**
   * 自定义确认弹窗，替代原生 confirm()
   * @param {string} message - 提示消息
   * @param {'warning'|'danger'} mode - 图标模式
   * @returns {Promise<boolean>}
   */
  showConfirm(message, mode = "warning") {
    return this.showBinaryConfirm(message, { mode });
  },

  showBinaryConfirm(message, options = {}) {
    return new Promise((resolve) => {
      const overlay = document.getElementById("confirm-dialog");
      const msgEl = document.getElementById("confirm-message");
      const iconEl = document.getElementById("confirm-icon");
      const okBtn = document.getElementById("confirm-ok-btn");
      const cancelBtn = document.getElementById("confirm-cancel-btn");
      const mode = options.mode || "warning";
      const confirmText =
        options.confirmText || (mode === "danger" ? "删除" : "确定");
      const cancelText = options.cancelText || "取消";

      msgEl.textContent = message;

      // 设置图标样式。
      iconEl.className = "confirm-icon" + (mode === "danger" ? " danger" : "");
      iconEl.innerHTML = this.icon(
        mode === "danger" ? "trash-2" : "triangle-alert",
      );
      this.refreshIcons();

      // 设置确认按钮样式。
      if (mode === "danger") {
        okBtn.className = "btn btn-danger";
        okBtn.textContent = confirmText;
      } else {
        okBtn.className = "btn btn-primary";
        okBtn.textContent = confirmText;
      }
      cancelBtn.textContent = cancelText;

      const cleanup = () => {
        overlay.classList.remove("active");
        okBtn.removeEventListener("click", onOk);
        cancelBtn.removeEventListener("click", onCancel);
        overlay.removeEventListener("click", onOverlayClick);
      };

      const onOk = () => {
        cleanup();
        resolve(true);
      };
      const onCancel = () => {
        cleanup();
        resolve(false);
      };
      const onOverlayClick = (e) => {
        if (e.target === overlay) {
          cleanup();
          resolve(false);
        }
      };

      okBtn.addEventListener("click", onOk);
      cancelBtn.addEventListener("click", onCancel);
      overlay.addEventListener("click", onOverlayClick);

      overlay.classList.add("active");
    });
  },
};

window.App = App;
App.init();
