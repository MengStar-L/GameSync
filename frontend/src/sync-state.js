const ACTIVE_STATUSES = new Set(["queued", "syncing"]);
const OFFLINE_STATUSES = new Set(["offline", "retrying"]);

export function deriveSyncStatus({ catalog = {}, covers = {}, saves = {} } = {}) {
  const coverStates = Object.values(covers);
  const saveStates = Object.values(saves);
  const active =
    ACTIVE_STATUSES.has(catalog.status) ||
    coverStates.some((item) => item?.status === "syncing") ||
    saveStates.some((item) => item?.status === "syncing");
  if (active) {
    const activeCover = coverStates.find((item) => item?.status === "syncing");
    const activeSave = saveStates.find((item) => item?.status === "syncing");
    return { state: "syncing", message: activeCover?.message || activeSave?.message || catalog.message || "正在同步" };
  }

  if (OFFLINE_STATUSES.has(catalog.status)) {
    return { state: "offline", message: catalog.message || "云端暂时无法连接，本地内容仍可使用" };
  }

  const pendingCover = coverStates.find((item) => item?.status === "pending");
  if (pendingCover) {
    return { state: "degraded", message: pendingCover.message || "封面等待重试" };
  }

  const failedSave = saveStates.find((item) => item?.status === "failed" || item?.status === "conflict");
  if (failedSave) {
    return { state: "degraded", message: failedSave.message || "部分内容尚未同步" };
  }

  if (
    catalog.status === "succeeded" ||
    coverStates.some((item) => item?.status === "succeeded") ||
    saveStates.some((item) => item?.status === "succeeded")
  ) {
    return { state: "succeeded", message: catalog.message || "已同步" };
  }

  if (catalog.status === "pending") {
    return { state: "pending", message: catalog.message || "等待同步" };
  }
  return { state: "checking", message: catalog.message || "检测中" };
}
