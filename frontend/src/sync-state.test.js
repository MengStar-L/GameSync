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
