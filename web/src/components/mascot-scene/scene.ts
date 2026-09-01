import type { SceneAssetID } from "./geometry";
import type { MascotMood } from "./mood";

export const SCENE_VIEWBOX_SIZE = 128;
export const SCENE_ORIGIN = { x: 64, y: 77, z: 0 } as const;
export const SCENE_PERSPECTIVE_PX = 170;
export const SCENE_POSE_LIMITS = { yaw: 20, pitch: 16, roll: 4 } as const;

export type Vec3 = {
  x: number;
  y: number;
  z: number;
};

export type ScenePose = {
  yaw: number;
  pitch: number;
  roll: number;
};

export type SceneLayerSize = {
  width: number;
  height: number;
  depth: number;
};

export type SceneLayerPoseResponse = {
  yawTranslateX: number;
  pitchTranslateY: number;
  yawScaleX: number;
};

export type SceneLayerMotionFrame = {
  translateX: number;
  translateY: number;
  rotateZ: number;
};

export type SceneLayerIdleMotion = {
  durationMs: number;
  delayMs: number;
  rest: SceneLayerMotionFrame;
  peak: SceneLayerMotionFrame;
};

export type SceneLayerAssetAnimation = {
  preset: "blink" | "mouth-idle";
  durationMs: number;
  delayMs: number;
};

export type SceneLayer = {
  assetID: SceneAssetID | null;
  id: string;
  label: string;
  parentID: string | null;
  position: Vec3;
  size: SceneLayerSize;
  pivot: Vec3;
  rotation: Vec3;
  poseResponse?: SceneLayerPoseResponse;
  idleMotion?: SceneLayerIdleMotion;
  assetAnimation?: SceneLayerAssetAnimation;
  debugColor: string;
  moods?: MascotMood[];
};

export type SceneDefinition = {
  scenePivot: Vec3;
  layers: SceneLayer[];
};

export type SceneLayerZRange = {
  farZ: number;
  nearZ: number;
};

export function cloneSceneLayer(layer: SceneLayer): SceneLayer {
  return {
    ...layer,
    position: { ...layer.position },
    size: { ...layer.size },
    pivot: { ...layer.pivot },
    rotation: { ...layer.rotation },
    poseResponse: layer.poseResponse ? { ...layer.poseResponse } : undefined,
    idleMotion: layer.idleMotion ? {
      ...layer.idleMotion,
      rest: { ...layer.idleMotion.rest },
      peak: { ...layer.idleMotion.peak },
    } : undefined,
    assetAnimation: layer.assetAnimation ? { ...layer.assetAnimation } : undefined,
    moods: layer.moods ? [...layer.moods] : undefined,
  };
}

export function cloneSceneDefinition(definition: SceneDefinition): SceneDefinition {
  return {
    scenePivot: { ...definition.scenePivot },
    layers: definition.layers.map(cloneSceneLayer),
  };
}

export function isSceneLayerActive(layer: SceneLayer, mood: MascotMood): boolean {
  return !layer.moods || layer.moods.includes(mood);
}

export function getLayerZRange(layer: SceneLayer): SceneLayerZRange {
  const halfDepth = layer.size.depth / 2;
  return {
    farZ: layer.position.z - halfDepth,
    nearZ: layer.position.z + halfDepth,
  };
}

export function validateSceneLayers(layers: SceneLayer[]): string[] {
  const errors: string[] = [];
  const byID = new Map<string, SceneLayer>();

  for (const layer of layers) {
    if (byID.has(layer.id)) {
      errors.push(`层 ID 重复:${layer.id}`);
    }
    byID.set(layer.id, layer);
    if (layer.size.width <= 0 || layer.size.height <= 0 || layer.size.depth < 0) {
      errors.push(`${layer.label} 的 width/height 必须大于 0，depth 不能小于 0`);
    }
    if (layer.moods?.length === 0) {
      errors.push(`${layer.label} 的 moods 不能为空数组`);
    }
    if (layer.idleMotion && layer.idleMotion.durationMs <= 0) {
      errors.push(`${layer.label} 的 idleMotion.durationMs 必须大于 0`);
    }
    if (layer.assetAnimation && layer.assetAnimation.durationMs <= 0) {
      errors.push(`${layer.label} 的 assetAnimation.durationMs 必须大于 0`);
    }
  }

  for (const layer of layers) {
    if (layer.parentID && !byID.has(layer.parentID)) {
      errors.push(`${layer.label} 的父层不存在:${layer.parentID}`);
      continue;
    }

    const visited = new Set([layer.id]);
    let parentID = layer.parentID;
    while (parentID) {
      if (visited.has(parentID)) {
        errors.push(`${layer.label} 存在父层循环`);
        break;
      }
      visited.add(parentID);
      parentID = byID.get(parentID)?.parentID ?? null;
    }
  }

  return Array.from(new Set(errors));
}
