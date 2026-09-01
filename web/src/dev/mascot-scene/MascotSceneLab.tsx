import { useMemo, useRef, useState } from "react";

import { SCENE_ASSET_OPTIONS, type SceneAssetID } from "@/components/mascot-scene/geometry";
import { createMascotSceneV1, MASCOT_SCENE_V1_ID } from "@/components/mascot-scene/mascotSceneV1";
import { MascotScenePrototype } from "@/components/mascot-scene/MascotScenePrototype";
import { MASCOT_MOOD_OPTIONS, type MascotMood } from "@/components/mascot-scene/mood";
import { getPointerTargetPose, useSmoothedScenePose, type PoseInputMode } from "@/components/mascot-scene/poseController";
import {
  getLayerZRange,
  SCENE_POSE_LIMITS,
  type SceneLayer,
  type SceneLayerAssetAnimation,
  type SceneLayerIdleMotion,
  type SceneLayerMotionFrame,
  type SceneLayerPoseResponse,
  type SceneLayerSize,
  type ScenePose,
  type Vec3,
  validateSceneLayers,
} from "@/components/mascot-scene/scene";
import { parseSceneConfig, serializeSceneConfig } from "./sceneConfig";

type VectorKey = "position" | "pivot" | "rotation";
type Axis = keyof Vec3;
type SizeAxis = keyof SceneLayerSize;

const ZERO_POSE: ScenePose = { yaw: 0, pitch: 0, roll: 0 };
const INITIAL_POSE: ScenePose = { yaw: 14, pitch: 0, roll: 0 };

