import { SCENE_POSE_LIMITS, type SceneLayer, type ScenePose, type Vec3 } from "./scene";

function translate({ x, y, z }: Vec3) {
  return `translate3d(${x}px, ${y}px, ${z}px)`;
}

function rotate({ x, y, z }: Vec3) {
  return `rotateX(${x}deg) rotateY(${y}deg) rotateZ(${z}deg)`;
}

export function getScenePoseTransform({ yaw, pitch, roll }: ScenePose) {
  return `rotateY(${yaw}deg) rotateX(${pitch}deg) rotateZ(${roll}deg)`;
}

export function getSceneLayerTransform(layer: SceneLayer, pose: ScenePose) {
  const inversePivot = {
    x: -layer.pivot.x,
    y: -layer.pivot.y,
    z: -layer.pivot.z,
  };
  const transforms = [translate(layer.position), translate(layer.pivot), rotate(layer.rotation), translate(inversePivot)];
  if (layer.poseResponse) {
    const normalizedYaw = pose.yaw / SCENE_POSE_LIMITS.yaw;
    const normalizedPitch = pose.pitch / SCENE_POSE_LIMITS.pitch;
    transforms.push(
      `translate3d(${normalizedYaw * layer.poseResponse.yawTranslateX}px, ${normalizedPitch * layer.poseResponse.pitchTranslateY}px, 0px)`,
      `scaleX(${1 - Math.abs(normalizedYaw) * layer.poseResponse.yawScaleX})`,
    );
  }
  return transforms.join(" ");
}
