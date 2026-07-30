import test from "node:test";
import assert from "node:assert/strict";

import {
  GAME_CARD_MODE_CLASSIC,
  GAME_CARD_MODE_OVERLAY_HOVER,
  GAME_CARD_MODE_OVERLAY_PERSISTENT,
  gameCardPrimaryAction,
  isCardClickSuppressed,
  normalizeGameCardMode,
} from "./game-card-mode.js";

test("normalizes unknown card modes to classic", () => {
  assert.equal(normalizeGameCardMode(""), GAME_CARD_MODE_CLASSIC);
  assert.equal(normalizeGameCardMode("unknown"), GAME_CARD_MODE_CLASSIC);
  assert.equal(normalizeGameCardMode(" overlay-hover "), GAME_CARD_MODE_OVERLAY_HOVER);
});

test("classic cards open details and overlay cards launch", () => {
  assert.equal(gameCardPrimaryAction(GAME_CARD_MODE_CLASSIC, true), "details");
  assert.equal(gameCardPrimaryAction(GAME_CARD_MODE_OVERLAY_HOVER, true), "launch");
  assert.equal(gameCardPrimaryAction(GAME_CARD_MODE_OVERLAY_PERSISTENT, true), "launch");
});

test("overlay cards without an install path open details", () => {
  assert.equal(gameCardPrimaryAction(GAME_CARD_MODE_OVERLAY_HOVER, false), "details");
});

test("suppresses the synthetic click immediately after dragging", () => {
  assert.equal(isCardClickSuppressed(100, 350), true);
  assert.equal(isCardClickSuppressed(350, 350), true);
  assert.equal(isCardClickSuppressed(351, 350), false);
});
