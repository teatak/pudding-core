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
const FACE_SHIFT_X = 4;
const FACE_SHIFT_Y = 2;
const FACE_YAW_SQUEEZE_X = 0.04;
const FACE_YAW_ORIGIN_SHIFT_PERCENT = 6;
const FACE_CENTER_X = 64;
const FACE_CENTER_Y = 77;
const FACE_FRAME_Z_OFFSET_PX = 4;
const FACE_GLASS_INSET_PX = 3;
const BODY_DEPTH_PX = 5;
const BODY_BACK_SHELL_SCALE = (HEAD_PERSPECTIVE_PX + BODY_DEPTH_PX) / HEAD_PERSPECTIVE_PX;
const SHADOW_SHIFT_X = 0.7;
const SHADOW_SQUEEZE_X = 0.45;
const SHADOW_SCALE_Y = 0.2;
const BODY_PATH = "M52 34 C56 34 72 34 76 34 C90 34 102 42 106 56 L108 84 C109 99 99 108 84 109 C71 111 57 111 44 109 C29 108 19 99 20 84 L22 56 C26 42 38 34 52 34 Z";
const LEFT_ARM_PATH = "M30 85 C21 90 13 99 13 106";
const RIGHT_ARM_PATH = "M98 85 C107 90 115 99 115 106";
const LEFT_PALM_X = 14;
const RIGHT_PALM_X = 114;
const PALM_Y = 106;
const MOUTH_PATH = "M59 88.5 Q64 91 69 88.5";
const FACE_SCREEN_PATH = "M40 53 H88 C96 53 100 58 100 66 V88 C100 96 95 100 87 100 H41 C33 100 28 96 28 88 V66 C28 58 32 53 40 53 Z";
const FACE_SCREEN_GROOVE_TRANSFORM = "translate(64 77) scale(0.89) translate(-64 -77)";
const FACE_SCREEN_INSET_TRANSFORM = "translate(64 77) scale(0.85) translate(-64 -77)";
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
  transform: `translate3d(0px, 0px, ${FACE_FRAME_Z_OFFSET_PX}px) rotateY(0deg) rotateX(0deg)`,
  transformOrigin: `${(FACE_CENTER_X / 128) * 100}% ${(FACE_CENTER_Y / 128) * 100}%`,
  transformStyle: "preserve-3d",
  zIndex: 1,
};

const compositeLayerStyle: CSSProperties = {
  ...absoluteLayerStyle,
  pointerEvents: "none",
};

const bodyBackShellStyle: CSSProperties = {
  ...compositeLayerStyle,
  transform: `translateZ(-${BODY_DEPTH_PX}px) scale(${BODY_BACK_SHELL_SCALE})`,
  transformOrigin: `50% ${HEAD_ORIGIN_Y_PERCENT}%`,
  transformStyle: "preserve-3d",
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
  transformStyle: "preserve-3d",
};

const faceGlassStyle: CSSProperties = {
  ...compositeLayerStyle,
  transform: `translateZ(-${FACE_GLASS_INSET_PX}px)`,
  transformStyle: "preserve-3d",
};

