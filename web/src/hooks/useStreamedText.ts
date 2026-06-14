import { useEffect, useRef, useState } from "react";

// 流式文本平滑(typewriter):provider 的 SSE chunk 粒度参差——OpenAI/DeepSeek
// 逐 token、Gemini 常一句/一整段,中间还可能被 dev proxy 批量——直接渲染会忽快
// 忽慢甚至"整段蹦"。这里把到达的全量 target 文本用 rAF 匀速揭示,解耦"网络到达
// 节奏"与"视觉呈现节奏":只改呈现,不改数据到达速度。
//
// 收敛策略 = 每帧揭示 max(按比例追赶 backlog, 最低速率底)字符:
//   - CATCH_UP 比例项让大 chunk 平滑加速吃进(不会卡住等很久),
//   - MIN_CPS 底让稳定小 delta 也始终在动。
// 非 streaming(完成/失败/取消)立即 snap 到全量,prefers-reduced-motion 同样 snap。
const MIN_CPS = 40; // 最低揭示速率(字符/秒)
const CATCH_UP = 0.18; // 每帧追赶 backlog 的比例

export function useStreamedText(target: string, streaming: boolean): string {
  const [display, setDisplay] = useState(streaming ? "" : target);
  // target 每次 render 同步进 ref:rAF 循环只在 streaming 翻转时重建,
  // 持续读 ref 拿最新全量,不必每个 delta 重启循环。
  const targetRef = useRef(target);
  targetRef.current = target;
  const displayRef = useRef(display);
  const rafRef = useRef<number | null>(null);

  useEffect(() => {
    const stop = () => {
      if (rafRef.current !== null) {
        cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
    };
    const reduce =
      typeof window !== "undefined" &&
      typeof window.matchMedia === "function" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (!streaming || reduce) {
      stop();
      displayRef.current = targetRef.current;
      setDisplay(targetRef.current);
      return;
    }
    let last = 0;
    const tick = (ts: number) => {
      const dt = last ? (ts - last) / 1000 : 0;
      last = ts;
      const tgt = targetRef.current;
      const cur = displayRef.current;
      if (cur.length > tgt.length) {
        // target 收缩(turnID 维度文本理论只增,防御性跟随)
        displayRef.current = tgt;
        setDisplay(tgt);
      } else if (cur.length < tgt.length) {
        const remaining = tgt.length - cur.length;
        const step = Math.max(1, Math.ceil(remaining * CATCH_UP), Math.ceil(MIN_CPS * dt));
        const next = tgt.slice(0, cur.length + Math.min(step, remaining));
        displayRef.current = next;
        setDisplay(next);
      }
      rafRef.current = requestAnimationFrame(tick);
    };
    rafRef.current = requestAnimationFrame(tick);
    return stop;
  }, [streaming]);

  return display;
}
