const longPasteAttachmentMinChars = 4000;

export function shouldAttachPastedText(text: string) {
  return text.trim().length >= longPasteAttachmentMinChars;
}

export function createPastedTextAttachmentFile(text: string, date = new Date()) {
  return new File([text], `pasted-text-${formatTimestamp(date)}.txt`, { type: "text/plain" });
}

function formatTimestamp(date: Date) {
  const pad = (value: number) => value.toString().padStart(2, "0");
  return [
    date.getFullYear(),
    pad(date.getMonth() + 1),
    pad(date.getDate()),
    "-",
    pad(date.getHours()),
    pad(date.getMinutes()),
    pad(date.getSeconds()),
  ].join("");
}
