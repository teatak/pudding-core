import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";

import { createMascotSceneV1 } from "./mascotSceneV1";
import { MascotScenePrototype } from "./MascotScenePrototype";
import type { MascotMood } from "./mood";
import { useSceneGazePose, type SceneGaze } from "./poseController";

export type MascotSceneV1AdapterProps = {
  ambientMotion?: boolean;
  className?: string;
  gaze?: SceneGaze;
  headShakeSignal?: number;
  inputPitchBias?: number;
  mood?: MascotMood;
  onPointerGaze?: () => void;
  pointerTracking?: boolean;
};

const DEFAULT_GAZE: SceneGaze = { type: "pointer" };
const MASCOT_SCENE_V1 = createMascotSceneV1();
const CLICK_ANIMATION_DURATION_MS = 2200;
const HEAD_SHAKE_DURATION_MS = 600;
const CLICK_KEY_TIMES = {
  start: 0,
  hit: 0.16,
  rebound: 0.32,
  echo1: 0.48,
  echo2: 0.62,
  echo3: 0.76,
  settle: 0.9,
  end: 1,
} as const;
export function MascotSceneV1Adapter({
  ambientMotion = true,
  className,
  gaze = DEFAULT_GAZE,
  headShakeSignal = 0,
  inputPitchBias = 0,
  mood = "idle",
  onPointerGaze,
  pointerTracking = true,
}: MascotSceneV1AdapterProps) {
  const rootRef = useRef<HTMLSpanElement>(null);
  const gestureRef = useRef<HTMLDivElement>(null);
  const gestureAnimationRef = useRef<Animation | null>(null);
  const [size, setSize] = useState(128);
  const pose = useSceneGazePose({
    ambientMotion,
    gaze,
    headShakeSignal,
    inputPitchBias,
    mood,
    onPointerGaze,
    pointerTracking,
    rootRef,
  });

  const playGesture = useCallback((keyframes: Keyframe[], duration: number, easing: string) => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    gestureAnimationRef.current?.cancel();
    gestureAnimationRef.current = gestureRef.current?.animate(keyframes, { duration, easing }) ?? null;
  }, []);

  const playClickFeedback = useCallback(() => {
    if (Math.random() < 0.5) {
      playGesture(createBounceKeyframes(), CLICK_ANIMATION_DURATION_MS, "cubic-bezier(0.2, 0.85, 0.18, 1)");
      return;
    }
    playGesture(createWobbleKeyframes(Math.random() < 0.5 ? -1 : 1), CLICK_ANIMATION_DURATION_MS, "cubic-bezier(0.2, 0.8, 0.2, 1)");
  }, [playGesture]);

  useEffect(() => {
    if (!ambientMotion) {
      gestureAnimationRef.current?.cancel();
      gestureAnimationRef.current = null;
      return;
    }
    if (headShakeSignal > 0) {
      playGesture(createHeadShakeKeyframes(), HEAD_SHAKE_DURATION_MS, "ease-in-out");
    }
  }, [ambientMotion, headShakeSignal, playGesture]);

  useEffect(() => () => gestureAnimationRef.current?.cancel(), []);

  useLayoutEffect(() => {
    const root = rootRef.current;
    if (!root) return;
    const updateSize = () => {
      const bounds = root.getBoundingClientRect();
      const nextSize = Math.max(bounds.width, bounds.height);
      if (nextSize > 0) setSize((current) => current === nextSize ? current : nextSize);
    };
    updateSize();
    const observer = new ResizeObserver(updateSize);
    observer.observe(root);
    return () => observer.disconnect();
  }, []);

  return (
    <span
      aria-hidden="true"
      className={["mascot-scene-v1-adapter", className].filter(Boolean).join(" ")}
      data-ambient-motion={ambientMotion}
      data-gaze={gaze.type}
      data-motion-enabled={ambientMotion || pointerTracking}
      data-mood={mood}
      data-pointer-tracking={pointerTracking}
      data-slot="mascot-scene-v1"
      ref={rootRef}
      onPointerDown={ambientMotion ? playClickFeedback : undefined}
    >
      <span className="mascot-scene-v1-paint-bounds">
        <span className="mascot-scene-v1-art-viewport">
          <MascotScenePrototype
            ambientMotion={ambientMotion}
            gestureRef={gestureRef}
            layers={MASCOT_SCENE_V1.layers}
            mood={mood}
            pose={pose}
            scenePivot={MASCOT_SCENE_V1.scenePivot}
            size={size}
          />
        </span>
      </span>
    </span>
  );
}

function createBounceKeyframes(): Keyframe[] {
  return [
    { offset: CLICK_KEY_TIMES.start, transform: "translateY(0%) rotateZ(0deg) scale(1, 1)" },
    { offset: CLICK_KEY_TIMES.hit, transform: "translateY(2.2%) rotateZ(-1.1deg) scale(1.13, 0.908)" },
    { offset: CLICK_KEY_TIMES.rebound, transform: "translateY(-2.5%) rotateZ(0.85deg) scale(0.924, 1.074)" },
    { offset: CLICK_KEY_TIMES.echo1, transform: "translateY(0.8%) rotateZ(-0.52deg) scale(1.058, 0.966)" },
    { offset: CLICK_KEY_TIMES.echo2, transform: "translateY(-1%) rotateZ(0.32deg) scale(0.958, 1.04)" },
    { offset: CLICK_KEY_TIMES.echo3, transform: "translateY(0.4%) rotateZ(-0.24deg) scale(1.032, 0.978)" },
    { offset: CLICK_KEY_TIMES.settle, transform: "translateY(-0.4%) rotateZ(0.13deg) scale(0.986, 1.014)" },
    { offset: CLICK_KEY_TIMES.end, transform: "translateY(0%) rotateZ(0deg) scale(1, 1)" },
  ];
}

function createWobbleKeyframes(direction: -1 | 1): Keyframe[] {
  return [
    { offset: CLICK_KEY_TIMES.start, transform: "translateX(0%) rotateZ(0deg)" },
    { offset: CLICK_KEY_TIMES.hit, transform: `translateX(${direction * -2.4}%) rotateZ(${direction * -9}deg)` },
    { offset: CLICK_KEY_TIMES.rebound, transform: `translateX(${direction * 2.1}%) rotateZ(${direction * 7.5}deg)` },
    { offset: CLICK_KEY_TIMES.echo1, transform: `translateX(${direction * -1.6}%) rotateZ(${direction * -5.6}deg)` },
    { offset: CLICK_KEY_TIMES.echo2, transform: `translateX(${direction * 1}%) rotateZ(${direction * 3.5}deg)` },
    { offset: CLICK_KEY_TIMES.echo3, transform: `translateX(${direction * -0.4}%) rotateZ(${direction * -1.4}deg)` },
    { offset: CLICK_KEY_TIMES.settle, transform: `translateX(${direction * 0.15}%) rotateZ(${direction * 0.5}deg)` },
    { offset: CLICK_KEY_TIMES.end, transform: "translateX(0%) rotateZ(0deg)" },
  ];
}

function createHeadShakeKeyframes(): Keyframe[] {
  return [
    { offset: 0, transform: "rotateY(0deg)" },
    { offset: 0.16, transform: "rotateY(-12deg)" },
    { offset: 0.4, transform: "rotateY(12deg)" },
    { offset: 0.64, transform: "rotateY(-12deg)" },
    { offset: 0.84, transform: "rotateY(4.3deg)" },
    { offset: 1, transform: "rotateY(0deg)" },
  ];
}
