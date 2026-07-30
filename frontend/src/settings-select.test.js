import test from "node:test";
import assert from "node:assert/strict";

import { moveSelectIndex, shouldOpenSelectUp } from "./settings-select.js";

test("custom select wraps arrow navigation", () => {
  assert.equal(moveSelectIndex("ArrowDown", 2, 3), 0);
  assert.equal(moveSelectIndex("ArrowUp", 0, 3), 2);
});

test("custom select handles Home and End", () => {
  assert.equal(moveSelectIndex("Home", 1, 3), 0);
  assert.equal(moveSelectIndex("End", 1, 3), 2);
});

test("custom select ignores unrelated keys", () => {
  assert.equal(moveSelectIndex("Tab", 1, 3), 1);
});

test("custom select opens upward only when the lower viewport would clip it", () => {
  const triggerRect = { top: 660, bottom: 696 };
  assert.equal(shouldOpenSelectUp(triggerRect, 150, 844), true);
  assert.equal(shouldOpenSelectUp({ top: 200, bottom: 236 }, 150, 844), false);
  assert.equal(shouldOpenSelectUp({ top: 50, bottom: 86 }, 800, 844), false);
});
