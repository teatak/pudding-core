import { useCallback, useEffect, useRef, useState, type RefObject } from "react";

import { SCENE_POSE_LIMITS, type ScenePose } from "./scene";
import type { MascotMood } from "./mood";

const POSE_RESPONSE_MS = 67;
const POSE_EPSILON = 0.02;
const POINTER_SATURATION_RATIO = 1.15;
const POINTER_SATURATION_MIN_PX = 128;
const INPUT_SATURATION_RATIO = 0.75;
const INPUT_SATURATION_MIN_PX = 64;
const CENTER_POSE: ScenePose = { yaw: 0, pitch: 0, roll: 0 };
const ERROR_HEAD_POSE: ScenePose = { yaw: 0, pitch: -3.7, roll: 0 };

type PointerBounds = {
  height: number;
  left: number;
  top: number;
  width: number;
};

export type PoseInputMode = "manual" | "pointer";

export type SceneGazePoint = {
  clientX: number;
  clientY: number;
};

export type SceneGaze =
  | { type: "pointer" }
  | { type: "input"; target: SceneGazePoint }
  | { type: "center" };

type SceneGazePoseOptions = {
  ambientMotion: boolean;
  gaze: SceneGaze;
  headShakeSignal: number;
  inputPitchBias: number;
  mood: MascotMood;
  onPointerGaze?: () => void;
  pointerTracking: boolean;
  rootRef: RefObject<HTMLElement | null>;
};

export function getPointerTargetPose(clientX: number, clientY: number, bounds: PointerBounds): ScenePose {
  const normalizedX = clamp((clientX - bounds.left) / bounds.width * 2 - 1, -1, 1);
  const normalizedY = clamp((clientY - bounds.top) / bounds.height * 2 - 1, -1, 1);
  return {
    yaw: normalizedX * SCENE_POSE_LIMITS.yaw,
    pitch: -normalizedY * SCENE_POSE_LIMITS.pitch,
    roll: 0,
  };
}

export function getGazeTargetPose(
  point: SceneGazePoint,
  bounds: PointerBounds,
  saturationRatio: number,
  saturationMinPx: number,
  pitchBias = 0,
): ScenePose {
  const centerX = bounds.left + bounds.width / 2;
  const centerY = bounds.top + bounds.height / 2;
  const deltaX = point.clientX - centerX;
  const deltaY = point.clientY - centerY;
  const distance = Math.hypot(deltaX, deltaY);
  if (distance < 1) return { ...CENTER_POSE };

  const saturationDistance = Math.max(bounds.width, bounds.height, saturationMinPx) * saturationRatio;
  const strength = Math.min(distance / saturationDistance, 1);
  const normalizedX = deltaX / distance * strength;
  const normalizedY = clamp(deltaY / distance * strength + pitchBias, -1, 1);
  return {
    yaw: normalizedX * SCENE_POSE_LIMITS.yaw,
    pitch: -normalizedY * SCENE_POSE_LIMITS.pitch,
    roll: 0,
  };
}