export function MascotSceneLab() {
  const [layers, setLayers] = useState(() => createMascotSceneV1().layers);
  const [selectedLayerID, setSelectedLayerID] = useState("face-error-eyes-plane");
  const { pose, setTargetPose, targetPose } = useSmoothedScenePose(INITIAL_POSE);
  const [poseInputMode, setPoseInputMode] = useState<PoseInputMode>("manual");
  const [scenePivot, setScenePivot] = useState<Vec3>(() => createMascotSceneV1().scenePivot);
  const [size, setSize] = useState(240);
  const [dark, setDark] = useState(false);
  const [mood, setMood] = useState<MascotMood>("idle");
  const [configMessage, setConfigMessage] = useState("");
  const importInputRef = useRef<HTMLInputElement>(null);
  const selectedLayer = layers.find((layer) => layer.id === selectedLayerID) ?? layers[0];
  const errors = useMemo(() => validateSceneLayers(layers), [layers]);
  const zRange = selectedLayer ? getLayerZRange(selectedLayer) : null;
  const hasChildren = selectedLayer ? layers.some((layer) => layer.parentID === selectedLayer.id) : false;

  const updateSelected = (update: (layer: SceneLayer) => SceneLayer) => {
    setLayers((current) => current.map((layer) => layer.id === selectedLayerID ? update(layer) : layer));
  };

  const updateVector = (key: VectorKey, axis: Axis, value: number) => {
    updateSelected((layer) => ({ ...layer, [key]: { ...layer[key], [axis]: value } }));
  };

  const updateSize = (axis: SizeAxis, value: number) => {
    updateSelected((layer) => ({ ...layer, size: { ...layer.size, [axis]: value } }));
  };

  const updatePoseResponse = (key: keyof SceneLayerPoseResponse, value: number) => {
    updateSelected((layer) => ({
      ...layer,
      poseResponse: {
        pitchTranslateY: 0,
        yawScaleX: 0,
        yawTranslateX: 0,
        ...layer.poseResponse,
        [key]: value,
      },
    }));
  };

  const setIdleMotionEnabled = (enabled: boolean) => {
    updateSelected((layer) => ({
      ...layer,
      idleMotion: enabled ? layer.idleMotion ?? {
        durationMs: 3600,
        delayMs: 0,
        rest: { translateX: 0, translateY: 0, rotateZ: -2 },
        peak: { translateX: 0, translateY: 0, rotateZ: 2 },
      } : undefined,
    }));
  };

  const updateIdleMotionTiming = (key: "durationMs" | "delayMs", value: number) => {
    updateSelected((layer) => layer.idleMotion ? {
      ...layer,
      idleMotion: { ...layer.idleMotion, [key]: value },
    } : layer);
  };

  const updateIdleMotionFrame = (frame: "rest" | "peak", key: keyof SceneLayerMotionFrame, value: number) => {
    updateSelected((layer) => layer.idleMotion ? {
      ...layer,
      idleMotion: {
        ...layer.idleMotion,
        [frame]: { ...layer.idleMotion[frame], [key]: value },
      },
    } : layer);
  };

  const setAssetAnimationPreset = (preset: SceneLayerAssetAnimation["preset"] | null) => {
    const defaults = preset === "mouth-idle"
      ? { durationMs: 7200, delayMs: 1100 }
      : { durationMs: 6000, delayMs: 0 };
    updateSelected((layer) => ({
      ...layer,
      assetAnimation: preset ? {
        preset,
        ...defaults,
      } : undefined,
    }));
  };

  const updateAssetAnimationTiming = (key: "durationMs" | "delayMs", value: number) => {
    updateSelected((layer) => layer.assetAnimation ? {
      ...layer,
      assetAnimation: { ...layer.assetAnimation, [key]: value },
    } : layer);
  };

  const addLayer = () => {
    const usedIDs = new Set(layers.map((layer) => layer.id));
    let index = layers.length + 1;
    while (usedIDs.has(`layer-${index}`)) index += 1;
    const id = `layer-${index}`;
    const layer: SceneLayer = {
      assetID: null,
      id,
      label: `自定义层 ${index}`,
      parentID: selectedLayer?.id ?? null,
      position: { x: 0, y: 0, z: 4 },
      size: { width: 48, height: 36, depth: 2 },
      pivot: { x: 0, y: 0, z: 0 },
      rotation: { x: 0, y: 0, z: 0 },
      debugColor: "#ec4899",
    };
    setLayers((current) => [...current, layer]);
    setSelectedLayerID(id);
  };

  const removeSelected = () => {
    if (!selectedLayer || hasChildren) return;
    const nextLayers = layers.filter((layer) => layer.id !== selectedLayer.id);
    setLayers(nextLayers);
    setSelectedLayerID(selectedLayer.parentID ?? nextLayers[0]?.id ?? "");
  };

  const resetScene = () => {
    const scene = createMascotSceneV1();
    setLayers(scene.layers);
    setSelectedLayerID("face-error-eyes-plane");
    setPoseInputMode("manual");
    setTargetPose(INITIAL_POSE);
    setScenePivot(scene.scenePivot);
    setMood("idle");
  };

  const exportConfig = () => {
    const url = URL.createObjectURL(new Blob([serializeSceneConfig(scenePivot, layers)], { type: "application/json" }));
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `${MASCOT_SCENE_V1_ID}.json`;
    anchor.click();
    URL.revokeObjectURL(url);
    setConfigMessage(`已导出 ${MASCOT_SCENE_V1_ID}.json`);
  };

  const importConfig = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const input = event.currentTarget;
    const file = input.files?.[0];
    if (!file) return;
    try {
      const config = parseSceneConfig(await file.text());
      setLayers(config.layers);
      setScenePivot(config.scenePivot);
      setSelectedLayerID((current) => config.layers.some((layer) => layer.id === current) ? current : config.layers[0].id);
      setConfigMessage(`已导入 ${file.name}`);
    } catch (error) {
      setConfigMessage(`导入失败:${error instanceof Error ? error.message : "配置格式无效"}`);
    } finally {
      input.value = "";
    }
  };

  return (
    <main className={dark ? "dark mascot-scene-lab" : "mascot-scene-lab"}>
      <header className="mascot-scene-lab-header">
        <div>
          <p className="mascot-scene-eyebrow">Development only</p>
          <h1>Mascot 2.5D Scene Lab</h1>
          <p>一个 mood 状态统一驱动正常眼睛、思考眼睛和错误表情。</p>
        </div>
        <div className="mascot-scene-header-controls">
          <div className="mascot-scene-actions">
            <button onClick={exportConfig} type="button">导出配置</button>
            <button onClick={() => importInputRef.current?.click()} type="button">导入配置</button>
            <input
              accept="application/json,.json"
              className="mascot-scene-import-input"
              onChange={importConfig}
              ref={importInputRef}
              type="file"
            />
          </div>
          <label className="mascot-scene-toggle">
            <input checked={dark} onChange={(event) => setDark(event.currentTarget.checked)} type="checkbox" />
            深色主题
          </label>
          {configMessage ? <span aria-live="polite" className="mascot-scene-config-message">{configMessage}</span> : null}
        </div>
      </header>

      <section className="mascot-scene-toolbar">
        <div aria-label="姿态输入" className="mascot-scene-input-control" role="group">
          <span>姿态</span>
          <button
            aria-pressed={poseInputMode === "manual"}
            className={poseInputMode === "manual" ? "is-selected" : undefined}
            onClick={() => {
              setPoseInputMode("manual");
              setTargetPose(pose);
            }}
            type="button"
          >
            手动
          </button>
          <button
            aria-pressed={poseInputMode === "pointer"}
            className={poseInputMode === "pointer" ? "is-selected" : undefined}
            onClick={() => {
              setPoseInputMode("pointer");
              setTargetPose(ZERO_POSE);
            }}
            type="button"
          >
            跟随鼠标
          </button>
        </div>
        <PoseField disabled={poseInputMode !== "manual"} label="Yaw" max={SCENE_POSE_LIMITS.yaw} min={-SCENE_POSE_LIMITS.yaw} value={targetPose.yaw} onChange={(yaw) => setTargetPose({ ...targetPose, yaw })} />
        <PoseField disabled={poseInputMode !== "manual"} label="Pitch" max={SCENE_POSE_LIMITS.pitch} min={-SCENE_POSE_LIMITS.pitch} value={targetPose.pitch} onChange={(pitch) => setTargetPose({ ...targetPose, pitch })} />
        <PoseField disabled={poseInputMode !== "manual"} label="Roll" max={SCENE_POSE_LIMITS.roll} min={-SCENE_POSE_LIMITS.roll} value={targetPose.roll} onChange={(roll) => setTargetPose({ ...targetPose, roll })} />
        <label className="mascot-scene-field">
          <span>尺寸</span>
          <select value={size} onChange={(event) => setSize(Number(event.currentTarget.value))}>
            {[32, 48, 120, 240].map((value) => <option key={value} value={value}>{value}px</option>)}
          </select>
        </label>
        <button disabled={poseInputMode !== "manual"} onClick={() => setTargetPose({ yaw: -SCENE_POSE_LIMITS.yaw, pitch: 0, roll: 0 })} type="button">左极限</button>
        <button disabled={poseInputMode !== "manual"} onClick={() => setTargetPose(ZERO_POSE)} type="button">正面</button>
        <button disabled={poseInputMode !== "manual"} onClick={() => setTargetPose({ yaw: SCENE_POSE_LIMITS.yaw, pitch: 0, roll: 0 })} type="button">右极限</button>
        <div aria-label="表情状态" className="mascot-scene-mood-control" role="group">
          <span>表情</span>
          {MASCOT_MOOD_OPTIONS.map((option) => (
            <button
              aria-pressed={mood === option.id}
              className={mood === option.id ? "is-selected" : undefined}
              key={option.id}
              onClick={() => setMood(option.id)}
              type="button"
            >
              {option.label}
            </button>
          ))}
        </div>
      </section>

      <section className="mascot-scene-scene-config">
        <div>
          <h2>全局旋转点</h2>
          <p>`scenePivot.z` 可以位于所有层之间；层厚度不使用负数。</p>
        </div>
        <VectorEditor
          label="scenePivot"
          value={scenePivot}
          onChange={(axis, value) => setScenePivot((current) => ({ ...current, [axis]: value }))}
        />
      </section>

      <section className="mascot-scene-comparison">
        <PreviewCard
          description="Idle 使用正常眼睛，Thinking 让三个方点依次点亮，Error 切换面罩、眼睛和嘴型。"
          onPointerLeave={() => {
            if (poseInputMode === "pointer") setTargetPose(ZERO_POSE);
          }}
          onPointerMove={(event) => {
            if (poseInputMode !== "pointer") return;
            setTargetPose(getPointerTargetPose(event.clientX, event.clientY, event.currentTarget.getBoundingClientRect()));
          }}
          pointerTracking={poseInputMode === "pointer"}
          title="统一表情状态 V1 Scene"
        >
          <MascotScenePrototype
            layers={layers}
            mood={mood}
            pose={pose}
            scenePivot={scenePivot}
            size={size}
          />
        </PreviewCard>
      </section>

      <section className="mascot-scene-editor">
        <aside className="mascot-scene-layer-list">
          <div className="mascot-scene-section-heading">
            <div>
              <h2>自定义层</h2>
              <p>层级来自配置数组，不固定为前、中、后。</p>
            </div>
            <button onClick={addLayer} type="button">新增层</button>
          </div>
          {layers.map((layer) => (
            <button
              className={layer.id === selectedLayerID ? "is-selected" : undefined}
              key={layer.id}
              onClick={() => setSelectedLayerID(layer.id)}
              type="button"
            >
              <span>{layer.label}</span>
              <small>{layer.id}</small>
            </button>
          ))}
        </aside>

        {selectedLayer ? (
          <div className="mascot-scene-layer-form">
            <div className="mascot-scene-section-heading">
              <div>
                <h2>{selectedLayer.label}</h2>
                <p>`position.z` 是中心距离，`size.depth` 是 Z 轴厚度。</p>
              </div>
              <div className="mascot-scene-actions">
                <button disabled={hasChildren} onClick={removeSelected} type="button">删除</button>
                <button onClick={resetScene} type="button">重置</button>
              </div>
            </div>

            <div className="mascot-scene-form-grid">
              <label className="mascot-scene-field">
                <span>名称</span>
                <input
                  value={selectedLayer.label}
                  onChange={(event) => updateSelected((layer) => ({ ...layer, label: event.currentTarget.value }))}
                />
              </label>
              <label className="mascot-scene-field">
                <span>父层</span>
                <select
                  value={selectedLayer.parentID ?? ""}
                  onChange={(event) => updateSelected((layer) => ({ ...layer, parentID: event.currentTarget.value || null }))}
                >
                  <option value="">sceneRoot</option>
                  {layers.filter((layer) => layer.id !== selectedLayer.id).map((layer) => (
                    <option key={layer.id} value={layer.id}>{layer.label}</option>
                  ))}
                </select>
              </label>
              <label className="mascot-scene-field">
                <span>内容</span>
                <select
                  value={selectedLayer.assetID ?? ""}
                  onChange={(event) => updateSelected((layer) => ({
                    ...layer,
                    assetID: (event.currentTarget.value || null) as SceneAssetID | null,
                  }))}
                >
                  <option value="">调试平面</option>
                  {SCENE_ASSET_OPTIONS.map((asset) => <option key={asset.id} value={asset.id}>{asset.label}</option>)}
                </select>
              </label>
              <label className="mascot-scene-field">
                <span>调试颜色</span>
                <input
                  type="color"
                  value={selectedLayer.debugColor}
                  onChange={(event) => updateSelected((layer) => ({ ...layer, debugColor: event.currentTarget.value }))}
                />
              </label>
            </div>

            <VectorEditor label="中心位置 position" value={selectedLayer.position} onChange={(axis, value) => updateVector("position", axis, value)} />
            <SizeEditor value={selectedLayer.size} onChange={updateSize} />
            <VectorEditor label="旋转中心 pivot" value={selectedLayer.pivot} onChange={(axis, value) => updateVector("pivot", axis, value)} />
            <VectorEditor label="局部旋转 rotation" value={selectedLayer.rotation} onChange={(axis, value) => updateVector("rotation", axis, value)} />
            <PoseResponseEditor response={selectedLayer.poseResponse} onChange={updatePoseResponse} />
            <IdleMotionEditor
              motion={selectedLayer.idleMotion}
              onEnabledChange={setIdleMotionEnabled}
              onFrameChange={updateIdleMotionFrame}
              onTimingChange={updateIdleMotionTiming}
            />
            <AssetAnimationEditor
              animation={selectedLayer.assetAnimation}
              onPresetChange={setAssetAnimationPreset}
              onTimingChange={updateAssetAnimationTiming}
            />

            {zRange ? (
              <div className="mascot-scene-derived">
                <span>推导远端 Z:`{zRange.farZ.toFixed(2)}`</span>
                <span>推导近端 Z:`{zRange.nearZ.toFixed(2)}`</span>
              </div>
            ) : null}
            {hasChildren ? <p className="mascot-scene-note">该层包含子层，需先移动或删除子层。</p> : null}
          </div>
        ) : null}
      </section>

      {errors.length > 0 ? (
        <section className="mascot-scene-errors">
          {errors.map((error) => <div key={error}>{error}</div>)}
        </section>
      ) : null}
    </main>
  );
}