const eyeMotionStyle: CSSProperties = {
  ...compositeLayerStyle,
  transformOrigin: "50% 54.296875%",
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
  const bodyMetalGradientID = `mascot-body-metal-${useId().replaceAll(":", "")}`;
  const bodyMetalPaint = `url(#${bodyMetalGradientID})`;
  const rootRef = useRef<HTMLSpanElement>(null);
  const headGestureRef = useRef<HTMLSpanElement>(null);
  const headRef = useRef<HTMLSpanElement>(null);
  const faceMotionRef = useRef<HTMLSpanElement>(null);
  const pupilMotionRef = useRef<SVGGElement>(null);
  const shadowMotionRef = useRef<HTMLSpanElement>(null);
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

  const applyPose = (x: number, y: number) => {
    const turnX = x / EYE_MAX_X;
    const turnY = y / EYE_MAX_Y;
    const pitchTurnY = clamp(turnY + currentInputPitchBiasRef.current, -1, 1);
    const head = headRef.current;
    const faceMotion = faceMotionRef.current;
    const pupilMotion = pupilMotionRef.current;
    const shadowMotion = shadowMotionRef.current;

    if (head) {
      head.style.transform = `translate3d(${turnX * HEAD_SHIFT_X_PERCENT}%, ${pitchTurnY * HEAD_SHIFT_Y_PERCENT}%, 0) rotateY(${turnX * HEAD_YAW_MAX_DEG}deg) rotateX(${-pitchTurnY * HEAD_PITCH_MAX_DEG}deg) rotateZ(${turnX * pitchTurnY * HEAD_ROLL_MAX_DEG}deg)`;
    }
    if (faceMotion) {
      const faceScaleX = 1 - Math.abs(turnX) * FACE_YAW_SQUEEZE_X;
      const faceOriginX = 50 - turnX * FACE_YAW_ORIGIN_SHIFT_PERCENT;
      faceMotion.style.transformOrigin = `${faceOriginX}% ${(FACE_CENTER_Y / 128) * 100}%`;
      faceMotion.style.transform = `translate3d(${turnX * (FACE_SHIFT_X / 128) * 100}%, ${pitchTurnY * (FACE_SHIFT_Y / 128) * 100}%, 0) scaleX(${faceScaleX})`;
    }
    if (pupilMotion) {
      pupilMotion.setAttribute("transform", `translate(${x * 0.28} ${y * 0.25 - 0.45})`);
    }
    if (shadowMotion) {
      const scaleX = 1 - (Math.abs(turnX) * SHADOW_SQUEEZE_X) / 34;
      const scaleY = 1 + (Math.max(pitchTurnY, 0) * SHADOW_SCALE_Y) / 7;
      shadowMotion.style.transform = `translate3d(${turnX * (SHADOW_SHIFT_X / 128) * 100}%, 0, 0) scale(${scaleX}, ${scaleY})`;
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
              <ellipse cx="64" cy="113" fill="var(--mascot-shadow)" rx="30" ry="4" />
            </svg>
          </span>

          <span data-slot="head-gesture" ref={headGestureRef} style={headGestureStyle}>
            <span data-slot="head" ref={headRef} style={headStyle}>
          <span data-slot="body-shell-outline" style={bodyBackShellStyle}>
            <svg data-slot="body-shell-outline-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
              <path
                d={BODY_PATH}
                fill="none"
                stroke="var(--mascot-shell-outline)"
                strokeLinejoin="round"
                strokeWidth="3.5"
              />
            </svg>
          </span>

          <span data-slot="body-back-shell" style={bodyBackShellStyle}>
            <svg data-slot="body-back-shell-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
              <path
                d={BODY_PATH}
                fill="var(--mascot-shell-rear)"
              />
            </svg>
          </span>

          <span data-slot="arm-left-motion" style={compositeLayerStyle}>
            <svg data-slot="head-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
              <path
                d={LEFT_ARM_PATH}
                fill="none"
                stroke="var(--mascot-shell-outline)"
                strokeLinecap="round"
                strokeWidth="9.5"
              />
              <ellipse
                cx={LEFT_PALM_X}
                cy={PALM_Y}
                fill="var(--mascot-shell-outline)"
                rx="8"
                ry="7"
                transform={`rotate(-52 ${LEFT_PALM_X} ${PALM_Y})`}
              />
              <path
                data-slot="arm-left-limb"
                d={LEFT_ARM_PATH}
                fill="none"
                stroke={bodyMetalPaint}
                strokeLinecap="round"
                strokeWidth="5.5"
              />
              <ellipse
                data-slot="arm-left-palm"
                cx={LEFT_PALM_X}
                cy={PALM_Y}
                fill={bodyMetalPaint}
                rx="6"
                ry="5"
                transform={`rotate(-52 ${LEFT_PALM_X} ${PALM_Y})`}
              />
            </svg>
          </span>

          <span data-slot="arm-right-motion" style={compositeLayerStyle}>
            <svg data-slot="head-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
              <path
                d={RIGHT_ARM_PATH}
                fill="none"
                stroke="var(--mascot-shell-outline)"
                strokeLinecap="round"
                strokeWidth="9.5"
              />
              <ellipse
                cx={RIGHT_PALM_X}
                cy={PALM_Y}
                fill="var(--mascot-shell-outline)"
                rx="8"
                ry="7"
                transform={`rotate(52 ${RIGHT_PALM_X} ${PALM_Y})`}
              />
              <path
                data-slot="arm-right-limb"
                d={RIGHT_ARM_PATH}
                fill="none"
                stroke={bodyMetalPaint}
                strokeLinecap="round"
                strokeWidth="5.5"
              />
              <ellipse
                data-slot="arm-right-palm"
                cx={RIGHT_PALM_X}
                cy={PALM_Y}
                fill={bodyMetalPaint}
                rx="6"
                ry="5"
                transform={`rotate(52 ${RIGHT_PALM_X} ${PALM_Y})`}
              />
            </svg>
          </span>

          <span data-slot="antenna-motion" style={antennaMotionStyle}>
            <span data-slot="antenna-glow" style={compositeLayerStyle}>
              <svg data-slot="head-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
                <path
                  d="M64 34 C65 28 63 23 60 18"
                  fill="none"
                  stroke="var(--mascot-shell-outline)"
                  strokeLinecap="round"
                  strokeWidth="8"
                />
                <circle cx="59" cy="16" fill="var(--mascot-shell-outline)" r="7" />
                <path
                  d="M64 34 C65 28 63 23 60 18"
                  fill="none"
                  stroke={bodyMetalPaint}
                  strokeLinecap="round"
                  strokeWidth="4"
                />
                <circle
                  cx="59"
                  cy="16"
                  fill={bodyMetalPaint}
                  r="5"
                />
              </svg>
            </span>
          </span>

          <svg data-slot="body-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
            <defs>
              <linearGradient
                id={bodyMetalGradientID}
                gradientUnits="userSpaceOnUse"
                x1="64"
                x2="64"
                y1="34"
                y2="110"
              >
                <stop offset="0" stopColor="var(--mascot-body-metal-top)" />
                <stop offset="1" stopColor="var(--mascot-body)" />
              </linearGradient>
            </defs>
            <path
              d={BODY_PATH}
              fill={bodyMetalPaint}
            />
            <path
              data-slot="body-metal-highlight"
              d="M25.5 62 C26.5 51 32.5 42 43 38.5"
              fill="none"
              stroke="var(--mascot-metal-highlight)"
              strokeLinecap="round"
              strokeWidth="2.75"
            />
            <path
              data-slot="body-metal-shade"
              d="M104.5 73 C107 86.5 105 96 98 101.5 C94 104.5 89.5 106 85 106.5"
              fill="none"
              stroke="var(--mascot-metal-shade)"
              strokeLinecap="round"
              strokeOpacity="0.72"
              strokeWidth="2.75"
            />
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

          <svg data-slot="antenna-base-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
            <path
              d="M59 35 L60.5 30 H67.5 L69 35 Z"
              fill={bodyMetalPaint}
              stroke="var(--mascot-shell-outline)"
              strokeLinejoin="round"
              strokeWidth="2.25"
            />
          </svg>

          <span data-slot="face-layer" style={faceLayerStyle}>
            <span data-slot="face-motion" ref={faceMotionRef} style={faceMotionStyle}>
              <span data-slot="face-screen-groove" style={compositeLayerStyle}>
                <svg data-slot="face-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
                  <path
                    d={FACE_SCREEN_PATH}
                    fill="none"
                    stroke="var(--mascot-screen-groove)"
                    strokeLinejoin="round"
                    strokeWidth="4.5"
                    transform={FACE_SCREEN_GROOVE_TRANSFORM}
                  />
                </svg>
              </span>
              <span data-slot="face-glass-plane" style={faceGlassStyle}>
                <span data-slot="face-screen" style={compositeLayerStyle}>
                  <svg data-slot="face-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
                    <path
                      d={FACE_SCREEN_PATH}
                      fill="var(--mascot-screen)"
                      fillOpacity="0.82"
                      stroke="var(--mascot-screen-edge)"
                      strokeOpacity="0.92"
                      strokeWidth="4.5"
                      transform={FACE_SCREEN_INSET_TRANSFORM}
                    />
                  </svg>
                </span>
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

                <span data-slot="status-lights" style={compositeLayerStyle}>
                  <svg data-slot="face-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
                    <g fill="var(--mascot-status-light)">
                      <circle cx="41" cy="82" r="3.5" />
                      <circle cx="87" cy="82" r="3.5" />
                    </g>
                    <g fill="#ffffff" fillOpacity="0.42">
                      <circle cx="40" cy="81" r="0.9" />
                      <circle cx="86" cy="81" r="0.9" />
                    </g>
                  </svg>
                </span>

                <span data-slot="eyes" style={eyeMotionStyle}>
                  <svg data-slot="face-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
                    <g ref={pupilMotionRef} fill="var(--mascot-feature-outline)">
                      <ellipse cx="52" cy="70" rx="5.6" ry="7" />
                      <ellipse cx="76" cy="70" rx="5.6" ry="7" />
                      <g fill="#ffffff">
                        <circle cx="50.2" cy="67.2" r="1.7" />
                        <circle cx="74.2" cy="67.2" r="1.7" />
                      </g>
                    </g>
                  </svg>
                </span>

                <span data-slot="eyes-error" style={compositeLayerStyle}>
                  <svg data-slot="face-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
                    <g fill="none" stroke="var(--mascot-feature-outline)" strokeLinecap="round" strokeWidth="3.8">
                      <path d="M48 70 Q52 73.5 56 70" />
                      <path d="M72 70 Q76 73.5 80 70" />
                    </g>
                  </svg>
                </span>

                <span data-slot="mouth" style={mouthMotionStyle}>
                  <svg data-slot="face-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
                    <path
                      d={MOUTH_PATH}
                      fill="none"
                      stroke="var(--mascot-feature-outline)"
                      strokeLinecap="round"
                      strokeWidth="5.4"
                    />
                    <path
                      d={MOUTH_PATH}
                      fill="none"
                      stroke="#ffffff"
                      strokeLinecap="round"
                      strokeWidth="3"
                    />
                  </svg>
                </span>

                <span data-slot="mouth-error" style={compositeLayerStyle}>
                  <svg data-slot="face-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
                    <path
                      d="M59 89.4 Q64 87.6 69 89.4"
                      fill="none"
                      stroke="var(--mascot-feature-outline)"
                      strokeLinecap="round"
                      strokeWidth="3.6"
                    />
                  </svg>
                </span>

                <span data-slot="face-screen-reflection" style={compositeLayerStyle}>
                  <svg data-slot="face-art" focusable="false" viewBox="0 0 128 128" style={absoluteLayerStyle}>
                    <path
                      d="M39 66 C40 61.5 43.5 59 48.5 59 H76 C69 61 63.5 64.5 59 68 H42 C40.5 68 39.5 67.25 39 66 Z"
                      fill="color-mix(in oklch, var(--mascot-screen), white 74%)"
                      fillOpacity="0.28"
                    />
                    <path
                      d="M39 65 C40 61.5 43 59.5 47 59.5 H72"
                      fill="none"
                      stroke="color-mix(in oklch, var(--mascot-screen), white 76%)"
                      strokeLinecap="round"
                      strokeOpacity="0.78"
                      strokeWidth="1.8"
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
