// 模型 id → 展示名:"deepseek-v4-flash" → "Deepseek V4 Flash"。
// 只影响展示,提交给 API 的始终是原始 id。
const ACRONYMS: Record<string, string> = {
  gpt: "GPT",
  glm: "GLM",
  oss: "OSS",
};

export function formatModelLabel(id: string): string {
  const tail = id.split("/").pop() || id;
  return tail
    .split(/[-_]/)
    .filter(Boolean)
    .map((segment) => ACRONYMS[segment.toLowerCase()] ?? segment.charAt(0).toUpperCase() + segment.slice(1))
    .join(" ");
}
