const NOTICE_STATUSES = new Set(["available", "blocked"]);

export function hasUpdateNotice(result) {
  return NOTICE_STATUSES.has(result?.status);
}

export function mergeUpdateCheckState(previous, incoming) {
  const prev = previous || { status: "idle", result: null, checkedAt: "" };
  const next = incoming || {};
  return {
    status: next.status || prev.status || "idle",
    result: next.result || prev.result || null,
    checkedAt: next.checkedAt || prev.checkedAt || "",
  };
}
