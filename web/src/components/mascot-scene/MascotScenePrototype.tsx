import type { CSSProperties, ReactNode, Ref } from "react";

import { SceneAsset } from "./SceneAsset";
import type { MascotMood } from "./mood";
import {
  isSceneLayerActive,
  SCENE_ORIGIN,
  SCENE_PERSPECTIVE_PX,
  SCENE_VIEWBOX_SIZE,
  type SceneLayer,
  type ScenePose,
  type Vec3,
  validateSceneLayers,
} from "./scene";
import { getSceneLayerTransform, getScenePoseTransform } from "./transforms";
import "./scene.css";

type MascotScenePrototypeProps = {
  ambientMotion?: boolean;
  gestureRef?: Ref<HTMLDivElement>;
  layers: SceneLayer[];
  pose: ScenePose;
  scenePivot: Vec3;
  size: number;
  mood: MascotMood;
};

export function MascotScenePrototype({ ambientMotion = true, gestureRef, layers, mood, pose, scenePivot, size }: MascotScenePrototypeProps) {
  const errors = validateSceneLayers(layers);
  if (errors.length > 0) {
    return (
      <div className="mascot-scene-prototype-error" style={{ height: size, width: size }}>
        {errors.map((error) => <div key={error}>{error}</div>)}
      </div>
    );
  }

  const childrenByParent = new Map<string | null, SceneLayer[]>();
  for (const layer of layers) {
    const siblings = childrenByParent.get(layer.parentID) ?? [];
    siblings.push(layer);
    childrenByParent.set(layer.parentID, siblings);
  }
  const scale = size / SCENE_VIEWBOX_SIZE;

  const renderLayer = (layer: SceneLayer): ReactNode => {
    const moodActive = isSceneLayerActive(layer, mood);
    const children = childrenByParent.get(layer.id) ?? [];
    const asset = layer.assetID ? <SceneAsset assetID={layer.assetID} size={layer.size} /> : <LayerVolume layer={layer} />;
    const animatedAsset = layer.assetAnimation ? (
      <div
        className="mascot-scene-asset-animation"
        data-asset-animation={layer.assetAnimation.preset}
        style={{
          animationDelay: `${layer.assetAnimation.delayMs}ms`,
          animationDuration: `${layer.assetAnimation.durationMs}ms`,
          transformOrigin: `${SCENE_ORIGIN.x + layer.pivot.x}px ${SCENE_ORIGIN.y + layer.pivot.y}px`,
        }}
      >
        {asset}
      </div>
    ) : asset;
    const content = (
      <>
        {animatedAsset}
        {children.map(renderLayer)}
      </>
    );
    return (
      <div
        className="mascot-scene-node"
        data-layer-id={layer.id}
        data-mood-active={moodActive}
        key={layer.id}
        style={{
          transform: getSceneLayerTransform(layer, pose),
          transformOrigin: `${SCENE_ORIGIN.x}px ${SCENE_ORIGIN.y}px 0px`,
        }}
      >
        <div
          className="mascot-scene-layer-state"
          style={{
            transformOrigin: `${SCENE_ORIGIN.x + layer.pivot.x}px ${SCENE_ORIGIN.y + layer.pivot.y}px`,
          }}
        >
          {layer.idleMotion ? (
            <div
              className="mascot-scene-layer-motion"
              data-idle-motion="true"
              style={{
                animationDelay: `${layer.idleMotion.delayMs}ms`,
                animationDuration: `${layer.idleMotion.durationMs}ms`,
                transformOrigin: `${SCENE_ORIGIN.x + layer.pivot.x}px ${SCENE_ORIGIN.y + layer.pivot.y}px`,
                "--mascot-scene-motion-rest-x": `${layer.idleMotion.rest.translateX}px`,
                "--mascot-scene-motion-rest-y": `${layer.idleMotion.rest.translateY}px`,
                "--mascot-scene-motion-rest-rotate": `${layer.idleMotion.rest.rotateZ}deg`,
                "--mascot-scene-motion-peak-x": `${layer.idleMotion.peak.translateX}px`,
                "--mascot-scene-motion-peak-y": `${layer.idleMotion.peak.translateY}px`,
                "--mascot-scene-motion-peak-rotate": `${layer.idleMotion.peak.rotateZ}deg`,
              } as CSSProperties}
            >
              {content}
            </div>
          ) : content}
        </div>
      </div>
    );
  };

  return (
    <div
      className="mascot-scene-prototype"
      data-ambient-motion={ambientMotion}
      data-mood={mood}
      style={{ height: size, width: size }}
    >
      <div
        className="mascot-scene-scale"
        style={{
          height: SCENE_VIEWBOX_SIZE,
          transform: `scale(${scale})`,
          width: SCENE_VIEWBOX_SIZE,
        }}
      >
        <div
          className="mascot-scene-perspective"
          style={{ perspective: `${SCENE_PERSPECTIVE_PX}px` }}
        >
          <div className="mascot-scene-ambient-motion">
            <div className="mascot-scene-gesture-motion" ref={gestureRef}>
              <div className="mascot-scene-state-motion">
                <div
                  className="mascot-scene-head"
                  style={{
                    transform: getScenePoseTransform(pose),
                    transformOrigin: `${scenePivot.x}px ${scenePivot.y}px ${scenePivot.z}px`,
                  }}
                >
                  {layers.length > 0 ? (childrenByParent.get(null) ?? []).map(renderLayer) : null}
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function LayerVolume({ layer }: { layer: SceneLayer }) {
  const halfDepth = layer.size.depth / 2;
  const faceStyle: CSSProperties = {
    backgroundColor: `color-mix(in srgb, ${layer.debugColor} 18%, transparent)`,
    height: layer.size.height,
    left: SCENE_ORIGIN.x - layer.size.width / 2,
    top: SCENE_ORIGIN.y - layer.size.height / 2,
    width: layer.size.width,
  };
  if (layer.size.depth === 0) {
    return (
      <div className="mascot-scene-face" style={faceStyle}>
        <span className="mascot-scene-axis is-horizontal" />
        <span className="mascot-scene-axis is-vertical" />
      </div>
    );
  }

  return (
    <>
      <div className="mascot-scene-face is-far" style={{ ...faceStyle, transform: `translateZ(${-halfDepth}px)` }} />
      <div className="mascot-scene-face is-near" style={{ ...faceStyle, transform: `translateZ(${halfDepth}px)` }}>
        <span className="mascot-scene-axis is-horizontal" />
        <span className="mascot-scene-axis is-vertical" />
      </div>
    </>
  );
}
