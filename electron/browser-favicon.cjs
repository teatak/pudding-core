const faviconFetchTimeoutMS = 5_000;
const faviconMaxDownloadBytes = 256 * 1024;
const faviconMaxPNGBytes = 64 * 1024;
const faviconSize = 32;
const allowedFaviconMIMETypes = new Set([
  "image/avif",
  "image/gif",
  "image/jpeg",
  "image/png",
  "image/vnd.microsoft.icon",
  "image/webp",
  "image/x-icon",
]);

async function resolveBrowserFavicon({ url: rawURL, pageURL: rawPageURL, fetch, nativeImage }) {
  const url = safeHTTPURL(rawURL);
  const pageURL = safeHTTPURL(rawPageURL);
  if (!url || !pageURL || url.origin !== pageURL.origin || typeof fetch !== "function" || !nativeImage?.createFromBuffer) {
    return "";
  }

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), faviconFetchTimeoutMS);
  timeout.unref?.();
  try {
    const response = await fetch(url.toString(), {
      credentials: "include",
      referrer: pageURL.toString(),
      signal: controller.signal,
    });
    const mimeType = String(response?.headers?.get?.("content-type") || "").split(";", 1)[0].trim().toLowerCase();
    if (!response?.ok || !allowedFaviconMIMETypes.has(mimeType)) {
      return "";
    }
    const declaredLength = Number(response.headers?.get?.("content-length"));
    if (Number.isFinite(declaredLength) && declaredLength > faviconMaxDownloadBytes) {
      return "";
    }
    const body = await readLimitedBody(response, faviconMaxDownloadBytes);
    if (!body?.length) {
      return "";
    }
    if (body.length <= faviconMaxPNGBytes) {
      return `data:${mimeType};base64,${body.toString("base64")}`;
    }
    const image = nativeImage.createFromBuffer(body);
    if (!image || image.isEmpty()) {
      return "";
    }
    const png = image.resize({ width: faviconSize, height: faviconSize, quality: "best" }).toPNG();
    if (!png.length || png.length > faviconMaxPNGBytes) {
      return "";
    }
    return `data:image/png;base64,${png.toString("base64")}`;
  } catch {
    return "";
  } finally {
    clearTimeout(timeout);
  }
}

function safeHTTPURL(rawURL) {
  try {
    const url = new URL(String(rawURL || "").trim());
    if ((url.protocol !== "http:" && url.protocol !== "https:") || url.username || url.password) {
      return null;
    }
    return url;
  } catch {
    return null;
  }
}

async function readLimitedBody(response, limit) {
  const reader = response.body?.getReader?.();
  if (!reader) {
    const body = Buffer.from(await response.arrayBuffer());
    return body.length <= limit ? body : null;
  }
  const chunks = [];
  let size = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) {
      break;
    }
    const chunk = Buffer.from(value);
    size += chunk.length;
    if (size > limit) {
      await reader.cancel().catch(() => undefined);
      return null;
    }
    chunks.push(chunk);
  }
  return Buffer.concat(chunks, size);
}

module.exports = { resolveBrowserFavicon };
