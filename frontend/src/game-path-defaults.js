const clean = (value) => String(value || "").trim();

export function gamePathDialogDefault(kind, game, preferences) {
  const current = game || {};
  const prefs = preferences || {};

  if (kind === "install") {
    return (
      clean(current.installPath) ||
      clean(current.isSteam ? prefs.defaultSteamInstallDir : prefs.defaultThirdInstallDir)
    );
  }

  if (kind === "save") {
    if (clean(current.savePath)) return clean(current.savePath);
    if (current.isSteam) return clean(prefs.defaultSteamSaveDir);
    return clean(current.installPath) || clean(prefs.defaultThirdInstallDir);
  }

  return "";
}
