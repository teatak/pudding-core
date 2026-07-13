const fs = require("node:fs");
const http = require("node:http");
const path = require("node:path");

const packageMetadata = require("../package.json");
const { resolveReleaseChannel } = require("../packaging/release-channel.cjs");

const root = path.resolve(process.argv[2] || path.join(__dirname, "..", "dist", "release"));
const port = positiveInt(process.argv[3] || process.env.PUDDING_UPDATE_TEST_PORT, 8099);
const host = "127.0.0.1";
const version = String(process.env.PUDDING_APP_VERSION || packageMetadata.version || "").trim();
const releaseChannel = resolveReleaseChannel(process.env.PUDDING_RELEASE_CHANNEL, version);

if (!fs.existsSync(path.join(root, releaseChannel.updateInfoFile))) {
  console.error(`${releaseChannel.updateInfoFile} not found in ${root}`);
  process.exit(1);
}

const server = http.createServer((request, response) => {
  if (request.method !== "GET" && request.method !== "HEAD") {
    response.writeHead(405, { Allow: "GET, HEAD" });
    response.end();
    return;
  }
  let pathname;
  try {
    pathname = decodeURIComponent(new URL(request.url || "/", `http://${host}`).pathname);
  } catch {
    response.writeHead(400);
    response.end();
    return;
  }
  const filePath = path.resolve(root, `.${pathname}`);
  if (filePath !== root && !filePath.startsWith(`${root}${path.sep}`)) {
    response.writeHead(403);
    response.end();
    return;
  }
  fs.stat(filePath, (error, stat) => {
    if (error || !stat.isFile()) {
      response.writeHead(404);
      response.end();
      return;
    }
    const range = parseRange(request.headers.range, stat.size);
    const headers = {
      "Accept-Ranges": "bytes",
      "Cache-Control": "no-store",
      "Content-Type": contentType(filePath),
    };
    let start = 0;
    let end = stat.size - 1;
    if (range) {
      start = range.start;
      end = range.end;
      headers["Content-Range"] = `bytes ${start}-${end}/${stat.size}`;
    }
    headers["Content-Length"] = String(Math.max(0, end - start + 1));
    response.writeHead(range ? 206 : 200, headers);
    if (request.method === "HEAD") {
      response.end();
      return;
    }
    fs.createReadStream(filePath, { start, end }).pipe(response);
  });
});

server.on("error", (error) => {
  console.error(`Unable to start update test feed: ${error.message}`);
  process.exitCode = 1;
});

server.listen(port, host, () => {
  console.log(`Pudding update test feed: http://${host}:${port}`);
  console.log(`Serving: ${root}`);
});

process.on("SIGINT", () => server.close(() => process.exit(0)));
process.on("SIGTERM", () => server.close(() => process.exit(0)));

function parseRange(value, size) {
  const match = String(value || "").match(/^bytes=(\d+)-(\d*)$/);
  if (!match) {
    return null;
  }
  const start = Number(match[1]);
  const requestedEnd = match[2] ? Number(match[2]) : size - 1;
  if (!Number.isSafeInteger(start) || !Number.isSafeInteger(requestedEnd) || start < 0 || start >= size) {
    return null;
  }
  return { start, end: Math.min(size - 1, Math.max(start, requestedEnd)) };
}

function contentType(filePath) {
  switch (path.extname(filePath).toLowerCase()) {
    case ".yml":
      return "text/yaml; charset=utf-8";
    case ".json":
      return "application/json; charset=utf-8";
    default:
      return "application/octet-stream";
  }
}

function positiveInt(value, fallback) {
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 && parsed <= 65535 ? parsed : fallback;
}