export function useSceneGazePose({
  ambientMotion,
  gaze,
  headShakeSignal,
  inputPitchBias,
  mood,
  onPointerGaze,
  pointerTracking,
  rootRef,
}: SceneGazePoseOptions): ScenePose {
  const { pose, setPoseImmediately, setTargetPose } = useSmoothedScenePose(CENTER_POSE);
  const gazeTypeRef = useRef(gaze.type);
  const lockedRef = useRef(gaze.type === "center" || mood === "error");
  const onPointerGazeRef = useRef(onPointerGaze);
  const motionEnabled = ambientMotion || pointerTracking;

  const setTargetFromPoint = useCallback((
    point: SceneGazePoint,
    saturationRatio: number,
    saturationMinPx: number,
    pitchBias = 0,
  ) => {
    const bounds = rootRef.current?.getBoundingClientRect();
    if (!bounds) return;
    setTargetPose(getGazeTargetPose(point, bounds, saturationRatio, saturationMinPx, pitchBias));
  }, [rootRef, setTargetPose]);

  useEffect(() => {
    gazeTypeRef.current = gaze.type;
    lockedRef.current = gaze.type === "center" || mood === "error";
    onPointerGazeRef.current = onPointerGaze;
  }, [gaze.type, mood, onPointerGaze]);

  useEffect(() => {
    if (!motionEnabled) return;
    if (mood === "error" || gaze.type === "center") {
      setTargetPose(CENTER_POSE);
      return;
    }
    if (gaze.type === "input") {
      setTargetFromPoint(
        gaze.target,
        INPUT_SATURATION_RATIO,
        INPUT_SATURATION_MIN_PX,
        inputPitchBias,
      );
    }
  }, [gaze, inputPitchBias, mood, motionEnabled, setTargetFromPoint, setTargetPose]);

  useEffect(() => {
    if (ambientMotion && headShakeSignal > 0) setPoseImmediately(ERROR_HEAD_POSE);
  }, [ambientMotion, headShakeSignal, setPoseImmediately]);

  useEffect(() => {
    if (!motionEnabled || !pointerTracking) return;
    const handlePointerMove = (event: PointerEvent) => {
      if (lockedRef.current) return;
      if (gazeTypeRef.current !== "pointer") {
        gazeTypeRef.current = "pointer";
        onPointerGazeRef.current?.();
      }
      setTargetFromPoint(
        event,
        POINTER_SATURATION_RATIO,
        POINTER_SATURATION_MIN_PX,
      );
    };
    window.addEventListener("pointermove", handlePointerMove, { capture: true, passive: true });
    return () => window.removeEventListener("pointermove", handlePointerMove, { capture: true });
  }, [motionEnabled, pointerTracking, setTargetFromPoint]);

  return pose;
}

export function useSmoothedScenePose(initialPose: ScenePose) {
  const [pose, setPose] = useState(initialPose);
  const [targetPose, setTargetPoseState] = useState(initialPose);
  const poseRef = useRef(initialPose);
  const targetRef = useRef(initialPose);
  const frameRef = useRef<number | null>(null);
  const previousTimeRef = useRef<number | null>(null);
  const stepRef = useRef<(time: number) => void>(() => undefined);

  stepRef.current = (time: number) => {
    const previousTime = previousTimeRef.current ?? time;
    const blend = 1 - Math.exp(-(time - previousTime) / POSE_RESPONSE_MS);
    const current = poseRef.current;
    const target = targetRef.current;
    const next = {
      yaw: interpolate(current.yaw, target.yaw, blend),
      pitch: interpolate(current.pitch, target.pitch, blend),
      roll: interpolate(current.roll, target.roll, blend),
    };
    const settled = poseDistance(next, target) < POSE_EPSILON;
    const renderedPose = settled ? target : next;
    poseRef.current = renderedPose;
    setPose(renderedPose);

    if (settled) {
      frameRef.current = null;
      previousTimeRef.current = null;
      return;
    }
    previousTimeRef.current = time;
    frameRef.current = requestAnimationFrame(stepRef.current);
  };

  const setTargetPose = useCallback((nextPose: ScenePose) => {
    const next = { ...nextPose };
    targetRef.current = next;
    setTargetPoseState(next);
    if (frameRef.current === null) {
      previousTimeRef.current = null;
      frameRef.current = requestAnimationFrame(stepRef.current);
    }
  }, []);

  const setPoseImmediately = useCallback((nextPose: ScenePose) => {
    if (frameRef.current !== null) cancelAnimationFrame(frameRef.current);
    const next = { ...nextPose };
    frameRef.current = null;
    previousTimeRef.current = null;
    poseRef.current = next;
    targetRef.current = next;
    setPose(next);
    setTargetPoseState(next);
  }, []);

  useEffect(() => () => {
    if (frameRef.current !== null) cancelAnimationFrame(frameRef.current);
    frameRef.current = null;
    previousTimeRef.current = null;
  }, []);

  return { pose, setPoseImmediately, setTargetPose, targetPose };
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

function interpolate(current: number, target: number, blend: number) {
  return current + (target - current) * blend;
}

function poseDistance(left: ScenePose, right: ScenePose) {
  return Math.max(
    Math.abs(left.yaw - right.yaw),
    Math.abs(left.pitch - right.pitch),
    Math.abs(left.roll - right.roll),
  );
}