function PreviewCard({
  children,
  description,
  onPointerLeave,
  onPointerMove,
  pointerTracking,
  title,
}: {
  children: React.ReactNode;
  description: string;
  onPointerLeave: React.PointerEventHandler<HTMLDivElement>;
  onPointerMove: React.PointerEventHandler<HTMLDivElement>;
  pointerTracking: boolean;
  title: string;
}) {
  return (
    <article className="mascot-scene-card">
      <div>
        <h2>{title}</h2>
        <p>{description}</p>
      </div>
      <div
        className="mascot-scene-preview-stage"
        data-pointer-tracking={pointerTracking || undefined}
        onPointerLeave={onPointerLeave}
        onPointerMove={onPointerMove}
      >
        {children}
      </div>
    </article>
  );
}

function PoseField({ disabled, label, max, min, onChange, value }: { disabled: boolean; label: string; max: number; min: number; onChange: (value: number) => void; value: number }) {
  return (
    <label className="mascot-scene-field mascot-scene-pose-field">
      <span>{label}: {value.toFixed(1)}°</span>
      <input disabled={disabled} max={max} min={min} step="0.5" type="range" value={value} onChange={(event) => onChange(Number(event.currentTarget.value))} />
    </label>
  );
}

function VectorEditor({ label, onChange, value }: { label: string; onChange: (axis: Axis, value: number) => void; value: Vec3 }) {
  return (
    <fieldset className="mascot-scene-vector">
      <legend>{label}</legend>
      {(["x", "y", "z"] as const).map((axis) => (
        <NumberField key={axis} label={axis.toUpperCase()} value={value[axis]} onChange={(next) => onChange(axis, next)} />
      ))}
    </fieldset>
  );
}

