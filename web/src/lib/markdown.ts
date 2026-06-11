function escapeHTML(value: string) {
  return value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function escapeAttr(value: string) {
  return escapeHTML(value).replace(/"/g, "&quot;");
}

function safeHref(raw: string) {
  try {
    const url = new URL(raw, window.location.origin);
    if (url.protocol === "http:" || url.protocol === "https:" || url.protocol === "mailto:") {
      return url.href;
    }
  } catch {
    return "";
  }
  return "";
}

// code span 与链接先从原文提取进槽位、再对剩余文本整体转义,
// 避免对捕获的已转义文本二次转义(`Vec<T>` 显示成 Vec&lt;T&gt; 那类 bug)。
function inline(value: string) {
  const slots: string[] = [];
  const stash = (html: string) => {
    const index = slots.length;
    slots.push(html);
    return `\u0000SLOT${index}\u0000`;
  };
  let out = value.replace(/`([^`]+)`/g, (_match, code: string) => stash(`<code>${escapeHTML(code)}</code>`));
  out = out.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_match, text: string, href: string) => {
    const safe = safeHref(href);
    if (!safe) {
      return text; // 留在原文里走统一转义
    }
    return stash(`<a href="${escapeAttr(safe)}" target="_blank" rel="noreferrer noopener">${escapeHTML(text)}</a>`);
  });
  out = escapeHTML(out);
  out = out.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  out = out.replace(/\*([^*]+)\*/g, "<em>$1</em>");
  // 槽位回填跑两遍:链接文本里可嵌 code span(嵌套深度至多 2)
  for (let pass = 0; pass < 2; pass++) {
    out = out.replace(/\u0000SLOT(\d+)\u0000/g, (_match, index: string) => slots[Number(index)] ?? "");
  }
  return out;
}

function isTableSeparator(line: string) {
  return /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(line);
}

function tableCells(line: string) {
  return line
    .trim()
    .replace(/^\|/, "")
    .replace(/\|$/, "")
    .split("|")
    .map((cell) => cell.trim());
}

export function renderMarkdown(source: string) {
  const lines = source.replace(/\r\n/g, "\n").split("\n");
  const blocks: string[] = [];
  let index = 0;

  while (index < lines.length) {
    const line = lines[index] ?? "";
    if (!line.trim()) {
      index += 1;
      continue;
    }

    const fence = /^```(\w+)?\s*$/.exec(line);
    if (fence) {
      const code: string[] = [];
      index += 1;
      while (index < lines.length && !/^```\s*$/.test(lines[index] ?? "")) {
        code.push(lines[index] ?? "");
        index += 1;
      }
      index += 1;
      const lang = fence[1] ? ` data-lang="${escapeAttr(fence[1])}"` : "";
      blocks.push(`<pre${lang}><code>${escapeHTML(code.join("\n"))}</code></pre>`);
      continue;
    }

    if (index + 1 < lines.length && isTableSeparator(lines[index + 1] ?? "")) {
      const headers = tableCells(line);
      const rows: string[][] = [];
      index += 2;
      while (index < lines.length && (lines[index] ?? "").includes("|") && (lines[index] ?? "").trim()) {
        rows.push(tableCells(lines[index] ?? ""));
        index += 1;
      }
      blocks.push(
        `<table><thead><tr>${headers.map((cell) => `<th>${inline(cell)}</th>`).join("")}</tr></thead><tbody>${rows
          .map((row) => `<tr>${row.map((cell) => `<td>${inline(cell)}</td>`).join("")}</tr>`)
          .join("")}</tbody></table>`,
      );
      continue;
    }

    const heading = /^(#{1,4})\s+(.+)$/.exec(line);
    if (heading) {
      const level = heading[1].length;
      blocks.push(`<h${level}>${inline(heading[2])}</h${level}>`);
      index += 1;
      continue;
    }

    if (/^\s*[-*]\s+/.test(line)) {
      const items: string[] = [];
      while (index < lines.length && /^\s*[-*]\s+/.test(lines[index] ?? "")) {
        items.push(`<li>${inline((lines[index] ?? "").replace(/^\s*[-*]\s+/, ""))}</li>`);
        index += 1;
      }
      blocks.push(`<ul>${items.join("")}</ul>`);
      continue;
    }

    if (/^\s*\d+\.\s+/.test(line)) {
      const items: string[] = [];
      while (index < lines.length && /^\s*\d+\.\s+/.test(lines[index] ?? "")) {
        items.push(`<li>${inline((lines[index] ?? "").replace(/^\s*\d+\.\s+/, ""))}</li>`);
        index += 1;
      }
      blocks.push(`<ol>${items.join("")}</ol>`);
      continue;
    }

    const paragraph: string[] = [line];
    index += 1;
    while (index < lines.length && (lines[index] ?? "").trim() && !/^```/.test(lines[index] ?? "")) {
      paragraph.push(lines[index] ?? "");
      index += 1;
    }
    blocks.push(`<p>${inline(paragraph.join("\n"))}</p>`);
  }

  return blocks.join("");
}
