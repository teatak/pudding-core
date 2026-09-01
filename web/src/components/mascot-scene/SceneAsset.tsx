import { useId, type CSSProperties } from "react";

import {
  ANTENNA_BASE_ASSET_ID,
  ANTENNA_BASE_PATH,
  ANTENNA_BASE_VIEW_BOX,
  ANTENNA_STEM_ASSET_ID,
  ANTENNA_STEM_PATH,
  ANTENNA_STEM_VIEW_BOX,
  ANTENNA_TIP_CENTER,
  BODY_HIGHLIGHT_ASSET_ID,
  BODY_METAL_HIGHLIGHT_PATH,
  BODY_SHELL_ASSET_ID,
  BODY_SHELL_PATH,
  BODY_SHELL_VIEW_BOX,
  LEFT_ARM_ASSET_ID,
  LEFT_ARM_PATH,
  LEFT_ARM_VIEW_BOX,
  FACE_FRAME_ASSET_ID,
  FACE_FRAME_VIEW_BOX,
  FACE_EYES_ASSET_ID,
  FACE_THINKING_EYES_ASSET_ID,
  FACE_ERROR_EYES_ASSET_ID,
  FACE_ERROR_MOUTH_ASSET_ID,
  FACE_GLASS_ASSET_ID,
  FACE_MOUTH_ASSET_ID,
  FACE_BLUSH_ASSET_ID,
  FACE_REFLECTION_ASSET_ID,
  FACE_SCREEN_GROOVE_TRANSFORM,
  FACE_SCREEN_INSET_TRANSFORM,
  FACE_SCREEN_PATH,
  MOUTH_PATH,
  RIGHT_ARM_ASSET_ID,
  RIGHT_ARM_PATH,
  RIGHT_ARM_VIEW_BOX,
  type SceneAssetID,
} from "./geometry";
import { SCENE_ORIGIN, type SceneLayerSize } from "./scene";

type SceneAssetProps = {
  assetID: SceneAssetID;
  size: SceneLayerSize;
};

const SHELL_OUTLINE_WIDTH = 1.6;
const ANTENNA_STEM_INNER_WIDTH = 2.6;
const ANTENNA_TIP_INNER_RADIUS = 4.2;
const BODY_METAL_GRADIENT_Y1 = 34;
const BODY_METAL_GRADIENT_Y2 = 110;

function antennaOuterStrokeWidth(innerWidth: number) {
  return innerWidth + SHELL_OUTLINE_WIDTH * 2;
}

function antennaOuterRadius(innerRadius: number) {
  return innerRadius + SHELL_OUTLINE_WIDTH;
}

export function SceneAsset({ assetID, size }: SceneAssetProps) {
  switch (assetID) {
    case BODY_SHELL_ASSET_ID:
      return <BodyShellAsset size={size} />;
    case BODY_HIGHLIGHT_ASSET_ID:
      return <BodyHighlightAsset size={size} />;
    case LEFT_ARM_ASSET_ID:
      return <ArmAsset path={LEFT_ARM_PATH} size={size} viewBox={LEFT_ARM_VIEW_BOX} />;
    case RIGHT_ARM_ASSET_ID:
      return <ArmAsset path={RIGHT_ARM_PATH} size={size} viewBox={RIGHT_ARM_VIEW_BOX} />;
    case ANTENNA_STEM_ASSET_ID:
      return <AntennaStemAsset size={size} />;
    case ANTENNA_BASE_ASSET_ID:
      return <AntennaBaseAsset size={size} />;
    case FACE_FRAME_ASSET_ID:
      return <FaceFrameAsset size={size} />;
    case FACE_GLASS_ASSET_ID:
      return <FaceGlassAsset size={size} />;
    case FACE_EYES_ASSET_ID:
      return <FaceEyesAsset size={size} />;
    case FACE_THINKING_EYES_ASSET_ID:
      return <FaceThinkingEyesAsset size={size} />;
    case FACE_MOUTH_ASSET_ID:
      return <FaceMouthAsset size={size} />;
    case FACE_BLUSH_ASSET_ID:
      return <FaceBlushAsset size={size} />;
    case FACE_REFLECTION_ASSET_ID:
      return <FaceReflectionAsset size={size} />;
    case FACE_ERROR_EYES_ASSET_ID:
      return <FaceErrorEyesAsset size={size} />;
    case FACE_ERROR_MOUTH_ASSET_ID:
      return <FaceErrorMouthAsset size={size} />;
  }
}

