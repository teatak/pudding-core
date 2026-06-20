import { useEffect, useRef, useState } from "react";

// 流式文本平滑(typewriter):provider 的 SSE chunk 粒度参差——OpenAI/DeepSeek
// 逐 token、Gemini 常一句/一整段,中间还可能被 dev proxy 批量——直接渲染会忽快
// 忽慢甚至"整段蹦"。这里把到达的全量 target 文本用 rAF 匀速揭示,解耦"网络到达
// 节奏"与"视觉呈现节奏":只改呈现,不改数据到达速度。
//
// 收敛策略:按字符预算匀速揭示,backlog 大时只温和提速,避免一帧吐出整段。
// completed 后如果还有未揭示 backlog,继续吃完;只有 reduced-motion 才 snap。
const MIN_CPS = 42; // 稳定速度(字符/秒)
const MAX_CPS = 120; // backlog 大时的最高速度
const MAX_STEP = 3; // 单帧最多吐几个字符,保证视觉上仍像打字

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
    if (reduce) {
      stop();
      displayRef.current = targetRef.current;
      setDisplay(targetRef.current);
      return;
    }
    let last = 0;
    let budget = 0;
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
        const cps = Math.min(MAX_CPS, MIN_CPS + remaining * 0.35);
        budget += cps * dt;
        if (budget >= 1) {
          const step = Math.min(remaining, MAX_STEP, Math.floor(budget));
          budget -= step;
          const next = tgt.slice(0, cur.length + step);
          displayRef.current = next;
          setDisplay(next);
        }
      }
      if (streaming || displayRef.current.length < targetRef.current.length) {
        rafRef.current = requestAnimationFrame(tick);
      } else {
        rafRef.current = null;
      }
    };
    if (!streaming && displayRef.current === targetRef.current) {
      stop();
      return;
    }
    rafRef.current = requestAnimationFrame(tick);
    return stop;
  }, [streaming]);

  return display;
}