function SizeEditor({ onChange, value }: { onChange: (axis: SizeAxis, value: number) => void; value: SceneLayerSize }) {
  return (
    <fieldset className="mascot-scene-vector">
      <legend>三维尺寸 size</legend>
      {(["width", "height", "depth"] as const).map((axis) => (
        <NumberField key={axis} label={axis} min={axis === "depth" ? 0 : 0.5} value={value[axis]} onChange={(next) => onChange(axis, next)} />
      ))}
    </fieldset>
  );
}

function PoseResponseEditor({
  onChange,
  response,
}: {
  onChange: (key: keyof SceneLayerPoseResponse, value: number) => void;
  response?: SceneLayerPoseResponse;
}) {
  return (
    <fieldset className="mascot-scene-vector">
      <legend>姿态响应 poseResponse</legend>
      <NumberField label="yaw → X" value={response?.yawTranslateX ?? 0} onChange={(value) => onChange("yawTranslateX", value)} />
      <NumberField label="pitch → Y" value={response?.pitchTranslateY ?? 0} onChange={(value) => onChange("pitchTranslateY", value)} />
      <NumberField label="yaw 压缩 X" min={0} value={response?.yawScaleX ?? 0} onChange={(value) => onChange("yawScaleX", value)} />
    </fieldset>
  );
}

