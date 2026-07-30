import test from "node:test";
import assert from "node:assert/strict";

import { gamePathDialogDefault } from "./game-path-defaults.js";

const preferences = {
  defaultSteamInstallDir: "D:/SteamLibrary/steamapps/common",
  defaultSteamSaveDir: "C:/Users/player/SteamSaves",
  defaultThirdInstallDir: "E:/Games",
};

test("new Steam games use the two Steam defaults", () => {
  const game = { isSteam: true, installPath: "", savePath: "" };
  assert.equal(gamePathDialogDefault("install", game, preferences), preferences.defaultSteamInstallDir);
  assert.equal(gamePathDialogDefault("save", game, preferences), preferences.defaultSteamSaveDir);
});

test("new third-party games use the game root then the selected executable", () => {
  const game = { isSteam: false, installPath: "", savePath: "" };
  assert.equal(gamePathDialogDefault("install", game, preferences), preferences.defaultThirdInstallDir);
  assert.equal(gamePathDialogDefault("save", game, preferences), preferences.defaultThirdInstallDir);

  game.installPath = "E:/Games/Hades/Hades.exe";
  assert.equal(gamePathDialogDefault("save", game, preferences), game.installPath);
});

test("existing game paths take priority over device defaults", () => {
  const game = { isSteam: true, installPath: "F:/Current/game.exe", savePath: "F:/Current/saves" };
  assert.equal(gamePathDialogDefault("install", game, preferences), game.installPath);
  assert.equal(gamePathDialogDefault("save", game, preferences), game.savePath);
});

test("platform changes affect empty fields without clearing entered paths", () => {
  const game = { isSteam: false, installPath: "", savePath: "" };
  assert.equal(gamePathDialogDefault("install", game, preferences), preferences.defaultThirdInstallDir);
  game.isSteam = true;
  assert.equal(gamePathDialogDefault("install", game, preferences), preferences.defaultSteamInstallDir);

  game.installPath = "G:/Chosen/game.exe";
  game.isSteam = false;
  assert.equal(gamePathDialogDefault("install", game, preferences), game.installPath);
});
