const fs = require("node:fs");
const path = require("node:path");
const util = require("node:util");

const defaultRetentionDays = 10;

class DailyLogWriter {
  constructor({ logsDir, prefix, retentionDays = defaultRetentionDays, now = () => new Date() }) {
    if (!String(prefix || "").trim()) {
      throw new Error("file logger requires a prefix");
    }
    if (!Number.isInteger(retentionDays) || retentionDays < 1) {
      throw new Error("file logger retentionDays must be positive");
    }
    this.logsDir = logsDir;
    this.prefix = prefix;
    this.retentionDays = retentionDays;
    this.now = now;
    this.currentDay = "";
    fs.mkdirSync(logsDir, { recursive: true, mode: 0o700 });
    this.rotate(this.now());
  }

  write(level, args) {
    const now = this.now();
    this.rotate(now);
    const message = redactLogText(util.format(...args)).replace(/\r?\n/g, "\\n");
    fs.appendFileSync(this.currentPath, `${now.toISOString()} level=${level} ${message}\n`, {
      encoding: "utf8",
      mode: 0o600,
    });
  }

  rotate(now) {
    const day = localDay(now);
    if (this.currentDay === day) {
      return;
    }
    try {
      cleanupExpiredLogs(this.logsDir, this.prefix, this.retentionDays, now);
    } catch (error) {
      process.stderr.write(`[electron] log cleanup failed: ${error.message}\n`);
    }
    this.currentDay = day;
    this.currentPath = path.join(this.logsDir, `${this.prefix}-${day}.log`);
  }
}

function installConsoleFileLogging(options) {
  let writer;
  try {
    writer = new DailyLogWriter(options);
  } catch (error) {
    process.stderr.write(`[electron] file logging unavailable: ${error.message}\n`);
    return null;
  }

  for (const [method, level] of [
    ["info", "INFO"],
    ["log", "INFO"],
    ["warn", "WARN"],
    ["error", "ERROR"],
  ]) {
    const original = console[method].bind(console);
    console[method] = (...args) => {
      original(...args);
      try {
        writer.write(level, args);
      } catch (error) {
        process.stderr.write(`[electron] file log write failed: ${error.message}\n`);
      }
    };
  }
  return writer;
}

function cleanupExpiredLogs(logsDir, prefix, retentionDays, now) {
  const cutoff = new Date(now.getFullYear(), now.getMonth(), now.getDate() - (retentionDays - 1));
  const escapedPrefix = escapeRegExp(prefix);
  const pattern = new RegExp(`^${escapedPrefix}-(\\d{4}-\\d{2}-\\d{2})\\.log$`);
  for (const entry of fs.readdirSync(logsDir, { withFileTypes: true })) {
    if (!entry.isFile()) {
      continue;
    }
    const match = pattern.exec(entry.name);
    if (!match) {
      continue;
    }
    const day = parseLocalDay(match[1]);
    if (day && day < cutoff) {
      try {
        fs.unlinkSync(path.join(logsDir, entry.name));
      } catch (error) {
        if (error.code !== "ENOENT") {
          throw error;
        }
      }
    }
  }
}

function localDay(value) {
  const year = String(value.getFullYear()).padStart(4, "0");
  const month = String(value.getMonth() + 1).padStart(2, "0");
  const day = String(value.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function parseLocalDay(value) {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (!match) {
    return null;
  }
  const parsed = new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]));
  return localDay(parsed) === value ? parsed : null;
}

function redactLogText(value) {
  return String(value)
    .replace(/([?&](?:token|access_token|refresh_token|id_token|code)=)[^&\s"'<>]+/gi, "$1[REDACTED]")
    .replace(/\b(Bearer\s+)[A-Za-z0-9._~+/=-]+/gi, "$1[REDACTED]")
    .replace(
      /((?:token|access_token|refresh_token|id_token|api[_-]?key|authorization|password|secret)\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s,;]+)/gi,
      "$1[REDACTED]",
    );
}

function escapeRegExp(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

module.exports = {
  DailyLogWriter,
  defaultRetentionDays,
  installConsoleFileLogging,
  redactLogText,
};
