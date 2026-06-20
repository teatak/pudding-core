import { useEffect, useId, useLayoutEffect, useRef, type CSSProperties } from "react";

type MascotProps = {
  className?: string;
  gaze?: MascotGaze;
  inputPitchBias?: number;
  mood?: MascotMood;
  onPointerGaze?: () => void;
  showFaceDebugFrame?: boolean;
  showHeadDebugFrame?: boolean;
};

export type MascotMood = "idle" | "thinking" | "ready" | "error";

export type MascotGaze =
  | { type: "pointer" }
  | { type: "input"; target: MascotGazePoint }
  | { type: "center" };

export type MascotGazePoint = {
  clientX: number;
  clientY: number;
};

const EYE_MAX_X = 5;
const EYE_MAX_Y = 3;
const HEAD_YAW_MAX_DEG = 20;
const HEAD_PITCH_MAX_DEG = 16;
const HEAD_ROLL_MAX_DEG = 4;
const SAT_RATIO = 1.15;
const POINTER_SAT_MIN_PX = 128;
const FOLLOW_EASE = 0.22;
const FOLLOW_EPSILON = 0.01;
const HEAD_ORIGIN_Y_PERCENT = (77 / 128) * 100;
const HEAD_PERSPECTIVE_PX = 170;
const HEAD_SHIFT_X_PERCENT = 2.7;
const HEAD_SHIFT_Y_PERCENT = 1.35;
const FACE_SHIFT_X = 14;
const FACE_SHIFT_Y = 5;
const FACE_CENTER_X = 64;
const FACE_CENTER_Y = 77;
const FACE_EXTRA_SKEW_DEG = 4;
const FACE_EXTRA_SQUEEZE_X = 0.035;
const FACE_EXTRA_SCALE_Y = 0.025;
const FACE_EXTRA_YAW_DEG = 4;
const FACE_EXTRA_PITCH_DEG = 3;
const FACE_Z_OFFSET_PX = 4;
const SHADOW_SHIFT_X = 0.7;
const SHADOW_SQUEEZE_X = 0.45;
const SHADOW_SCALE_Y = 0.2;
const SHADE_BASE_OPACITY = 0.03;
const SHADE_TURN_OPACITY = 0.11;
const HIGHLIGHT_SHIFT_X = 2;
const HIGHLIGHT_SHIFT_Y = 1;
const BLINK_DUR = "6s";
const BLINK_KEY_TIMES = "0;0.3;0.38;0.6;0.64;0.7;0.84;0.87;0.89;0.91;0.94;1";
const BLINK_HEIGHT_VALUES = "9;9;9;9;3.1;9;9;1.2;9;1.2;9;9";
const BLINK_Y_VALUES = "65;65;65;65;68;65;65;68.9;65;68.9;65;65";
const BLINK_GLINT_VALUES = "0.28;0.28;0.28;0.28;0.12;0.28;0.28;0;0.28;0;0.28;0.28";
const MOUTH_BLINK_DUR = "7.2s";
const MOUTH_BLINK_KEY_TIMES = "0;0.2;0.23;0.255;0.62;0.635;0.65;1";
const MOUTH_BLINK_D_VALUES =
  "M57 88.7 Q64 90.1 71 88.7;M57 88.7 Q64 90.1 71 88.7;M60 88.9 Q64 89.5 68 88.9;M57 88.7 Q64 90.1 71 88.7;M57 88.7 Q64 90.1 71 88.7;M58.5 88.8 Q64 90.3 69.5 88.8;M57 88.7 Q64 90.1 71 88.7;M57 88.7 Q64 90.1 71 88.7";
const MOUTH_BLINK_WIDTH_VALUES = "3.5;3.5;3;3.5;3.5;3.3;3.5;3.5";
const CLICK_ANIMATION_DURATION_MS = 2200;
const CLICK_KEY_TIMES = {
  start: 0,
  hit: 0.16,
  rebound: 0.32,
  echo1: 0.48,
  echo2: 0.62,
  echo3: 0.76,
  settle: 0.9,
  end: 1,
};
const DEFAULT_GAZE: MascotGaze = { type: "pointer" };

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

