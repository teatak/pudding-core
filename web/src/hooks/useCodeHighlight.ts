import { useEffect, useState } from "react";

import { requestCodeHighlight } from "@/lib/shiki";

export function useCodeHighlight(code: string, lang?: string) {
  const [result, setResult] = useState<{ code: string; lang?: string; html: string | null }>();
  useEffect(() => requestCodeHighlight(code, lang, (html) => setResult({ code, lang, html })), [code, lang]);
  // A late worker reply must never display an older version of the text.
  return result?.code === code && result.lang === lang ? result.html : null;
}
