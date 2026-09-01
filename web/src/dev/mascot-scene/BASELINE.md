# Mascot Scene Lab 生产基线

记录时间:2026-09-01

## 文件哈希

```text
ca6d4eb662c552f5fd7d69928cf51fdaf356b430996c0571cc341f231161df81  web/src/components/Mascot.tsx
2af071568dd8414597ccacc88c1995fc9faecabc108ca4064582819b7b2b4be6  web/src/styles.css
```

Lab 阶段不得修改这两个文件。每个阶段完成后重新执行:

```bash
shasum -a 256 web/src/components/Mascot.tsx web/src/styles.css
```

## 当前公开行为

- `className`
- `gaze`
- `inputPitchBias`
- `mood`
- `headShakeSignal`
- `ambientMotion`
- `pointerTracking`
- `onPointerGaze`
- `showFaceDebugFrame`
- `showHeadDebugFrame`

## 当前调用位置

- `web/src/components/Composer.tsx`
- `web/src/components/DraftConversation.tsx`

Lab 可以导入当前 `Mascot` 作为只读对比基线，但不得修改其内部实现或生产调用位置。
