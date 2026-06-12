import { useId } from "react";

// 空态吉祥物:旧项目 MascotHint 的静态极简版(几何参数同源:圆角脸 +
// 天线 + 屏幕眼 + 微笑),去掉状态机/换肤/话术池,只做空态插画。
// 配色跟随 --primary token,亮暗两套自动适配。
export function Mascot({ className }: { className?: string }) {
  const faceID = useId();
  return (
    <svg aria-hidden="true" className={className} viewBox="-2.5 -4 33 28.5">
      <defs>
        <linearGradient id={faceID} x1="0" x2="0" y1="0" y2="1">
          <stop offset="0%" stopColor="color-mix(in oklab, var(--primary) 78%, white 22%)" />
          <stop offset="55%" stopColor="var(--primary)" />
          <stop offset="100%" stopColor="color-mix(in oklab, var(--primary) 86%, black 14%)" />
        </linearGradient>
      </defs>
      {/* 天线 */}
      <path
        d="M14 2.35 C14.15 1.25 14.0 0.2 13.65 -0.8"
        fill="none"
        stroke="var(--primary)"
        strokeLinecap="round"
        strokeWidth="0.9"
      />
      <circle cx="13.55" cy="-1.25" fill="var(--primary)" r="1.08" />
      {/* 垂放的双手(画在脸后面,palm 露在脸外两侧) */}
      <path
        d="M3.0 16.6 Q0.9 18.2 1.7 20.7"
        fill="none"
        stroke="var(--primary)"
        strokeLinecap="round"
        strokeWidth="1.1"
      />
      <circle cx="1.7" cy="20.7" fill="var(--primary)" r="1.6" />
      <path
        d="M25.0 16.6 Q27.1 18.2 26.3 20.7"
        fill="none"
        stroke="var(--primary)"
        strokeLinecap="round"
        strokeWidth="1.1"
      />
      <circle cx="26.3" cy="20.7" fill="var(--primary)" r="1.6" />
      {/* 脸 + 顶部高光 */}
      <rect fill={`url(#${faceID})`} height="20" rx="6" width="24" x="2" y="2" />
      <rect fill="white" height="8.8" opacity="0.18" rx="5.2" width="22" x="3" y="2.8" />
      {/* 屏幕眼 + 微笑 */}
      <rect fill="white" height="2.25" rx="0.68" width="3.85" x="8.05" y="10.9" />
      <rect fill="white" height="2.25" rx="0.68" width="3.85" x="16.1" y="10.9" />
      <path
        d="M12.4 17.35 Q14 17.55 15.6 17.35"
        fill="none"
        stroke="white"
        strokeLinecap="round"
        strokeWidth="0.8"
      />
    </svg>
  );
}
