import test from "node:test";
import assert from "node:assert/strict";

import { hasUpdateNotice, mergeUpdateCheckState } from "./update-state.js";

test("available and blocked versions require a settings notice", () => {
  assert.equal(hasUpdateNotice({ status: "available" }), true);
  assert.equal(hasUpdateNotice({ status: "blocked" }), true);
  assert.equal(hasUpdateNotice({ status: "latest" }), false);
  assert.equal(hasUpdateNotice({ status: "unconfigured" }), false);
});

test("checking and failed attempts retain the last valid result", () => {
  const previous = {
    status: "succeeded",
    result: { status: "available", latestVersion: "1.2.3" },
    checkedAt: "2026-07-30T12:00:00Z",
  };
  assert.equal(mergeUpdateCheckState(previous, { status: "checking" }).result.latestVersion, "1.2.3");
  assert.equal(mergeUpdateCheckState(previous, { status: "idle" }).result.latestVersion, "1.2.3");
});

test("a successful latest result clears the notice", () => {
  const next = mergeUpdateCheckState(
    { status: "succeeded", result: { status: "available" } },
    { status: "succeeded", result: { status: "latest" }, checkedAt: "2026-07-30T13:00:00Z" },
  );
  assert.equal(hasUpdateNotice(next.result), false);
  assert.equal(next.checkedAt, "2026-07-30T13:00:00Z");
});