function IdleMotionEditor({
  motion,
  onEnabledChange,
  onFrameChange,
  onTimingChange,
}: {
  motion?: SceneLayerIdleMotion;
  onEnabledChange: (enabled: boolean) => void;
  onFrameChange: (frame: "rest" | "peak", key: keyof SceneLayerMotionFrame, value: number) => void;
  onTimingChange: (key: "durationMs" | "delayMs", value: number) => void;
}) {
  return (
    <fieldset className="mascot-scene-vector mascot-scene-motion-editor">
      <legend>空闲动画 idleMotion</legend>
      <label className="mascot-scene-toggle">
        <input checked={Boolean(motion)} onChange={(event) => onEnabledChange(event.currentTarget.checked)} type="checkbox" />
        启用
      </label>
      {motion ? (
        <>
          <NumberField label="周期 ms" min={100} value={motion.durationMs} onChange={(value) => onTimingChange("durationMs", value)} />
          <NumberField label="延迟 ms" value={motion.delayMs} onChange={(value) => onTimingChange("delayMs", value)} />
          <MotionFrameEditor frame="rest" label="静止帧" value={motion.rest} onChange={onFrameChange} />
          <MotionFrameEditor frame="peak" label="峰值帧" value={motion.peak} onChange={onFrameChange} />
        </>
      ) : null}
    </fieldset>
  );
}

