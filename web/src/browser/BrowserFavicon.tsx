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
  const [source, setSource] = useState((faviconURL || "").trim());
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    const remoteURL = (faviconURL || "").trim();
    setSource(remoteURL);
    setFailed(false);
    const resolveFavicon = electronBrowserBridge()?.resolveFavicon;
    if (!resolveFavicon || !remoteURL || remoteURL.startsWith("data:") || !pageURL.trim()) {
      return;
    }
    let disposed = false;
    void resolveFavicon({ url: remoteURL, pageURL }).then((resolvedURL) => {
      if (!disposed && resolvedURL.startsWith("data:image/")) {
        setSource(resolvedURL);
        setFailed(false);
      }
    }).catch(() => undefined);
    return () => {
      disposed = true;
    };
  }, [faviconURL, pageURL]);

  if (!source || failed) {
    return fallback;
  }
  return <img alt="" className={className} draggable={false} src={source} onError={() => setFailed(true)} />;
}
