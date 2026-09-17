import { providerModelDisplayName } from "@/provider/presets";

// 模型 id / 自定义展示名 → 最终界面展示名。
// 优先使用用户自定义的 displayName，未设置时通过统一的 providerModelDisplayName 格式化 tail id。
export function formatModelLabel(id: string, displayName?: string): string {
  const trimmed = displayName?.trim();
  if (trimmed) {
    return trimmed;
  }
  const tail = id.split("/").pop() || id;
  return providerModelDisplayName(tail);
}