function MotionFrameEditor({
  frame,
  label,
  onChange,
  value,
}: {
  frame: "rest" | "peak";
  label: string;
  onChange: (frame: "rest" | "peak", key: keyof SceneLayerMotionFrame, value: number) => void;
  value: SceneLayerMotionFrame;
}) {
  return (
    <div className="mascot-scene-motion-frame">
      <span>{label}</span>
      <NumberField label="X" value={value.translateX} onChange={(next) => onChange(frame, "translateX", next)} />
      <NumberField label="Y" value={value.translateY} onChange={(next) => onChange(frame, "translateY", next)} />
      <NumberField label="旋转 Z" value={value.rotateZ} onChange={(next) => onChange(frame, "rotateZ", next)} />
    </div>
  );
}

function AssetAnimationEditor({
  animation,
  onPresetChange,
  onTimingChange,
}: {
  animation?: SceneLayerAssetAnimation;
  onPresetChange: (preset: SceneLayerAssetAnimation["preset"] | null) => void;
  onTimingChange: (key: "durationMs" | "delayMs", value: number) => void;
}) {
  return (
    <fieldset className="mascot-scene-vector">
      <legend>素材动画 assetAnimation</legend>
      <label className="mascot-scene-field">
        <span>预设</span>
        <select
          value={animation?.preset ?? ""}
          onChange={(event) => onPresetChange(event.currentTarget.value ? event.currentTarget.value as SceneLayerAssetAnimation["preset"] : null)}
        >
          <option value="">无</option>
          <option value="blink">眨眼</option>
          <option value="mouth-idle">嘴部空闲</option>
        </select>
      </label>
      {animation ? (
        <>
          <NumberField label="周期 ms" min={100} value={animation.durationMs} onChange={(value) => onTimingChange("durationMs", value)} />
          <NumberField label="延迟 ms" value={animation.delayMs} onChange={(value) => onTimingChange("delayMs", value)} />
        </>
      ) : null}
    </fieldset>
  );
}

function NumberField({ label, min, onChange, value }: { label: string; min?: number; onChange: (value: number) => void; value: number }) {
  return (
    <label className="mascot-scene-field">
      <span>{label}</span>
      <input min={min} step="0.5" type="number" value={value} onChange={(event) => onChange(Number(event.currentTarget.value))} />
    </label>
  );
}
