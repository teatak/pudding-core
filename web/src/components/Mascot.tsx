import { useEffect, useId, useRef, type CSSProperties } from "react";

type MascotProps = {
  className?: string;
  showFaceDebugFrame?: boolean;
};

const EYE_MAX_X = 5;
const EYE_MAX_Y = 3;
const HEAD_YAW_MAX_DEG = 20;
const HEAD_PITCH_MAX_DEG = 16;
const HEAD_ROLL_MAX_DEG = 4;
const SAT_RATIO = 1.15;
const FOLLOW_EASE = 0.22;
const FOLLOW_EPSILON = 0.01;
const HEAD_ORIGIN_Y_PERCENT = (79 / 128) * 100;
const HEAD_PERSPECTIVE_PX = 220;
const HEAD_SHIFT_X = 1.5;
const HEAD_SHIFT_Y = 0.75;
const FACE_SHIFT_X = 14;
const FACE_SHIFT_Y = 5;

const absoluteLayerStyle: CSSProperties = {
  height: "100%",
  inset: 0,
  overflow: "visible",
  position: "absolute",
  width: "100%",
};

const rootStyle: CSSProperties = {
  display: "inline-block",
  overflow: "visible",
  perspective: `${HEAD_PERSPECTIVE_PX}px`,
  perspectiveOrigin: `50% ${HEAD_ORIGIN_Y_PERCENT}%`,
  position: "relative",
};

const headStyle: CSSProperties = {
  ...absoluteLayerStyle,
  transform: "translate(0px, 0px) rotateY(0deg) rotateX(0deg) rotateZ(0deg)",
  transformOrigin: `50% ${HEAD_ORIGIN_Y_PERCENT}%`,
  transformStyle: "preserve-3d",
  willChange: "transform",
};

