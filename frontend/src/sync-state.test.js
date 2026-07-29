import test from "node:test";
import assert from "node:assert/strict";

import { deriveSyncStatus } from "./sync-state.js";

test("active work has priority", () => {
  assert.equal(
    deriveSyncStatus({ catalog: { status: "succeeded" }, covers: { game: { status: "syncing" } } }).state,
    "syncing",
  );
});

test("pending cover degrades a completed catalog", () => {
  const status = deriveSyncStatus({
    catalog: { status: "succeeded" },
    covers: { game: { status: "pending", message: "封面等待重试" } },
  });
  assert.deepEqual(status, { state: "degraded", message: "封面等待重试" });
});

test("successful cover retry restores synced state", () => {
  assert.equal(
    deriveSyncStatus({ catalog: { status: "succeeded" }, covers: { game: { status: "succeeded" } } }).state,
    "succeeded",
  );
});

test("offline catalog preserves an explicit offline state", () => {
  assert.equal(deriveSyncStatus({ catalog: { status: "retrying" } }).state, "offline");
});

test("background checking stays silent while a completed catalog remains synced", () => {
  assert.deepEqual(
    deriveSyncStatus({ catalog: { status: "succeeded", message: "云端目录已同步" }, background: { status: "checking" } }),
    { state: "succeeded", message: "云端目录已同步" },
  );
});

test("background transfer and offline state participate in global aggregation", () => {
  assert.equal(deriveSyncStatus({ background: { status: "syncing" } }).state, "syncing");
  assert.equal(deriveSyncStatus({ background: { status: "offline", message: "连接失败" } }).state, "offline");
});

test("background conflict remains degraded after the task completes", () => {
  const status = deriveSyncStatus({
    background: { status: "succeeded" },
    saves: { game: { status: "conflict", message: "存档冲突，等待手动处理" } },
  });
  assert.deepEqual(status, { state: "degraded", message: "存档冲突，等待手动处理" });
});