function AntennaStemAsset({ size }: { size: SceneLayerSize }) {
  return (
    <svg
      className="mascot-scene-asset"
      focusable="false"
      preserveAspectRatio="none"
      style={getAssetStyle(size)}
      viewBox={ANTENNA_STEM_VIEW_BOX}
    >
      <path
        d={ANTENNA_STEM_PATH}
        fill="none"
        stroke="var(--mascot-shell-outline)"
        strokeLinecap="round"
        strokeWidth={antennaOuterStrokeWidth(ANTENNA_STEM_INNER_WIDTH)}
      />
      <circle
        cx={ANTENNA_TIP_CENTER.x}
        cy={ANTENNA_TIP_CENTER.y}
        fill="var(--mascot-shell-outline)"
        r={antennaOuterRadius(ANTENNA_TIP_INNER_RADIUS)}
      />
      <path
        d={ANTENNA_STEM_PATH}
        fill="none"
        stroke="var(--mascot-body-metal-top)"
        strokeLinecap="round"
        strokeWidth={ANTENNA_STEM_INNER_WIDTH}
      />
      <circle
        cx={ANTENNA_TIP_CENTER.x}
        cy={ANTENNA_TIP_CENTER.y}
        fill="var(--mascot-body-metal-top)"
        r={ANTENNA_TIP_INNER_RADIUS}
      />
    </svg>
  );
}

function AntennaBaseAsset({ size }: { size: SceneLayerSize }) {
  return (
    <svg
      className="mascot-scene-asset"
      focusable="false"
      preserveAspectRatio="none"
      style={getAssetStyle(size)}
      viewBox={ANTENNA_BASE_VIEW_BOX}
    >
      <path
        d={ANTENNA_BASE_PATH}
        fill="var(--mascot-body-metal-top)"
        stroke="var(--mascot-shell-outline)"
        strokeLinejoin="round"
        strokeWidth={SHELL_OUTLINE_WIDTH}
      />
    </svg>
  );
}

function BodyShellAsset({ size }: { size: SceneLayerSize }) {
  const gradientID = `mascot-scene-body-metal-${useId().replaceAll(":", "")}`;

  return (
    <svg
      className="mascot-scene-asset"
      focusable="false"
      preserveAspectRatio="none"
      style={getAssetStyle(size)}
      viewBox={BODY_SHELL_VIEW_BOX}
    >
      <defs>
        <linearGradient
          id={gradientID}
          gradientUnits="userSpaceOnUse"
          x1="64"
          x2="64"
          y1={BODY_METAL_GRADIENT_Y1}
          y2={BODY_METAL_GRADIENT_Y2}
        >
          <stop offset="0" stopColor="var(--mascot-body-metal-top)" />
          <stop offset="1" stopColor="var(--mascot-body)" />
        </linearGradient>
      </defs>
      <path
        d={BODY_SHELL_PATH}
        fill={`url(#${gradientID})`}
        stroke="var(--mascot-shell-outline)"
        strokeLinejoin="round"
        strokeWidth={SHELL_OUTLINE_WIDTH}
      />
    </svg>
  );
}

function BodyHighlightAsset({ size }: { size: SceneLayerSize }) {
  return (
    <svg
      className="mascot-scene-asset"
      focusable="false"
      preserveAspectRatio="none"
      style={getAssetStyle(size)}
      viewBox={BODY_SHELL_VIEW_BOX}
    >
      <path
        d={BODY_METAL_HIGHLIGHT_PATH}
        fill="none"
        stroke="var(--mascot-metal-highlight)"
        strokeLinecap="round"
        strokeWidth="2.75"
      />
    </svg>
  );
}

function ArmAsset({ path, size, viewBox }: { path: string; size: SceneLayerSize; viewBox: string }) {
  const gradientID = `mascot-scene-arm-metal-${useId().replaceAll(":", "")}`;

  return (
    <svg
      className="mascot-scene-asset"
      focusable="false"
      preserveAspectRatio="none"
      style={getAssetStyle(size)}
      viewBox={viewBox}
    >
      <defs>
        <linearGradient
          id={gradientID}
          gradientUnits="userSpaceOnUse"
          x1="64"
          x2="64"
          y1={BODY_METAL_GRADIENT_Y1}
          y2={BODY_METAL_GRADIENT_Y2}
        >
          <stop offset="0" stopColor="var(--mascot-body-metal-top)" />
          <stop offset="1" stopColor="var(--mascot-body)" />
        </linearGradient>
      </defs>
      <path
        d={path}
        fill={`url(#${gradientID})`}
        stroke="var(--mascot-shell-outline)"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth={SHELL_OUTLINE_WIDTH}
      />
    </svg>
  );
}

function FaceFrameAsset({ size }: { size: SceneLayerSize }) {
  return (
    <svg
      className="mascot-scene-asset"
      focusable="false"
      preserveAspectRatio="none"
      style={getAssetStyle(size)}
      viewBox={FACE_FRAME_VIEW_BOX}
    >
      <path
        d={FACE_SCREEN_PATH}
        fill="none"
        stroke="var(--mascot-screen-groove)"
        strokeLinejoin="round"
        strokeWidth="4.5"
        transform={FACE_SCREEN_GROOVE_TRANSFORM}
      />
    </svg>
  );
}

