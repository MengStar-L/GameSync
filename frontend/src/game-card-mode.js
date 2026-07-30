export const GAME_CARD_MODE_CLASSIC = "classic";
export const GAME_CARD_MODE_OVERLAY_HOVER = "overlay-hover";
export const GAME_CARD_MODE_OVERLAY_PERSISTENT = "overlay-persistent";

const VALID_MODES = new Set([
  GAME_CARD_MODE_CLASSIC,
  GAME_CARD_MODE_OVERLAY_HOVER,
  GAME_CARD_MODE_OVERLAY_PERSISTENT,
]);

export function normalizeGameCardMode(value) {
  const mode = String(value || "").trim();
  return VALID_MODES.has(mode) ? mode : GAME_CARD_MODE_CLASSIC;
}

export function isOverlayGameCardMode(value) {
  return normalizeGameCardMode(value) !== GAME_CARD_MODE_CLASSIC;
}

export function gameCardPrimaryAction(value, hasInstallPath) {
  return isOverlayGameCardMode(value) && hasInstallPath ? "launch" : "details";
}

export function isCardClickSuppressed(now, suppressUntil) {
  return Number(now) <= Number(suppressUntil || 0);
}
