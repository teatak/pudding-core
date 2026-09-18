import { providerModelDisplayName } from "@/provider/presets";

export function cleanModelDisplayName(raw: string): string {
  let text = raw.trim();
  if (!text) {
    return "";
  }
  // 1. 如果展示名中包含 " / "，如 "OpenRouter / Free", "Z Ai / GLM 5.3 Flash", "DeepSeek / DeepSeek V4.1 Flash"
  if (text.includes(" / ")) {
    const parts = text.split(" / ").map((part) => part.trim()).filter(Boolean);
    if (parts.length > 1) {
      text = parts[parts.length - 1];
    }
  }
  // 2. 如果包含冒号组织前缀，如 "Qwen: Qwen3.8 27B (free)", "Z.ai: GLM 5.3 Flash", "Meta: Llama 3.3 70B"
  const colonMatch = text.match(/^([A-Za-z0-9_.-]+):\s+(.+)$/);
  if (colonMatch && colonMatch[2]?.trim()) {
    text = colonMatch[2].trim();
  }
  // 3. 剥离末尾的 (free) / (batch) 等冗余状态后缀（列表中已有专用徽标承载）
  // 例如 "Qwen3.8 27B (free)" -> "Qwen3.8 27B", "GLM 5.2 (Free)" -> "GLM 5.2"
  const cleanedSuffix = text.replace(/\s*\((?:free|batch)\)$/i, "").trim();
  if (cleanedSuffix) {
    text = cleanedSuffix;
  }
  return text;
}

// 模型 id / 自定义展示名 → 最终界面展示名。
// 优先使用用户自定义的 displayName（清洗去除聚合前缀），未设置时通过统一的 providerModelDisplayName 格式化 tail id。
export function formatModelLabel(id: string, displayName?: string): string {
  const trimmed = displayName?.trim();
  if (trimmed) {
    return cleanModelDisplayName(trimmed);
  }
  const tail = id.split("/").pop() || id;
  return providerModelDisplayName(tail);
}