const headGestureStyle: CSSProperties = {
  ...absoluteLayerStyle,
  transform: "translateX(0px) rotateZ(0deg)",
  transformOrigin: `50% ${HEAD_ORIGIN_Y_PERCENT}%`,
  transformStyle: "preserve-3d",
  willChange: "transform",
};

const headStyle: CSSProperties = {
  ...absoluteLayerStyle,
  transform: "translate(0px, 0px) rotateY(0deg) rotateX(0deg) rotateZ(0deg)",
  transformOrigin: `50% ${HEAD_ORIGIN_Y_PERCENT}%`,
  transformStyle: "preserve-3d",
  willChange: "transform",
};

const faceLayerStyle: CSSProperties = {
  ...absoluteLayerStyle,
  transform: `translate3d(0px, 0px, ${FACE_Z_OFFSET_PX}px) rotateY(0deg) rotateX(0deg)`,
  transformOrigin: `${(FACE_CENTER_X / 128) * 100}% ${(FACE_CENTER_Y / 128) * 100}%`,
  transformStyle: "preserve-3d",
  willChange: "transform",
  zIndex: 1,
};

export function Mascot({
  className,
  gaze = DEFAULT_GAZE,
  inputPitchBias = 0,
  mood = "idle",
  onPointerGaze,
  showFaceDebugFrame = false,
  showHeadDebugFrame = false,
}: MascotProps) {
  const rootRef = useRef<HTMLSpanElement>(null);
  const headGestureRef = useRef<HTMLSpanElement>(null);
  const headRef = useRef<HTMLSpanElement>(null);
  const faceLayerRef = useRef<HTMLSpanElement>(null);
  const faceRef = useRef<SVGGElement>(null);
  const shadowRef = useRef<SVGEllipseElement>(null);
  const highlightRef = useRef<SVGRectElement>(null);
  const leftShadeRef = useRef<SVGPathElement>(null);
  const rightShadeRef = useRef<SVGPathElement>(null);
  const currentXRef = useRef(0);
  const currentYRef = useRef(0);
  const targetXRef = useRef(0);
  const targetYRef = useRef(0);
  const rafRef = useRef(0);
  const pressAnimationRef = useRef<Animation | null>(null);
  const inputPitchBiasRef = useRef(inputPitchBias);
  const onPointerGazeRef = useRef(onPointerGaze);
  const id = useId().replace(/:/g, "");
  const faceID = `mascot-face-${id}`;
  const highlightID = `mascot-highlight-${id}`;

  const applyPose = (x: number, y: number) => {
    const turnX = x / EYE_MAX_X;
    const turnY = y / EYE_MAX_Y;
    const pitchTurnY = clamp(turnY + inputPitchBiasRef.current, -1, 1);
    const head = headRef.current;
    const faceLayer = faceLayerRef.current;
    const face = faceRef.current;
    const shadow = shadowRef.current;
    const highlight = highlightRef.current;
    const leftShade = leftShadeRef.current;
    const rightShade = rightShadeRef.current;

    if (head) {
      head.style.transform = `translate(${turnX * HEAD_SHIFT_X_PERCENT}%, ${pitchTurnY * HEAD_SHIFT_Y_PERCENT}%) rotateY(${turnX * HEAD_YAW_MAX_DEG}deg) rotateX(${-pitchTurnY * HEAD_PITCH_MAX_DEG}deg) rotateZ(${turnX * pitchTurnY * HEAD_ROLL_MAX_DEG}deg)`;
    }
    if (faceLayer) {
      faceLayer.style.transform = `translate3d(0px, 0px, ${FACE_Z_OFFSET_PX}px) rotateY(${turnX * FACE_EXTRA_YAW_DEG}deg) rotateX(${-pitchTurnY * FACE_EXTRA_PITCH_DEG}deg)`;
    }
    if (face) {
      face.setAttribute("transform", faceTransform(turnX, turnY));
    }
    if (shadow) {
      shadow.setAttribute("cx", `${64 + turnX * SHADOW_SHIFT_X}`);
      shadow.setAttribute("rx", `${34 - Math.abs(turnX) * SHADOW_SQUEEZE_X}`);
      shadow.setAttribute("ry", `${7 + Math.max(turnY, 0) * SHADOW_SCALE_Y}`);
    }
    if (highlight) {
      highlight.setAttribute("transform", `translate(${turnX * HIGHLIGHT_SHIFT_X} ${turnY * HIGHLIGHT_SHIFT_Y})`);
      highlight.setAttribute("opacity", `${1 - Math.max(turnY, 0) * 0.14}`);
    }
    if (leftShade) {
      leftShade.setAttribute("opacity", `${SHADE_BASE_OPACITY + Math.max(-turnX, 0) * SHADE_TURN_OPACITY}`);
    }
    if (rightShade) {
      rightShade.setAttribute("opacity", `${SHADE_BASE_OPACITY + Math.max(turnX, 0) * SHADE_TURN_OPACITY}`);
    }
  };

  const startTick = () => {
    if (!rafRef.current) {
      rafRef.current = window.requestAnimationFrame(tick);
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

  const setTarget = (x: number, y: number) => {
    targetXRef.current = clamp(x, -EYE_MAX_X, EYE_MAX_X);
    targetYRef.current = clamp(y, -EYE_MAX_Y, EYE_MAX_Y);
    startTick();
  };

  const setTargetFromPoint = (clientX: number, clientY: number) => {
    const rect = rootRef.current?.getBoundingClientRect();
    if (!rect) return;

    const cx = rect.left + rect.width / 2;
    const cy = rect.top + rect.height / 2;
    const dx = clientX - cx;
    const dy = clientY - cy;
    const distance = Math.hypot(dx, dy);
    if (distance < 1) {
      setTarget(0, 0);
      return;
    }

    const sat = Math.max(rect.width, rect.height, POINTER_SAT_MIN_PX) * SAT_RATIO;
    const k = Math.min(distance / sat, 1);
    setTarget((dx / distance) * k * EYE_MAX_X, (dy / distance) * k * EYE_MAX_Y);
  };

  useEffect(() => {
    if (gaze.type === "input") {
      setTargetFromPoint(gaze.target.clientX, gaze.target.clientY);
      return;
    }
    if (gaze.type === "center") {
      setTarget(0, 0);
    }
  }, [gaze]);

  useEffect(() => {
    inputPitchBiasRef.current = gaze.type === "input" ? inputPitchBias : 0;
    applyPose(currentXRef.current, currentYRef.current);
  }, [gaze.type, inputPitchBias]);

  useEffect(() => {
    onPointerGazeRef.current = onPointerGaze;
  }, [onPointerGaze]);

  useLayoutEffect(() => {
    const onPointerMove = (event: PointerEvent) => {
      onPointerGazeRef.current?.();
      setTargetFromPoint(event.clientX, event.clientY);
    };
    const onMouseMove = (event: MouseEvent) => {
      onPointerGazeRef.current?.();
      setTargetFromPoint(event.clientX, event.clientY);
    };

    applyPose(0, 0);
    window.addEventListener("pointermove", onPointerMove, { capture: true, passive: true });
    window.addEventListener("mousemove", onMouseMove, { capture: true, passive: true });
    return () => {
      if (rafRef.current) window.cancelAnimationFrame(rafRef.current);
      window.removeEventListener("pointermove", onPointerMove, { capture: true });
      window.removeEventListener("mousemove", onMouseMove, { capture: true });
    };
  }, []);

  const animateLayer = (element: HTMLElement | null, keyframes: Keyframe[], options: KeyframeAnimationOptions) => {
    if (!element) return;
    pressAnimationRef.current?.cancel();
    pressAnimationRef.current = element.animate(keyframes, options);
  };

  const bounce = () => {
    animateLayer(
      rootRef.current,
      [
        { offset: CLICK_KEY_TIMES.start, transform: "translateY(0%) rotateZ(0deg) scale(1, 1)" },
        { offset: CLICK_KEY_TIMES.hit, transform: "translateY(2.2%) rotateZ(-1.1deg) scale(1.13, 0.908)" },
        { offset: CLICK_KEY_TIMES.rebound, transform: "translateY(-2.5%) rotateZ(0.85deg) scale(0.924, 1.074)" },
        { offset: CLICK_KEY_TIMES.echo1, transform: "translateY(0.8%) rotateZ(-0.52deg) scale(1.058, 0.966)" },
        { offset: CLICK_KEY_TIMES.echo2, transform: "translateY(-1%) rotateZ(0.32deg) scale(0.958, 1.04)" },
        { offset: CLICK_KEY_TIMES.echo3, transform: "translateY(0.4%) rotateZ(-0.24deg) scale(1.032, 0.978)" },
        { offset: CLICK_KEY_TIMES.settle, transform: "translateY(-0.4%) rotateZ(0.13deg) scale(0.986, 1.014)" },
        { offset: CLICK_KEY_TIMES.end, transform: "translateY(0%) rotateZ(0deg) scale(1, 1)" },
      ],
      {
        duration: CLICK_ANIMATION_DURATION_MS,
        easing: "cubic-bezier(0.2, 0.85, 0.18, 1)",
      },
    );
  };

  const wobble = () => {
    const direction = Math.random() < 0.5 ? -1 : 1;
    animateLayer(
      headGestureRef.current,
      [
        { offset: CLICK_KEY_TIMES.start, transform: "translateX(0%) rotateZ(0deg)" },
        { offset: CLICK_KEY_TIMES.hit, transform: `translateX(${direction * -2.4}%) rotateZ(${direction * -9}deg)` },
        { offset: CLICK_KEY_TIMES.rebound, transform: `translateX(${direction * 2.1}%) rotateZ(${direction * 7.5}deg)` },
        { offset: CLICK_KEY_TIMES.echo1, transform: `translateX(${direction * -1.6}%) rotateZ(${direction * -5.6}deg)` },
        { offset: CLICK_KEY_TIMES.echo2, transform: `translateX(${direction * 1}%) rotateZ(${direction * 3.5}deg)` },
        { offset: CLICK_KEY_TIMES.echo3, transform: `translateX(${direction * -0.4}%) rotateZ(${direction * -1.4}deg)` },
        { offset: CLICK_KEY_TIMES.settle, transform: `translateX(${direction * 0.15}%) rotateZ(${direction * 0.5}deg)` },
        { offset: CLICK_KEY_TIMES.end, transform: "translateX(0%) rotateZ(0deg)" },
      ],
      {
        duration: CLICK_ANIMATION_DURATION_MS,
        easing: "cubic-bezier(0.2, 0.8, 0.2, 1)",
      },
    );
  };

  const playClickAnimation = () => {
    if (Math.random() < 0.5) {
      bounce();
      return;
    }
    wobble();
  };

  return (
    <span
      ref={rootRef}
      aria-hidden="true"
      className={className}
      data-gaze={gaze.type}
      data-mood={mood}
      style={rootStyle}
      onPointerDown={playClickAnimation}
    >
      <svg data-slot="mascot-base" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
        <g data-slot="shadow">
          <ellipse ref={shadowRef} cx="64" cy="113" fill="var(--mascot-shadow)" rx="34" ry="7" />
        </g>
      </svg>

      <span data-slot="head-gesture" ref={headGestureRef} style={headGestureStyle}>
        <span data-slot="head" ref={headRef} style={headStyle}>
          <svg data-slot="head-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
          <defs>
            <linearGradient id={faceID} x1="0" x2="0" y1="0" y2="1">
              <stop offset="0%" stopColor="var(--mascot-body-top)" />
              <stop offset="55%" stopColor="var(--mascot-body-mid)" />
              <stop offset="100%" stopColor="var(--mascot-body-bottom)" />
            </linearGradient>
            <linearGradient id={highlightID} x1="0" x2="0" y1="0" y2="1">
              <stop offset="0%" stopColor="var(--mascot-highlight)" />
              <stop offset="100%" stopColor="#ffffff" stopOpacity="0" />
            </linearGradient>
          </defs>

          <g data-slot="arms">
            <g data-slot="arm-left">
              <animateTransform
                attributeName="transform"
                calcMode="spline"
                dur="3.4s"
                keySplines="0.42 0 0.58 1;0.42 0 0.58 1"
                keyTimes="0;0.5;1"
                repeatCount="indefinite"
                type="translate"
                values="0 0;0 -3;0 0"
              />
              <path
                data-slot="arm-left-limb"
                d="M30 85 C21 90 16 98 16 105"
                fill="none"
                stroke="var(--mascot-limb)"
                strokeLinecap="round"
                strokeWidth="5.5"
              />
              <circle
                data-slot="arm-left-palm"
                cx="16"
                cy="106"
                fill="var(--mascot-limb)"
                r="7"
              />
            </g>
            <g data-slot="arm-right">
              <animateTransform
                attributeName="transform"
                begin="0.12s"
                calcMode="spline"
                dur="3.4s"
                keySplines="0.42 0 0.58 1;0.42 0 0.58 1"
                keyTimes="0;0.5;1"
                repeatCount="indefinite"
                type="translate"
                values="0 0;0 -2.7;0 0"
              />
              <path
                data-slot="arm-right-limb"
                d="M98 85 C107 90 112 98 112 105"
                fill="none"
                stroke="var(--mascot-limb)"
                strokeLinecap="round"
                strokeWidth="5.5"
              />
              <circle
                data-slot="arm-right-palm"
                cx="112"
                cy="106"
                fill="var(--mascot-limb)"
                r="7"
              />
            </g>
          </g>

          <g data-slot="antenna">
            <animateTransform
              attributeName="transform"
              begin="0.18s"
              calcMode="spline"
              dur="3.6s"
              keySplines="0.42 0 0.58 1;0.42 0 0.58 1"
              keyTimes="0;0.5;1"
              repeatCount="indefinite"
              type="rotate"
              values="-3 64 34;4 64 34;-3 64 34"
            />
            <path
              d="M64 34 C65 28 63 23 60 18"
              fill="none"
              stroke="var(--mascot-antenna-stroke)"
              strokeLinecap="round"
              strokeWidth="4"
            />
            <circle
              cx="59"
              cy="16"
              fill="var(--mascot-antenna)"
              r="6"
              stroke="var(--mascot-antenna-stroke)"
              strokeWidth="2"
            />
          </g>

          <g data-slot="body">
            <rect fill={`url(#${faceID})`} height="76" rx="24" width="86" x="21" y="34" />
            <rect ref={highlightRef} fill={`url(#${highlightID})`} height="34" rx="18" width="74" x="27" y="39" />
            <path
              ref={leftShadeRef}
              d="M31 45 C26 51 24 59 24 70 V86 C24 98 35 108 48 110 C36 103 31 93 31 79 Z"
              fill="var(--mascot-side-shade)"
              opacity={SHADE_BASE_OPACITY}
            />
            <path
              ref={rightShadeRef}
              d="M97 45 C102 51 104 59 104 70 V86 C104 98 93 108 80 110 C92 103 97 93 97 79 Z"
              fill="var(--mascot-side-shade)"
              opacity={SHADE_BASE_OPACITY}
            />
          </g>

          {showHeadDebugFrame ? (
            <rect
              data-slot="head-debug-frame"
              fill="none"
              height="76"
              rx="24"
              stroke="#00d5ff"
              strokeDasharray="3 3"
              strokeWidth="2"
              vectorEffect="non-scaling-stroke"
              width="86"
              x="21"
              y="34"
            />
          ) : null}
          </svg>

          <span data-slot="face-layer" ref={faceLayerRef} style={faceLayerStyle}>
            <svg data-slot="face-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
            <g ref={faceRef} data-slot="face" transform="translate(0 0)">
              {showFaceDebugFrame ? (
                <rect
                  data-slot="face-debug-frame"
                  fill="none"
                  height="38"
                  rx="6"
                  stroke="#ff7a00"
                  strokeDasharray="3 3"
                  strokeWidth="2"
                  vectorEffect="non-scaling-stroke"
                  width="52"
                  x="38"
                  y="58"
                />
              ) : null}
              <g data-slot="eyes" fill="#ffffff">
                <rect data-slot="eye-left" height="9" rx="3" width="15" x="43" y="65">
                  <animate attributeName="height" dur={BLINK_DUR} keyTimes={BLINK_KEY_TIMES} repeatCount="indefinite" values={BLINK_HEIGHT_VALUES} />
                  <animate attributeName="y" dur={BLINK_DUR} keyTimes={BLINK_KEY_TIMES} repeatCount="indefinite" values={BLINK_Y_VALUES} />
                </rect>
                <rect data-slot="eye-right" height="9" rx="3" width="15" x="70" y="65">
                  <animate attributeName="height" dur={BLINK_DUR} keyTimes={BLINK_KEY_TIMES} repeatCount="indefinite" values={BLINK_HEIGHT_VALUES} />
                  <animate attributeName="y" dur={BLINK_DUR} keyTimes={BLINK_KEY_TIMES} repeatCount="indefinite" values={BLINK_Y_VALUES} />
                </rect>
              </g>
              <g data-slot="eye-glints" fill="#ffffff" opacity="0.28">
                <animate attributeName="opacity" dur={BLINK_DUR} keyTimes={BLINK_KEY_TIMES} repeatCount="indefinite" values={BLINK_GLINT_VALUES} />
                <rect height="1.4" rx="0.7" width="3.2" x="46" y="67" />
                <rect height="1.4" rx="0.7" width="3.2" x="73" y="67" />
              </g>
              <path
                data-slot="mouth"
                d="M57 88.7 Q64 90.1 71 88.7"
                fill="none"
                stroke="#ffffff"
                strokeLinecap="round"
                strokeWidth="3.5"
              >
                <animate
                  attributeName="d"
                  begin="1.1s"
                  dur={MOUTH_BLINK_DUR}
                  keyTimes={MOUTH_BLINK_KEY_TIMES}
                  repeatCount="indefinite"
                  values={MOUTH_BLINK_D_VALUES}
                />
                <animate
                  attributeName="stroke-width"
                  begin="1.1s"
                  dur={MOUTH_BLINK_DUR}
                  keyTimes={MOUTH_BLINK_KEY_TIMES}
                  repeatCount="indefinite"
                  values={MOUTH_BLINK_WIDTH_VALUES}
                />
              </path>
            </g>
            </svg>
          </span>
        </span>
      </span>
    </span>
  );
}

function faceTransform(turnX: number, turnY: number): string {
  const scaleX = 1 - Math.abs(turnX) * FACE_EXTRA_SQUEEZE_X;
  const scaleY = 1 + turnY * FACE_EXTRA_SCALE_Y;
  const skewX = -turnX * FACE_EXTRA_SKEW_DEG;

  return [
    `translate(${turnX * FACE_SHIFT_X} ${turnY * FACE_SHIFT_Y})`,
    `translate(${FACE_CENTER_X} ${FACE_CENTER_Y})`,
    `skewX(${skewX})`,
    `scale(${scaleX} ${scaleY})`,
    `translate(${-FACE_CENTER_X} ${-FACE_CENTER_Y})`,
  ].join(" ");
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}