export function Mascot({ className, showFaceDebugFrame = false }: MascotProps) {
  const rootRef = useRef<HTMLSpanElement>(null);
  const headRef = useRef<HTMLSpanElement>(null);
  const faceRef = useRef<SVGGElement>(null);
  const currentXRef = useRef(0);
  const currentYRef = useRef(0);
  const targetXRef = useRef(0);
  const targetYRef = useRef(0);
  const rafRef = useRef(0);
  const id = useId().replace(/:/g, "");
  const faceID = `mascot-face-${id}`;
  const highlightID = `mascot-highlight-${id}`;

  useEffect(() => {
    const applyPose = (x: number, y: number) => {
      const turnX = x / EYE_MAX_X;
      const turnY = y / EYE_MAX_Y;
      const head = headRef.current;
      const face = faceRef.current;

      if (head) {
        head.style.transform = `translate(${turnX * HEAD_SHIFT_X}px, ${turnY * HEAD_SHIFT_Y}px) rotateY(${turnX * HEAD_YAW_MAX_DEG}deg) rotateX(${-turnY * HEAD_PITCH_MAX_DEG}deg) rotateZ(${turnX * turnY * HEAD_ROLL_MAX_DEG}deg)`;
      }
      if (face) {
        face.setAttribute("transform", `translate(${turnX * FACE_SHIFT_X} ${turnY * FACE_SHIFT_Y})`);
      }
    };

    const tick = () => {
      const currentX = currentXRef.current;
      const currentY = currentYRef.current;
      const targetX = targetXRef.current;
      const targetY = targetYRef.current;
      const nextX = currentX + (targetX - currentX) * FOLLOW_EASE;
      const nextY = currentY + (targetY - currentY) * FOLLOW_EASE;
      currentXRef.current = Math.abs(nextX - targetX) < FOLLOW_EPSILON ? targetX : nextX;
      currentYRef.current = Math.abs(nextY - targetY) < FOLLOW_EPSILON ? targetY : nextY;
      applyPose(currentXRef.current, currentYRef.current);

      if (
        Math.abs(currentXRef.current - targetX) > FOLLOW_EPSILON ||
        Math.abs(currentYRef.current - targetY) > FOLLOW_EPSILON
      ) {
        rafRef.current = window.requestAnimationFrame(tick);
        return;
      }
      rafRef.current = 0;
    };

    const startTick = () => {
      if (!rafRef.current) {
        rafRef.current = window.requestAnimationFrame(tick);
      }
    };

    const onMove = (event: MouseEvent) => {
      const rect = rootRef.current?.getBoundingClientRect();
      if (!rect) return;

      const cx = rect.left + rect.width / 2;
      const cy = rect.top + rect.height / 2;
      const dx = event.clientX - cx;
      const dy = event.clientY - cy;
      const distance = Math.hypot(dx, dy);
      if (distance < 1) {
        targetXRef.current = 0;
        targetYRef.current = 0;
        startTick();
        return;
      }

      const sat = Math.max(rect.width, rect.height) * SAT_RATIO;
      const k = Math.min(distance / sat, 1);
      targetXRef.current = (dx / distance) * k * EYE_MAX_X;
      targetYRef.current = (dy / distance) * k * EYE_MAX_Y;
      startTick();
    };

    applyPose(0, 0);
    window.addEventListener("mousemove", onMove, { passive: true });
    return () => {
      if (rafRef.current) window.cancelAnimationFrame(rafRef.current);
      window.removeEventListener("mousemove", onMove);
    };
  }, []);

  return (
    <span ref={rootRef} aria-hidden="true" className={className} style={rootStyle}>
      <svg data-slot="mascot-base" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
        <g data-slot="shadow">
          <ellipse cx="64" cy="113" fill="color-mix(in oklab, var(--primary) 18%, transparent)" rx="34" ry="7" />
        </g>
      </svg>

      <span data-slot="head" ref={headRef} style={headStyle}>
        <svg data-slot="head-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
          <defs>
            <linearGradient id={faceID} x1="0" x2="0" y1="0" y2="1">
              <stop offset="0%" stopColor="color-mix(in oklab, var(--primary) 78%, white 22%)" />
              <stop offset="55%" stopColor="var(--primary)" />
              <stop offset="100%" stopColor="color-mix(in oklab, var(--primary) 86%, black 14%)" />
            </linearGradient>
            <linearGradient id={highlightID} x1="0" x2="0" y1="0" y2="1">
              <stop offset="0%" stopColor="#ffffff" stopOpacity="0.26" />
              <stop offset="100%" stopColor="#ffffff" stopOpacity="0" />
            </linearGradient>
          </defs>

          <g data-slot="arms">
            <g data-slot="arm-left">
              <path
                data-slot="arm-left-limb"
                d="M30 85 C21 90 16 98 16 105"
                fill="none"
                stroke="color-mix(in oklab, var(--primary) 86%, black 14%)"
                strokeLinecap="round"
                strokeWidth="5.5"
              />
              <circle
                data-slot="arm-left-palm"
                cx="16"
                cy="106"
                fill="color-mix(in oklab, var(--primary) 86%, black 14%)"
                r="7"
              />
            </g>
            <g data-slot="arm-right">
              <path
                data-slot="arm-right-limb"
                d="M98 85 C107 90 112 98 112 105"
                fill="none"
                stroke="color-mix(in oklab, var(--primary) 86%, black 14%)"
                strokeLinecap="round"
                strokeWidth="5.5"
              />
              <circle
                data-slot="arm-right-palm"
                cx="112"
                cy="106"
                fill="color-mix(in oklab, var(--primary) 86%, black 14%)"
                r="7"
              />
            </g>
          </g>

          <g data-slot="antenna">
            <path
              d="M64 34 C65 28 63 23 60 18"
              fill="none"
              stroke="color-mix(in oklab, var(--primary) 86%, white 14%)"
              strokeLinecap="round"
              strokeWidth="4"
            />
            <circle
              cx="59"
              cy="16"
              fill="var(--primary)"
              r="6"
              stroke="color-mix(in oklab, var(--primary) 62%, white 38%)"
              strokeWidth="2"
            />
          </g>

          <g data-slot="body">
            <rect fill={`url(#${faceID})`} height="76" rx="24" width="86" x="21" y="34" />
            <rect fill={`url(#${highlightID})`} height="34" rx="18" width="74" x="27" y="39" />
            <path
              d="M97 45 C102 51 104 59 104 70 V86 C104 98 93 108 80 110 C92 103 97 93 97 79 Z"
              fill="color-mix(in oklab, var(--primary) 76%, black 24%)"
              opacity="0.16"
            />
          </g>

          <g ref={faceRef} data-slot="face" transform="translate(0 0)">
            {showFaceDebugFrame ? (
              <rect
                data-slot="face-debug-frame"
                fill="none"
                height="38"
                rx="6"
                stroke="#ff7a00"
                strokeDasharray="3 3"
                strokeWidth="1.5"
                width="52"
                x="38"
                y="58"
              />
            ) : null}
            <g data-slot="eyes" fill="#ffffff">
              <rect data-slot="eye-left" height="9" rx="3" width="15" x="43" y="65" />
              <rect data-slot="eye-right" height="9" rx="3" width="15" x="70" y="65" />
            </g>
            <g data-slot="eye-glints" fill="#ffffff" opacity="0.28">
              <rect height="1.4" rx="0.7" width="3.2" x="46" y="67" />
              <rect height="1.4" rx="0.7" width="3.2" x="73" y="67" />
            </g>
            <path
              data-slot="mouth"
              d="M55 88 Q64 91.5 73 88"
              fill="none"
              stroke="#ffffff"
              strokeLinecap="round"
              strokeWidth="4"
            />
          </g>
        </svg>
      </span>
    </span>
  );
}
