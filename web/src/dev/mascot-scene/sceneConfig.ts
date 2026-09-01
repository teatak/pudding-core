import { z } from "zod";

import { SCENE_ASSET_OPTIONS, type SceneAssetID } from "@/components/mascot-scene/geometry";
import { cloneSceneLayer, type SceneLayer, type Vec3, validateSceneLayers } from "@/components/mascot-scene/scene";

const SCENE_CONFIG_VERSION = 1 as const;
const assetIDs = new Set<SceneAssetID>(SCENE_ASSET_OPTIONS.map((asset) => asset.id));
const finiteNumber = z.number().refine(Number.isFinite, "必须是有限数字");
const vec3Schema = z.object({ x: finiteNumber, y: finiteNumber, z: finiteNumber }).strict();
const layerSizeSchema = z.object({
  width: finiteNumber.positive(),
  height: finiteNumber.positive(),
  depth: finiteNumber.nonnegative(),
}).strict();
const poseResponseSchema = z.object({
  yawTranslateX: finiteNumber,
  pitchTranslateY: finiteNumber,
  yawScaleX: finiteNumber.nonnegative(),
}).strict();
const motionFrameSchema = z.object({
  translateX: finiteNumber,
  translateY: finiteNumber,
  rotateZ: finiteNumber,
}).strict();
const idleMotionSchema = z.object({
  durationMs: finiteNumber.positive(),
  delayMs: finiteNumber,
  rest: motionFrameSchema,
  peak: motionFrameSchema,
}).strict();
const assetAnimationSchema = z.object({
  preset: z.enum(["blink", "mouth-idle"]),
  durationMs: finiteNumber.positive(),
  delayMs: finiteNumber,
}).strict();
const assetIDSchema = z.custom<SceneAssetID>((value) => typeof value === "string" && assetIDs.has(value as SceneAssetID), "未知素材 ID");
const moodSchema = z.enum(["idle", "thinking", "error"]);
const sceneLayerSchema = z.object({
  assetID: assetIDSchema.nullable(),
  id: z.string().min(1),
  label: z.string().min(1),
  parentID: z.string().min(1).nullable(),
  position: vec3Schema,
  size: layerSizeSchema,
  pivot: vec3Schema,
  rotation: vec3Schema,
  poseResponse: poseResponseSchema.optional(),
  idleMotion: idleMotionSchema.optional(),
  assetAnimation: assetAnimationSchema.optional(),
  debugColor: z.string().regex(/^#[0-9a-f]{6}$/i, "调试颜色必须是六位十六进制颜色"),
  moods: z.array(moodSchema).min(1).optional(),
}).strict();
const sceneConfigSchema = z.object({
  version: z.literal(SCENE_CONFIG_VERSION),
  scenePivot: vec3Schema,
  layers: z.array(sceneLayerSchema).min(1),
}).strict();

export type SceneConfig = {
  version: typeof SCENE_CONFIG_VERSION;
  scenePivot: Vec3;
  layers: SceneLayer[];
};

export function serializeSceneConfig(scenePivot: Vec3, layers: SceneLayer[]): string {
  return JSON.stringify({
    version: SCENE_CONFIG_VERSION,
    scenePivot,
    layers,
  } satisfies SceneConfig, null, 2);
}

export function parseSceneConfig(json: string): SceneConfig {
  const config = sceneConfigSchema.parse(JSON.parse(json));
  const layerErrors = validateSceneLayers(config.layers);
  if (layerErrors.length > 0) throw new Error(layerErrors.join("；"));
  return {
    version: config.version,
    scenePivot: { ...config.scenePivot },
    layers: config.layers.map(cloneSceneLayer),
  };
}