function FaceGlassAsset({ size }: { size: SceneLayerSize }) {
  return (
    <svg
      className="mascot-scene-asset"
      focusable="false"
      preserveAspectRatio="none"
      style={getAssetStyle(size)}
      viewBox={FACE_FRAME_VIEW_BOX}
    >
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
  );
}

function FaceEyesAsset({ size }: { size: SceneLayerSize }) {
  return (
    <svg
      className="mascot-scene-asset"
      focusable="false"
      preserveAspectRatio="none"
      style={getAssetStyle(size)}
      viewBox={FACE_FRAME_VIEW_BOX}
    >
      <FaceEyePair />
    </svg>
  );
}

function FaceThinkingEyesAsset({ size }: { size: SceneLayerSize }) {
  return (
    <svg
      className="mascot-scene-asset"
      focusable="false"
      preserveAspectRatio="none"
      style={getAssetStyle(size)}
      viewBox={FACE_FRAME_VIEW_BOX}
    >
      <g fill="var(--mascot-feature-outline)">
        <rect className="mascot-scene-thinking-dot mascot-scene-thinking-dot-left" height="8" rx="2" width="8" x="48.5" y="66" />
        <rect className="mascot-scene-thinking-dot mascot-scene-thinking-dot-center" height="8" rx="2" width="8" x="60" y="66" />
        <rect className="mascot-scene-thinking-dot mascot-scene-thinking-dot-right" height="8" rx="2" width="8" x="71.5" y="66" />
      </g>
    </svg>
  );
}

function FaceEyePair({ className }: { className?: string }) {
  return (
    <g className={className} fill="var(--mascot-feature-outline)">
      <ellipse cx="52" cy="70" rx="5.6" ry="7" />
      <ellipse cx="76" cy="70" rx="5.6" ry="7" />
      <g fill="#ffffff">
        <circle cx="50.2" cy="67.2" r="1.7" />
        <circle cx="74.2" cy="67.2" r="1.7" />
      </g>
    </g>
  );
}

function FaceMouthAsset({ size }: { size: SceneLayerSize }) {
  return (
    <svg
      className="mascot-scene-asset"
      focusable="false"
      preserveAspectRatio="none"
      style={getAssetStyle(size)}
      viewBox={FACE_FRAME_VIEW_BOX}
    >
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
  );
}

function FaceBlushAsset({ size }: { size: SceneLayerSize }) {
  return (
    <svg
      className="mascot-scene-asset"
      focusable="false"
      preserveAspectRatio="none"
      style={getAssetStyle(size)}
      viewBox={FACE_FRAME_VIEW_BOX}
    >
      <g fill="var(--mascot-blush)">
        <circle cx="41" cy="82" r="3.5" />
        <circle cx="87" cy="82" r="3.5" />
      </g>
      <g fill="#ffffff" fillOpacity="0.42">
        <circle cx="40" cy="81" r="0.9" />
        <circle cx="86" cy="81" r="0.9" />
      </g>
    </svg>
  );
}

function FaceReflectionAsset({ size }: { size: SceneLayerSize }) {
  return (
    <svg
      className="mascot-scene-asset"
      focusable="false"
      preserveAspectRatio="none"
      style={getAssetStyle(size)}
      viewBox={FACE_FRAME_VIEW_BOX}
    >
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
  );
}

function FaceErrorEyesAsset({ size }: { size: SceneLayerSize }) {
  return (
    <svg
      className="mascot-scene-asset"
      focusable="false"
      preserveAspectRatio="none"
      style={getAssetStyle(size)}
      viewBox={FACE_FRAME_VIEW_BOX}
    >
      <g fill="none" stroke="var(--mascot-feature-outline)" strokeLinecap="round" strokeWidth="3.8">
        <path d="M48 70 Q52 73.5 56 70" />
        <path d="M72 70 Q76 73.5 80 70" />
      </g>
    </svg>
  );
}

function FaceErrorMouthAsset({ size }: { size: SceneLayerSize }) {
  return (
    <svg
      className="mascot-scene-asset"
      focusable="false"
      preserveAspectRatio="none"
      style={getAssetStyle(size)}
      viewBox={FACE_FRAME_VIEW_BOX}
    >
      <path
        d="M59 89.4 Q64 87.6 69 89.4"
        fill="none"
        stroke="var(--mascot-feature-outline)"
        strokeLinecap="round"
        strokeWidth="3.6"
      />
    </svg>
  );
}

function getAssetStyle(size: SceneLayerSize): CSSProperties {
  return {
    height: size.height,
    left: SCENE_ORIGIN.x - size.width / 2,
    top: SCENE_ORIGIN.y - size.height / 2,
    width: size.width,
  };
}
