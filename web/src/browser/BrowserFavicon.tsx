import { useEffect, useState, type ReactNode } from "react";

import { electronBrowserBridge } from "@/browser/electronBridge";

export function BrowserFavicon({
  className,
  fallback,
  faviconURL,
  pageURL,
}: {
  className?: string;
  fallback: ReactNode;
  faviconURL?: string;
  pageURL: string;
}) {
  const requestedURL = faviconURL?.trim() || conventionalFaviconURL(pageURL);
  const [source, setSource] = useState(requestedURL);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    const remoteURL = requestedURL;
    setSource(remoteURL);
    setFailed(false);
    const resolveFavicon = electronBrowserBridge()?.resolveFavicon;
    if (
      !resolveFavicon ||
      !remoteURL ||
      remoteURL.startsWith("data:") ||
      !pageURL.trim()
    ) {
      return;
    }
    let disposed = false;
    void resolveFavicon({ url: remoteURL, pageURL })
      .then((resolvedURL) => {
        if (!disposed && resolvedURL.startsWith("data:image/")) {
          setSource(resolvedURL);
          setFailed(false);
        }
      })
      .catch(() => undefined);
    return () => {
      disposed = true;
    };
  }, [requestedURL, pageURL]);

  if (!source || failed) {
    return fallback;
  }
  return (
    <img
      alt=""
      className={className}
      draggable={false}
      src={source}
      onError={() => setFailed(true)}
    />
  );
}

// Bookmarks may only have a page URL. Use the site's conventional icon through
// the same browser-session resolver used for icons advertised by open tabs.
function conventionalFaviconURL(pageURL: string): string {
  try {
    const page = new URL(pageURL);
    if (
      (page.protocol === "http:" || page.protocol === "https:") &&
      !page.username &&
      !page.password
    ) {
      return new URL("/favicon.ico", page).href;
    }
  } catch {
    /* Non-page URLs have no site icon. */
  }
  return "";
}
