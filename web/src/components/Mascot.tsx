import { useEffect, useId, useLayoutEffect, useRef, type CSSProperties } from "react";

type MascotProps = {
  className?: string;
  gaze?: MascotGaze;
  inputPitchBias?: number;
  mood?: MascotMood;
  headShakeSignal?: number;
  ambientMotion?: boolean;
  pointerTracking?: boolean;
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
const INPUT_SAT_RATIO = 0.75;
const INPUT_SAT_MIN_PX = 64;
const FOLLOW_EASE = 0.22;
const FOLLOW_EPSILON = 0.01;
const INPUT_PITCH_BIAS_EASE = 0.12;
const INPUT_PITCH_BIAS_EPSILON = 0.005;
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
const EYE_DEPTH_SCALE = 0.08;
const SHADOW_SHIFT_X = 0.7;
const SHADOW_SQUEEZE_X = 0.45;
const SHADOW_SCALE_Y = 0.2;
const SHADE_BASE_OPACITY = 0.03;
const SHADE_TURN_OPACITY = 0.11;
const HIGHLIGHT_SHIFT_X = 2;
const HIGHLIGHT_SHIFT_Y = 1;
const CLICK_ANIMATION_DURATION_MS = 2200;
// Standard error gesture: face forward, lower the head slightly, then shake no.
const HEAD_SHAKE_NO_DURATION_MS = 600;
const HEAD_SHAKE_NO_X = EYE_MAX_X * 0.6;
const HEAD_SHAKE_NO_Y = EYE_MAX_Y * 0.23;
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
  position: "relative",
};

// Keep the animated SVG damage inside a small compositor surface. The extra
// margin preserves the head tilt and arm motion without letting the mascot's
// visible overflow invalidate unrelated hover surfaces across the app.
const paintBoundsStyle: CSSProperties = {
  contain: "layout style paint",
  inset: "-25%",
  isolation: "isolate",
  overflow: "hidden",
  pointerEvents: "none",
  position: "absolute",
  transform: "translateZ(0)",
};

const artViewportStyle: CSSProperties = {
  height: "66.666667%",
  left: "16.666667%",
  overflow: "visible",
  perspective: `${HEAD_PERSPECTIVE_PX}px`,
  perspectiveOrigin: `50% ${HEAD_ORIGIN_Y_PERCENT}%`,
  position: "absolute",
  top: "16.666667%",
  width: "66.666667%",
};

const headGestureStyle: CSSProperties = {
  ...absoluteLayerStyle,
  transform: "translateX(0px) rotateZ(0deg)",
  transformOrigin: `50% ${HEAD_ORIGIN_Y_PERCENT}%`,
  transformStyle: "preserve-3d",
};

const headStyle: CSSProperties = {
  ...absoluteLayerStyle,
  transform: "translate(0px, 0px) rotateY(0deg) rotateX(0deg) rotateZ(0deg)",
  transformOrigin: `50% ${HEAD_ORIGIN_Y_PERCENT}%`,
  transformStyle: "preserve-3d",
};

const faceLayerStyle: CSSProperties = {
  ...absoluteLayerStyle,
  transform: `translate3d(0px, 0px, ${FACE_Z_OFFSET_PX}px) rotateY(0deg) rotateX(0deg)`,
  transformOrigin: `${(FACE_CENTER_X / 128) * 100}% ${(FACE_CENTER_Y / 128) * 100}%`,
  transformStyle: "preserve-3d",
  zIndex: 1,
};

const compositeLayerStyle: CSSProperties = {
  ...absoluteLayerStyle,
  pointerEvents: "none",
};

const shadowMotionStyle: CSSProperties = {
  ...compositeLayerStyle,
  transformOrigin: "50% 88.28125%",
};

const antennaMotionStyle: CSSProperties = {
  ...compositeLayerStyle,
  transformOrigin: "50% 26.5625%",
};

const faceMotionStyle: CSSProperties = {
  ...compositeLayerStyle,
  transformOrigin: `${(FACE_CENTER_X / 128) * 100}% ${(FACE_CENTER_Y / 128) * 100}%`,
};

const eyeMotionStyle: CSSProperties = {
  ...compositeLayerStyle,
  transformOrigin: "50% 54.296875%",
};

