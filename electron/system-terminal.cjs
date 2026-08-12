function createSystemTerminalOpener({
  env = process.env,
  platform = process.platform,
  spawn,
}) {
  return async function openSystemTerminal(cwd) {
    if (platform === "darwin") {
      return spawnDetached(spawn, "open", ["-a", "Terminal", cwd]);
    }
    if (platform === "win32") {
      return spawnDetached(spawn, env.ComSpec || "cmd.exe", ["/d", "/k"], { cwd });
    }
    const configured = String(env.TERMINAL || "").trim();
    const candidates = [
      ...(configured && !/\s/.test(configured) ? [[configured, []]] : []),
      ["x-terminal-emulator", ["--working-directory", cwd]],
      ["gnome-terminal", ["--working-directory", cwd]],
      ["konsole", ["--workdir", cwd]],
      ["kitty", ["--directory", cwd]],
      ["wezterm", ["start", "--cwd", cwd]],
    ];
    for (const [command, args] of candidates) {
      if (await spawnDetached(spawn, command, args, { cwd })) {
        return true;
      }
    }
    return false;
  };
}

function spawnDetached(spawn, command, args, options = {}) {
  return new Promise((resolve) => {
    let settled = false;
    const child = spawn(command, args, {
      cwd: options.cwd,
      detached: true,
      stdio: "ignore",
      windowsHide: false,
    });
    child.once("error", () => {
      if (!settled) {
        settled = true;
        resolve(false);
      }
    });
    child.once("spawn", () => {
      if (!settled) {
        settled = true;
        child.unref();
        resolve(true);
      }
    });
  });
}

module.exports = { createSystemTerminalOpener };