const individualEyeStyle: CSSProperties = {
  transformBox: "fill-box",
  transformOrigin: "center",
};

const mouthMotionStyle: CSSProperties = {
  ...compositeLayerStyle,
  transformOrigin: "50% 69.53125%",
};

type MascotMetrics = {
  centerX: number;
  centerY: number;
  size: number;
};

export function Mascot({
  className,
  gaze = DEFAULT_GAZE,
  inputPitchBias = 0,
  mood = "idle",
  headShakeSignal = 0,
  ambientMotion = true,
  pointerTracking = true,
  onPointerGaze,
  showFaceDebugFrame = false,
  showHeadDebugFrame = false,
}: MascotProps) {
  const motionEnabled = ambientMotion || pointerTracking;
  const rootRef = useRef<HTMLSpanElement>(null);
  const headGestureRef = useRef<HTMLSpanElement>(null);
  const headRef = useRef<HTMLSpanElement>(null);
  const faceLayerRef = useRef<HTMLSpanElement>(null);
  const faceMotionRef = useRef<HTMLSpanElement>(null);
  const screenLeftEyeRef = useRef<SVGGElement>(null);
  const screenRightEyeRef = useRef<SVGGElement>(null);
  const screenLeftGlintRef = useRef<SVGGElement>(null);
  const screenRightGlintRef = useRef<SVGGElement>(null);
  const shadowMotionRef = useRef<HTMLSpanElement>(null);
  const highlightMotionRef = useRef<HTMLSpanElement>(null);
  const leftShadeRef = useRef<HTMLSpanElement>(null);
  const rightShadeRef = useRef<HTMLSpanElement>(null);
  const metricsRef = useRef<MascotMetrics | null>(null);
  const currentXRef = useRef(0);
  const currentYRef = useRef(0);
  const targetXRef = useRef(0);
  const targetYRef = useRef(0);
  const rafRef = useRef(0);
  const headShakeRafRef = useRef(0);
  const pressAnimationRef = useRef<Animation | null>(null);
  const currentInputPitchBiasRef = useRef(gaze.type === "input" ? inputPitchBias : 0);
  const targetInputPitchBiasRef = useRef(gaze.type === "input" ? inputPitchBias : 0);
  const gazeLockedRef = useRef(gaze.type === "center" || mood === "error");
  const gazeTypeRef = useRef(gaze.type);
  const onPointerGazeRef = useRef(onPointerGaze);
  const id = useId().replace(/:/g, "");
  const faceID = `mascot-face-${id}`;
  const highlightID = `mascot-highlight-${id}`;

  const applyPose = (x: number, y: number) => {
    const turnX = x / EYE_MAX_X;
    const turnY = y / EYE_MAX_Y;
    const pitchTurnY = clamp(turnY + currentInputPitchBiasRef.current, -1, 1);
    const head = headRef.current;
    const faceLayer = faceLayerRef.current;
    const faceMotion = faceMotionRef.current;
    const screenLeftEye = screenLeftEyeRef.current;
    const screenRightEye = screenRightEyeRef.current;
    const screenLeftGlint = screenLeftGlintRef.current;
    const screenRightGlint = screenRightGlintRef.current;
    const shadowMotion = shadowMotionRef.current;
    const highlightMotion = highlightMotionRef.current;
    const leftShade = leftShadeRef.current;
    const rightShade = rightShadeRef.current;

    if (head) {
      head.style.transform = `translate3d(${turnX * HEAD_SHIFT_X_PERCENT}%, ${pitchTurnY * HEAD_SHIFT_Y_PERCENT}%, 0) rotateY(${turnX * HEAD_YAW_MAX_DEG}deg) rotateX(${-pitchTurnY * HEAD_PITCH_MAX_DEG}deg) rotateZ(${turnX * pitchTurnY * HEAD_ROLL_MAX_DEG}deg)`;
    }
    if (faceLayer) {
      faceLayer.style.transform = `translate3d(0px, 0px, ${FACE_Z_OFFSET_PX}px) rotateY(${turnX * FACE_EXTRA_YAW_DEG}deg) rotateX(${-pitchTurnY * FACE_EXTRA_PITCH_DEG}deg)`;
    }
    if (faceMotion) {
      const scaleX = 1 - Math.abs(turnX) * FACE_EXTRA_SQUEEZE_X;
      const scaleY = 1 + pitchTurnY * FACE_EXTRA_SCALE_Y;
      faceMotion.style.transform = `translate3d(${turnX * (FACE_SHIFT_X / 128) * 100}%, ${pitchTurnY * (FACE_SHIFT_Y / 128) * 100}%, 0) skewX(${-turnX * FACE_EXTRA_SKEW_DEG}deg) scale(${scaleX}, ${scaleY})`;
    }
    const screenLeftEyeScale = 1 + turnX * EYE_DEPTH_SCALE;
    const screenRightEyeScale = 1 - turnX * EYE_DEPTH_SCALE;
    if (screenLeftEye) screenLeftEye.style.transform = `scale(${screenLeftEyeScale})`;
    if (screenRightEye) screenRightEye.style.transform = `scale(${screenRightEyeScale})`;
    if (screenLeftGlint) screenLeftGlint.style.transform = `scale(${screenLeftEyeScale})`;
    if (screenRightGlint) screenRightGlint.style.transform = `scale(${screenRightEyeScale})`;
    if (shadowMotion) {
      const scaleX = 1 - (Math.abs(turnX) * SHADOW_SQUEEZE_X) / 34;
      const scaleY = 1 + (Math.max(pitchTurnY, 0) * SHADOW_SCALE_Y) / 7;
      shadowMotion.style.transform = `translate3d(${turnX * (SHADOW_SHIFT_X / 128) * 100}%, 0, 0) scale(${scaleX}, ${scaleY})`;
    }
    if (highlightMotion) {
      highlightMotion.style.transform = `translate3d(${turnX * (HIGHLIGHT_SHIFT_X / 128) * 100}%, ${pitchTurnY * (HIGHLIGHT_SHIFT_Y / 128) * 100}%, 0)`;
      highlightMotion.style.opacity = `${1 - Math.max(pitchTurnY, 0) * 0.14}`;
    }
    if (leftShade) {
      leftShade.style.opacity = `${SHADE_BASE_OPACITY + Math.max(-turnX, 0) * SHADE_TURN_OPACITY}`;
    }
    if (rightShade) {
      rightShade.style.opacity = `${SHADE_BASE_OPACITY + Math.max(turnX, 0) * SHADE_TURN_OPACITY}`;
    }
  };

  const startTick = () => {
    if (!rafRef.current) {
      rafRef.current = window.requestAnimationFrame(tick);
    }
  };

  const tick = () => {
    rafRef.current = 0;
    const currentX = currentXRef.current;
    const currentY = currentYRef.current;
    const targetX = targetXRef.current;
    const targetY = targetYRef.current;
    const currentInputPitchBias = currentInputPitchBiasRef.current;
    const targetInputPitchBias = targetInputPitchBiasRef.current;
    const nextX = currentX + (targetX - currentX) * FOLLOW_EASE;
    const nextY = currentY + (targetY - currentY) * FOLLOW_EASE;
    const nextInputPitchBias =
      currentInputPitchBias + (targetInputPitchBias - currentInputPitchBias) * INPUT_PITCH_BIAS_EASE;
    currentXRef.current = Math.abs(nextX - targetX) < FOLLOW_EPSILON ? targetX : nextX;
    currentYRef.current = Math.abs(nextY - targetY) < FOLLOW_EPSILON ? targetY : nextY;
    currentInputPitchBiasRef.current =
      Math.abs(nextInputPitchBias - targetInputPitchBias) < INPUT_PITCH_BIAS_EPSILON
        ? targetInputPitchBias
        : nextInputPitchBias;
    applyPose(currentXRef.current, currentYRef.current);

    if (
      Math.abs(currentXRef.current - targetX) > FOLLOW_EPSILON ||
      Math.abs(currentYRef.current - targetY) > FOLLOW_EPSILON ||
      Math.abs(currentInputPitchBiasRef.current - targetInputPitchBias) > INPUT_PITCH_BIAS_EPSILON
    ) {
      startTick();
      return;
    }
  };

  const setTarget = (x: number, y: number) => {
    targetXRef.current = clamp(x, -EYE_MAX_X, EYE_MAX_X);
    targetYRef.current = clamp(y, -EYE_MAX_Y, EYE_MAX_Y);
    startTick();
  };

  const readMetrics = () => {
    const rect = rootRef.current?.getBoundingClientRect();
    if (!rect) return null;
    const metrics = {
      centerX: rect.left + rect.width / 2,
      centerY: rect.top + rect.height / 2,
      size: Math.max(rect.width, rect.height),
    };
    metricsRef.current = metrics;
    return metrics;
  };

  const setTargetFromPoint = (
    clientX: number,
    clientY: number,
    satRatio = SAT_RATIO,
    satMinPx = POINTER_SAT_MIN_PX,
  ) => {
    // Layout panels can move the mascot without resizing it. Refresh its viewport
    // position for every new target so sidebar and split-pane changes cannot leave
    // the gaze calculation using a stale center point.
    const metrics = readMetrics();
    if (!metrics) return;

    const dx = clientX - metrics.centerX;
    const dy = clientY - metrics.centerY;
    const distance = Math.hypot(dx, dy);
    if (distance < 1) {
      setTarget(0, 0);
      return;
    }

    const sat = Math.max(metrics.size, satMinPx) * satRatio;
    const k = Math.min(distance / sat, 1);
    setTarget((dx / distance) * k * EYE_MAX_X, (dy / distance) * k * EYE_MAX_Y);
  };

  useEffect(() => {
    gazeTypeRef.current = gaze.type;
    if (!motionEnabled) {
      return;
    }
    if (gaze.type === "input") {
      setTargetFromPoint(gaze.target.clientX, gaze.target.clientY, INPUT_SAT_RATIO, INPUT_SAT_MIN_PX);
      return;
    }
    if (gaze.type === "center") {
      setTarget(0, 0);
    }
  }, [gaze, motionEnabled]);

  useEffect(() => {
    if (!motionEnabled) {
      return;
    }
    targetInputPitchBiasRef.current = gaze.type === "input" ? inputPitchBias : 0;
    startTick();
  }, [gaze.type, inputPitchBias, motionEnabled]);

  useEffect(() => {
    onPointerGazeRef.current = onPointerGaze;
  }, [onPointerGaze]);

  useEffect(() => {
    gazeLockedRef.current = gaze.type === "center" || mood === "error";
  }, [gaze.type, mood]);

  useLayoutEffect(() => {
    const updateMetrics = () => {
      metricsFrame = 0;
      readMetrics();
    };
    let metricsFrame = 0;
    const scheduleMetricsUpdate = () => {
      if (metricsFrame) return;
      metricsFrame = window.requestAnimationFrame(updateMetrics);
    };

    updateMetrics();
    applyPose(0, 0);
    if (!motionEnabled) {
      return;
    }
    const resizeObserver = new ResizeObserver(scheduleMetricsUpdate);
    if (rootRef.current) resizeObserver.observe(rootRef.current);
    if (rootRef.current?.parentElement) resizeObserver.observe(rootRef.current.parentElement);
    window.addEventListener("resize", scheduleMetricsUpdate, { passive: true });
    window.addEventListener("scroll", scheduleMetricsUpdate, { capture: true, passive: true });

    if (!pointerTracking) {
      return () => {
        resizeObserver.disconnect();
        if (metricsFrame) window.cancelAnimationFrame(metricsFrame);
        window.removeEventListener("resize", scheduleMetricsUpdate);
        window.removeEventListener("scroll", scheduleMetricsUpdate, { capture: true });
      };
    }
    const onPointerMove = (event: PointerEvent) => {
      if (gazeLockedRef.current) {
        return;
      }
      if (gazeTypeRef.current !== "pointer") {
        gazeTypeRef.current = "pointer";
        onPointerGazeRef.current?.();
      }
      setTargetFromPoint(event.clientX, event.clientY);
    };

    window.addEventListener("pointermove", onPointerMove, { capture: true, passive: true });
    return () => {
      if (rafRef.current) {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = 0;
      }
      if (headShakeRafRef.current) {
        window.cancelAnimationFrame(headShakeRafRef.current);
        headShakeRafRef.current = 0;
      }
      if (metricsFrame) window.cancelAnimationFrame(metricsFrame);
      window.removeEventListener("pointermove", onPointerMove, { capture: true });
      resizeObserver.disconnect();
      window.removeEventListener("resize", scheduleMetricsUpdate);
      window.removeEventListener("scroll", scheduleMetricsUpdate, { capture: true });
    };
  }, [motionEnabled, pointerTracking]);

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

  const centerPoseImmediately = () => {
    targetXRef.current = 0;
    targetYRef.current = HEAD_SHAKE_NO_Y;
    currentXRef.current = 0;
    currentYRef.current = HEAD_SHAKE_NO_Y;
    targetInputPitchBiasRef.current = 0;
    currentInputPitchBiasRef.current = 0;
    applyPose(0, HEAD_SHAKE_NO_Y);
  };

  const playHeadShakeNo = () => {
    centerPoseImmediately();
    pressAnimationRef.current?.cancel();
    pressAnimationRef.current = null;
    if (headShakeRafRef.current) {
      window.cancelAnimationFrame(headShakeRafRef.current);
      headShakeRafRef.current = 0;
    }
    const startedAt = performance.now();
    const step = (now: number) => {
      const progress = Math.min((now - startedAt) / HEAD_SHAKE_NO_DURATION_MS, 1);
      const x = headShakeNoXAt(progress) * HEAD_SHAKE_NO_X;
      currentXRef.current = x;
      targetXRef.current = x;
      currentYRef.current = HEAD_SHAKE_NO_Y;
      targetYRef.current = HEAD_SHAKE_NO_Y;
      currentInputPitchBiasRef.current = 0;
      targetInputPitchBiasRef.current = 0;
      applyPose(x, HEAD_SHAKE_NO_Y);
      if (progress < 1) {
        headShakeRafRef.current = window.requestAnimationFrame(step);
        return;
      }
      headShakeRafRef.current = 0;
    };
    headShakeRafRef.current = window.requestAnimationFrame(step);
  };

  const playClickAnimation = () => {
    if (Math.random() < 0.5) {
      bounce();
      return;
    }
    wobble();
  };

  useEffect(() => {
    if (ambientMotion && headShakeSignal > 0) {
      playHeadShakeNo();
    }
  }, [ambientMotion, headShakeSignal]);

  return (
    <span
      ref={rootRef}
      aria-hidden="true"
      className={className}
      data-gaze={gaze.type}
      data-ambient-motion={ambientMotion}
      data-motion-enabled={motionEnabled}
      data-pointer-tracking={pointerTracking}
      data-mood={mood}
      style={rootStyle}
      onPointerDown={ambientMotion ? playClickAnimation : undefined}
    >
      <span data-slot="mascot-paint-bounds" style={paintBoundsStyle}>
        <span data-slot="mascot-art-viewport" style={artViewportStyle}>
          <span data-slot="shadow-motion" ref={shadowMotionRef} style={shadowMotionStyle}>
            <svg data-slot="mascot-base" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
              <ellipse cx="64" cy="113" fill="var(--mascot-shadow)" rx="34" ry="7" />
            </svg>
          </span>

          <span data-slot="head-gesture" ref={headGestureRef} style={headGestureStyle}>
            <span data-slot="head" ref={headRef} style={headStyle}>
          <span data-slot="arm-left-motion" style={compositeLayerStyle}>
            <svg data-slot="head-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
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
            </svg>
          </span>

          <span data-slot="arm-right-motion" style={compositeLayerStyle}>
            <svg data-slot="head-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
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
            </svg>
          </span>

          <span data-slot="antenna-motion" style={antennaMotionStyle}>
            <span data-slot="antenna-glow" style={compositeLayerStyle}>
              <svg data-slot="head-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
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
              </svg>
            </span>
          </span>

          <svg data-slot="body-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
            <defs>
              <linearGradient id={faceID} x1="0" x2="0" y1="0" y2="1">
                <stop offset="0%" stopColor="var(--mascot-body-top)" />
                <stop offset="55%" stopColor="var(--mascot-body-mid)" />
                <stop offset="100%" stopColor="var(--mascot-body-bottom)" />
              </linearGradient>
            </defs>
            <rect fill={`url(#${faceID})`} height="76" rx="24" width="86" x="21" y="34" />
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

          <span data-slot="highlight-motion" ref={highlightMotionRef} style={compositeLayerStyle}>
            <svg data-slot="head-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
              <defs>
                <linearGradient id={highlightID} x1="0" x2="0" y1="0" y2="1">
                  <stop offset="0%" stopColor="var(--mascot-highlight)" />
                  <stop offset="100%" stopColor="#ffffff" stopOpacity="0" />
                </linearGradient>
              </defs>
              <rect fill={`url(#${highlightID})`} height="34" rx="18" width="74" x="27" y="39" />
            </svg>
          </span>

          <span data-slot="shade-left" ref={leftShadeRef} style={compositeLayerStyle}>
            <svg data-slot="head-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
              <path
                d="M31 45 C26 51 24 59 24 70 V86 C24 98 35 108 48 110 C36 103 31 93 31 79 Z"
                fill="var(--mascot-side-shade)"
              />
            </svg>
          </span>

          <span data-slot="shade-right" ref={rightShadeRef} style={compositeLayerStyle}>
            <svg data-slot="head-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
              <path
                d="M97 45 C102 51 104 59 104 70 V86 C104 98 93 108 80 110 C92 103 97 93 97 79 Z"
                fill="var(--mascot-side-shade)"
              />
            </svg>
          </span>

          <span data-slot="face-layer" ref={faceLayerRef} style={faceLayerStyle}>
            <span data-slot="face-motion" ref={faceMotionRef} style={faceMotionStyle}>
              {showFaceDebugFrame ? (
                <svg data-slot="face-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
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
                </svg>
              ) : null}

              <span data-slot="eyes" style={eyeMotionStyle}>
                <svg data-slot="face-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
                  <g ref={screenLeftEyeRef} fill="#ffffff" style={individualEyeStyle}>
                    <rect height="9" rx="3" width="15" x="43" y="65" />
                  </g>
                  <g ref={screenRightEyeRef} fill="#ffffff" style={individualEyeStyle}>
                    <rect height="9" rx="3" width="15" x="70" y="65" />
                  </g>
                </svg>
              </span>

              <span data-slot="eye-glints" style={eyeMotionStyle}>
                <svg data-slot="face-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
                  <g ref={screenLeftGlintRef} fill="#ffffff" style={individualEyeStyle}>
                    <rect height="1.4" rx="0.7" width="3.2" x="46" y="67" />
                  </g>
                  <g ref={screenRightGlintRef} fill="#ffffff" style={individualEyeStyle}>
                    <rect height="1.4" rx="0.7" width="3.2" x="73" y="67" />
                  </g>
                </svg>
              </span>

              <span data-slot="eyes-error" style={compositeLayerStyle}>
                <svg data-slot="face-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
                  <g fill="none" stroke="#ffffff" strokeLinecap="round" strokeWidth="4.1">
                    <path d="M47 66.9 L56 70.8 L47 74.7" />
                    <path d="M81 66.9 L72 70.8 L81 74.7" />
                  </g>
                </svg>
              </span>

              <span data-slot="mouth" style={mouthMotionStyle}>
                <svg data-slot="face-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
                  <path
                    d="M57 88.7 Q64 90.1 71 88.7"
                    fill="none"
                    stroke="#ffffff"
                    strokeLinecap="round"
                    strokeWidth="3.5"
                  />
                </svg>
              </span>

              <span data-slot="mouth-error" style={compositeLayerStyle}>
                <svg data-slot="face-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
                  <path
                    d="M58 89.1 Q64 88.1 70 89.1"
                    fill="none"
                    stroke="#ffffff"
                    strokeLinecap="round"
                    strokeWidth="3.2"
                  />
                </svg>
              </span>
            </span>
              </span>
            </span>
          </span>
        </span>
      </span>
    </span>
  );
}

function headShakeNoXAt(progress: number): number {
  const points = [
    { t: 0, x: 0 },
    { t: 0.16, x: -1 },
    { t: 0.4, x: 1 },
    { t: 0.64, x: -1 },
    { t: 0.84, x: 0.36 },
    { t: 1, x: 0 },
  ];
  for (let index = 1; index < points.length; index += 1) {
    const previous = points[index - 1];
    const next = points[index];
    if (progress <= next.t) {
      const rawSegmentProgress = (progress - previous.t) / (next.t - previous.t);
      return previous.x + (next.x - previous.x) * rawSegmentProgress;
    }
  }
  return 0;
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}
