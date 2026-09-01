# 吉祥物 2.5D Scene Lab 实施计划

> 状态:已完成（V1 生产替换）
> 日期:2026-09-01
> 实验入口:`web/dev/mascot-scene-lab.html`
> 生产基线:`web/src/components/Mascot.tsx`、`web/src/styles.css`

## 0. 结论

先在当前仓库内建立独立的 Mascot Scene Lab，验证轻量 2.5D 场景模型。Lab 不进入正式应用依赖链，不修改当前吉祥物，不新增产品开关，也不使用 worktree。

实验阶段只回答三个问题:

1. 前、中、后景深平面能否让外壳、面框和反光保持协调。
2. 左右侧面节点能否让远侧手臂自然后退、压缩并被身体遮挡。
3. 新场景模型能否覆盖当前 `Mascot` 的动作和调用契约，而不增加第二套运行时事实源。

验证通过后，再单独评审生产切换。切换必须一次完成并删除旧实现，不长期保留双轨。

## 1. 当前问题与实验假设

当前外壳、左右手臂使用同一个 `bodyBackShellStyle`，共享相同的 `translateZ` 和整体头部旋转。它们在坐标上同步，但仍然是共面的 SVG 图层。身体对两只手臂的遮挡顺序固定，不会根据转头方向改变。

因此侧身时会出现以下视觉问题:

- 远侧手臂仍保留接近正面的完整轮廓。
- 手臂像贴在外壳平面上一起横移，而不是从身体侧面伸出。
- 为某个角度单独调整路径，会影响正面或另一个侧面。

本实验采用以下假设:

- 吉祥物不需要 WebGL 或完整三维模型。
- 使用 DOM、SVG 和 CSS `preserve-3d` 即可建立足够的 2.5D 景深。
- 外壳可以使用前、中、后三个主平面。
- 手臂不能只归入三个平行面，必须是带肩部锚点的左右侧面节点。
- 浏览器负责透视投影；业务代码不手工计算每个平面的屏幕中心。

## 2. 隔离边界

### 2.1 实验阶段只读文件

以下生产文件在 Lab 实验阶段保持不变:

- `web/src/components/Mascot.tsx`
- `web/src/styles.css` 中现有吉祥物变量
- `web/src/components/Composer.tsx`
- `web/src/components/DraftConversation.tsx`
- 正式 TanStack Router 路由和 Electron 入口

开始实施时记录 `Mascot.tsx` 与 `styles.css` 的 SHA-256。每个阶段结束后重新计算，确认生产基线没有变化。

### 2.2 实验目录

```text
web/dev/mascot-scene-lab.html
web/src/dev/mascot-scene/
├─ BASELINE.md
├─ main.tsx
├─ MascotSceneLab.tsx
├─ MascotScenePrototype.tsx
├─ geometry.ts
├─ scene.ts
└─ transforms.ts
```

职责:

- `main.tsx`:Lab 独立入口，只导入 Lab 和全局样式。
- `MascotSceneLab.tsx`:对比布局、姿态控制和调试开关。
- `MascotScenePrototype.tsx`:新场景树和实验 SVG。
- `geometry.ts`:实验版几何路径。实验期间允许与生产基线暂时重复，切换时必须删除重复源。
- `scene.ts`:场景节点、父子关系、景深和肩部锚点。
- `transforms.ts`:统一生成节点变换，不包含 React 状态。

### 2.3 不进入产品路径

- 正式页面不得导入 `@/dev/mascot-scene/*`。
- Lab 不注册产品路由，不显示在设置、菜单或开发者开关中。
- Lab 只通过 Vite 开发服务器访问:`/dev/mascot-scene-lab.html`。
- 默认 `vite build` 仍只使用正式 `index.html` 入口。
- 构建后检查 `web/dist`，不得包含 `mascot-scene-lab` 或 `MascotScenePrototype`。
- 不增加 feature flag、localStorage 开关、fallback 或旧新版运行时选择。

## 3. 场景模型

### 3.1 坐标约定

- 设计坐标继续使用当前 `128 × 128` viewBox。
- 世界原点使用头部视觉中心:`(64, 77, 0)`。
- `X` 向右，`Y` 向下，`Z` 正方向朝向观察者。
- 左右转头绕 `Y` 轴。
- 上下点头绕 `X` 轴。
- 轻微侧倾绕 `Z` 轴。
- 初始透视距离沿用当前 `170px`，只允许在 Lab 中调参。

所有节点共享一个 `ScenePose`:

```ts
type ScenePose = {
  yaw: number;
  pitch: number;
  roll: number;
};
```

姿态是唯一动画事实源。眼神输入、指针输入和错误摇头最终都只产生一个 `ScenePose`，节点不得各自保存另一套 yaw/pitch。

### 3.2 可自定义节点模型

场景不内置固定的前、中、后三层。所有层都来自同一个可编辑配置数组，通过 `parentID` 组成任意树。前、中、后只是第一组验证配置，不是引擎类型。

```ts
type Vec3 = {
  x: number;
  y: number;
  z: number;
};

type SceneNode = {
  id: string;
  parentID: string | null;
  position: Vec3;
  size: {
    width: number;
    height: number;
    depth: number;
  };
  pivot: Vec3;
  rotation: Vec3;
};
```

参数语义:

- `position`:该层中心相对父级原点的位置。
- `position.z`:层中心距离父级原点的 Z 距离。
- `size.width/height`:层在自身局部平面的实际尺寸。
- `size.depth`:层沿 Z 轴的实际厚度；纯平面可以为 `0`。
- `pivot`:相对层中心的局部旋转点。
- `rotation`:相对父级的局部 X/Y/Z 旋转。

近端和远端位置不作为配置保存，而是统一推导:

```text
nearZ = position.z + size.depth / 2
farZ  = position.z - size.depth / 2
```

这样中心距离与 Z 轴厚度是唯一事实源，修改任一参数都不会造成近端、远端数据不一致。透视后的屏幕尺寸继续由相机距离和 Z 位置自动推导，不单独保存“远处显示尺寸”。

`size.depth` 是长度，必须大于等于 `0`，不承担方向语义。前后方向只由带符号的 `position.z` 表达:

- `position.z > 0`:层中心位于原点前方，靠近观察者。
- `position.z = 0`:层中心位于原点平面。
- `position.z < 0`:层中心位于原点后方，远离观察者。

整体头部另有独立的 `scenePivot: Vec3`。它是所有根层共享的旋转点，`scenePivot.z` 可以处于最底层前方、层与层之间或某个层内部，不要求与任何层中心重合。节点自身的 `pivot` 只负责该节点的局部旋转。

示例:

```text
scenePivot.z = 0
后壳 position.z = -6, size.depth = 4  -> Z 范围 [-8, -4]
前层 position.z = 5,  size.depth = 2  -> Z 范围 [4, 6]
```

两层围绕同一个 Y 轴转头时，负 Z 的后壳与正 Z 的前层会产生方向相反的横向视差。该效果由统一三维旋转自然产生，不为前层或后层添加单独的位移公式。

第一版示例节点拓扑:

```text
sceneRoot
└─ headPivot
   ├─ rearPlane
   ├─ leftArmNode
   ├─ bodyPlane
   ├─ rightArmNode
   └─ frontPlane
      └─ faceGlassPlane
```

节点职责:

| 节点 | 初始职责 | 关键约束 |
| --- | --- | --- |
| `rearPlane` | 后壳和后部结构 | 位于身体中心之后 |
| `bodyPlane` | 主外壳、轮廓和金属反光 | 作为手臂根部的中央遮挡面 |
| `frontPlane` | 面框、表情和前部结构 | 位于身体之前 |
| `faceGlassPlane` | 面屏玻璃内凹 | 相对前面板轻微后退 |
| `leftArmNode` | 左手臂 | 以左肩为锚点，具有侧面朝向 |
| `rightArmNode` | 右手臂 | 以右肩为锚点，具有侧面朝向 |

三个主平面是平行的；左右手臂不是平行面。手臂节点从身体两侧向后倾斜，整体转头后由透视自然形成近侧和远侧差异。

### 3.3 变换职责

场景根节点只负责透视。`headPivot` 统一应用 yaw、pitch 和 roll。子节点只保存相对头部的静态位置、局部朝向和锚点。

概念顺序:

```text
世界变换
  = 头部中心平移
  × 头部 yaw/pitch/roll
  × 节点局部位置
  × 节点局部旋转
  × 节点锚点修正
```

实现时由 `transforms.ts` 统一生成 CSS transform，避免各 SVG 层拼接自己的变换字符串。CSS `perspective` 和 `preserve-3d` 负责投影，不再手工推导每一层的屏幕中心。

手臂允许使用静态侧面角度和肩部锚点，但第一版不增加弹簧、骨骼、物理或独立动画循环。

## 4. Lab 页面

### 4.1 预览区

第一版只显示 Scene Prototype:

- 后面、中面、前面三个不同颜色的正方形。
- 每个正方形内部的水平和垂直中心线。
- 32px、48px、120px 和 240px 四种尺寸。
- 浅色和深色背景。

Lab 不导入当前吉祥物。生产版仅通过文件哈希保持只读基线，进入外观迁移阶段后再决定是否增加独立对比视图。

### 4.2 控制项

第一版只提供必要控件:

- yaw:`-20°` 到 `20°`。
- pitch:`-16°` 到 `16°`。
- roll:`-4°` 到 `4°`。
- 固定姿态预设:`左极限、左半侧、正面、右半侧、右极限`。
- 自动左右扫动。
- 尺寸选择。
- 浅色/深色主题。
- `idle/thinking/ready/error` 状态。
- 平面边界、坐标轴、中心点和肩部锚点调试显示。

Lab 控件使用原生表单元素即可，不为开发页引入新的 UI 依赖。

### 4.3 调试显示

启用调试后显示:

- 前、中、后平面的半透明边界和 Z 值。
- 世界原点及 `headPivot`。
- 左右肩部锚点。
- 每个节点的局部 X/Y/Z 轴。
- 当前 yaw、pitch、roll 数值。

调试图层只存在于 Prototype，不进入最终吉祥物。

## 5. 实施阶段

### 阶段 0:冻结生产基线

工作:

1. 记录生产 `Mascot.tsx` 与吉祥物 CSS 的 SHA-256。
2. 保存当前正面、左右半侧、左右极限截图。
3. 覆盖 48px 和 120px、浅色和深色主题。
4. 记录当前公开 Props 与两个调用位置。

完成条件:

- 基线截图可重复生成。
- 当前构建通过。
- 尚未新增 Lab 以外的代码改动。

### 阶段 1:建立隔离的 Lab 入口

工作:

1. 创建 `web/dev/mascot-scene-lab.html`。
2. 创建 Lab React 入口和对比布局。
3. 只渲染开发场景，不导入当前生产版吉祥物。

完成条件:

- Lab 可通过 Vite 独立访问。
- 正式应用页面和产物不包含 Lab。
- 生产基线文件哈希未变化。

### 阶段 2:验证场景核心

工作:

1. 定义 `ScenePose`、`SceneNode` 和统一变换函数。
2. 先使用三个不同颜色的正方形验证后、中、前三个面，每个正方形绘制水平和垂直中心线。
3. 添加左右侧面测试节点和肩部锚点。
4. 验证 yaw/pitch 下的景深、透视和遮挡方向。

本阶段不复制吉祥物 SVG。

完成条件:

- 所有节点只消费同一个 `ScenePose`。
- 转头时前后关系正确，没有固定 z-index 补丁。
- 一帧内不读取布局，不通过 React state 驱动连续动画。

### 阶段 3:迁移静态外观到 Prototype

工作:

1. 将当前几何复制到 Lab 的 `geometry.ts`。
2. 先完成正面静态姿态。
3. 分别放置后壳、主外壳、面框、面屏和反光。
4. 保留当前 CSS 颜色变量，只调整场景位置，不重新设计颜色。

完成条件:

- yaw/pitch 为零时，Prototype 与生产版外观基本一致。
- 32px 和 48px 下没有新增杂线。
- 金属渐变和高光不漂移。

### 阶段 4:迁移左右手臂

工作:

1. 将左右手臂放入独立侧面节点。
2. 使用肩部锚点确定局部旋转中心。
3. 让身体平面负责根部遮挡。
4. 只调节点位置、局部角度和深度，不按具体 yaw 修改 SVG 路径。

完成条件:

- 远侧手臂随转头自然后退并缩短。
- 近侧手臂保持与身体连接。
- 正面左右对称。
- 左右极限都没有缺口、穿帮或手掌分离。
- 不存在“左转一套路径、右转另一套路径”。

阶段 4 完成后必须由用户确认手臂观感，未确认前不迁移表情和其他动画。

### 阶段 5:补齐现有行为

按顺序迁移:

1. 面框与玻璃景深。
2. 眼神跟随。
3. pitch、roll 和阴影。
4. thinking/ready/error 状态。
5. 点击回弹、摆头和错误摇头。
6. 天线动作。

完成条件:

- Prototype 覆盖当前 `MascotProps` 的全部行为。
- `ambientMotion={false}` 时不启动无关动画。
- 只有现有 requestAnimationFrame 驱动姿态缓动。
- 每个行为只有一个事实源。

### 阶段 6:切换评审

本阶段只输出对比结果和切换清单，不直接修改生产调用方。

评审通过后再执行一次性切换:

1. 将通过验证的场景代码从 `src/dev` 移到 `src/components/mascot`。
2. 保持当前公开 Props 语义。
3. 迁移 `Composer.tsx` 和 `DraftConversation.tsx` 的 import。
4. 删除旧 `components/Mascot.tsx`。
5. 删除 Lab 中重复的 Prototype、几何和变换代码。
6. 不保留 feature flag、fallback 或两套 Mascot。

## 6. 验收矩阵

| 维度 | 场景 |
| --- | --- |
| yaw | `-20°`、`-10°`、`0°`、`10°`、`20°` |
| pitch | `-16°`、`0°`、`16°` |
| 尺寸 | 32px、48px、120px、240px |
| 主题 | 浅色、深色 |
| 状态 | idle、thinking、ready、error |
| 动作 | gaze、点击回弹、摆头、错误摇头、天线 |

重点观察:

- 远侧手臂的长度、景深和身体遮挡。
- 近侧手臂是否从肩部自然伸出。
- 底壳轮廓是否完整。
- 前后壳反光是否在转头时保持连续。
- 面框和玻璃是否出现漂移。
- 48px 下是否产生额外锯齿或杂线。
- 连续扫动时是否发生跳层、闪烁或布局抖动。

## 7. 性能与实现约束

- 不引入 Three.js、WebGL、Canvas 渲染器或物理引擎。
- 不新增通用动画状态库。
- 连续姿态更新不得触发 React 重渲染。
- 帧循环中不得调用 `getBoundingClientRect`。
- 场景节点使用静态配置，姿态使用 ref 和统一帧循环。
- 每个 SVG gradient ID 必须保持实例唯一。
- 避免在 `preserve-3d` 子树中引入会意外创建平面化上下文的 filter、opacity 容器或无关 z-index。
- 当前动画关闭语义、pointer tracking 和错误态锁定语义保持不变。

## 8. 每阶段验证命令

```bash
npm --prefix web run build
git diff --check
rg "mascot-scene-lab|MascotScenePrototype" web/dist
```

最后一条命令预期无输出。

桌面视觉检查必须确认页面来自当前 Vite dev server，不使用已安装的发布版应用。

## 9. 停止条件

出现以下任一情况，停止当前阶段并先解决根因:

- 生产基线文件在 Lab 阶段发生变化。
- Lab 被正式应用入口或路由引用。
- 为某个角度增加专用 SVG 路径或显示/隐藏分支。
- 节点各自保存 yaw/pitch，形成多个姿态事实源。
- 远侧手臂仍依赖固定 DOM 顺序模拟景深。
- 新版正面明显偏离当前已确认造型。
- 48px 表现变差。
- 为保留旧版而增加运行时 fallback。

## 10. 非目标

本次 Lab 不负责:

- 建设通用开源 3D 引擎。
- 修改吉祥物颜色、脸型、表情设计或身体比例。
- 增加骨骼、IK、布料或物理模拟。
- 增加移动端兼容路径。
- 在实验未完成前修改产品中的现有吉祥物。

## 11. 第一实施项

阶段 0、阶段 1 和阶段 2 已完成。阶段 3 已将主体后壳、金属高光、左右手臂、双层面罩、静态表情、面罩反光和分层天线拆成独立节点。两只手臂位于后壳与前面罩之间的中层，开放的内端与后壳重叠形成连续连接；各自保存肩部旋转点，为后续动作保留唯一关节来源。Lab 以唯一的 `mood` 状态驱动生产中实际使用的 `idle / thinking / error`：`thinking` 用三个固定位置的深色方点依次明暗变化表现思考，不绘制底槽或边框；`error` 切换面罩、错误眼睛和错误嘴型并隐藏腮红；天线不再承担状态提示。手臂动作暂缓，几何只复制到 Lab 的 `geometry.ts`，生产吉祥物保持不变。

## 12. 静态第一版验收

静态第一版已覆盖 `48 / 120 / 240px`、左极限/正面/右极限以及 `idle / thinking / error` 的 27 种组合。场景旋转中心仍由配置控制，但不再绘制红色调试标记。地面阴影和手臂动作暂不进入第一版。

## 13. 统一姿态驱动

Lab 的手动滑杆与鼠标跟随只生成 `targetPose`，缓动控制器输出唯一的渲染 `pose`。所有场景层只读取这份最终姿态。鼠标位置统一映射到 yaw/pitch 限幅，移出预览区后目标姿态回到正面；roll 暂时只由手动输入控制。

## 14. 新旧只读对照（替换前）

正式替换前，Lab 曾同时渲染生产吉祥物与 2.5D Scene。两者共享尺寸、mood 和姿态目标；生产组件只被导入，不修改其实现或调用位置。该对照用于判断新版在正面比例、转头协调和小尺寸清晰度上是否真实改善；完成替换后已随旧实现删除。

同一 yaw 数值不保证视觉角度一致。每个层可选配置 `poseResponse`，从唯一的全局 pose 推导 yaw 横移、pitch 纵移和 yaw 横向压缩。第一版只在前部面框根层启用，使整套面罩及其子层共同响应，不为左右方向建立独立分支。

## 15. 配置导入与导出

Lab 使用单一的版本化 JSON 配置保存 `scenePivot` 和完整 `layers` 数组。当前格式版本为 `1`。导入时先校验字段、素材 ID、数值范围和父层关系；校验失败不会覆盖当前场景。姿态、表情、预览尺寸和主题属于临时调试状态，不写入场景配置。

## 16. 层级空闲动画

层可选配置 `idleMotion`，包含周期、延迟、静止帧和峰值帧。每个帧描述 X/Y 位移与 Z 轴旋转，动画围绕该层自己的 `pivot` 循环。天线第一版使用生产版参数：`3600ms` 周期、`180ms` 延迟，在 `-3°` 与 `4°` 之间摆动。左右手臂使用 `3400ms` 周期，峰值分别上浮 `3px` 与 `2.7px`，右手延迟 `120ms`。系统减少动态效果时禁用这些动画。

复杂素材动画使用独立的 `assetAnimation`，避免把多关键帧行为塞进两帧往返的 `idleMotion`。正常眼睛层使用 `blink` 预设，以 `6000ms` 周期复现一次主眨眼和一次短双眨；Thinking 与 Error 使用独立眼睛素材，不参与该动画。普通嘴部使用 `mouth-idle` 预设，以 `7200ms` 周期和 `1100ms` 延迟完成一次轻微收缩与一次纵向拉伸；Idle、Thinking 共用，Error 切换为独立嘴型。

场景整体空闲动画位于姿态旋转层外，使用 `4800ms` 周期上浮 `1px` 并放大 `0.8%`。它不覆盖 Yaw/Pitch/Roll，也不影响天线、手臂及素材动画的独立 transform。系统减少动态效果时关闭整体动画。

## 17. 第一版状态过渡

mood 限定图层常驻场景，通过统一的激活态切换，不再在状态变化时直接卸载。表情使用 `180ms` 交叉淡入和 `220ms` 收拢过渡；面框与面罩颜色使用同一段 `180ms` 颜色过渡。Error 状态让整体下沉 `1.5px`，左右手臂分别向下收拢 `2deg`。系统减少动态效果时关闭这些过渡。

## 18. V1 场景预设边界

第一版吉祥物的旋转点、15 个层级、素材绑定和动画参数集中在 `mascotSceneV1.ts`，作为唯一的 V1 造型事实源。`scene.ts` 只保留通用场景类型、深拷贝、激活态、Z 范围和层级校验。Lab 每次初始化或重置都通过 `createMascotSceneV1()` 取得独立副本，编辑不会修改冻结的默认预设。

## 19. 第一版生产输入适配

`MascotSceneV1Adapter` 将 V1 预设绑定到与现有组件一致的 `pointer / input / center` 注视语义，并支持 `inputPitchBias`、`pointerTracking`、`onPointerGaze` 和 `ambientMotion`。三种输入统一转换成唯一的目标姿态，再由同一个平滑控制器驱动场景。适配器可读取 CSS 布局尺寸，不要求调用方传入像素尺寸。当前仍未替换生产调用点。

## 20. 第一版生产手势适配

点击弹跳、点击晃动和 Error 摇头统一作用于姿态层外的 `gesture-motion`，不会覆盖鼠标注视、状态下沉或空闲动画。点击反馈沿用 `2200ms` 双形态节奏；`headShakeSignal` 使用 `600ms` 的 3D Yaw 摇头，并先把唯一基础姿态归到 `Yaw 0° / Pitch -3.7°`。关闭 `ambientMotion` 或系统减少动态效果时不启动这些手势。

## 21. 渲染样式与绘制边界

渲染器所需的层级、状态、动画、素材和减少动态效果样式集中到 `scene.css`，由 `MascotScenePrototype` 自己导入；`lab.css` 只保留 Lab 布局和编辑器样式。正式适配器在组件尺寸外扩 `25%` 建立独立 paint containment，再将实际画面放回中央 `66.67%` 视口。这样 32px、48px、120px 和 240px 使用同一尺寸事实源，转头、手臂和点击手势不会被裁切，也不会扩大到页面其他交互区域的重绘范围。当前仍未修改生产吉祥物及其调用点。

## 22. Draft 单点灰度

通用场景引擎、V1 预设、渲染器、样式和生产输入适配器已从 `src/dev` 迁入 `src/components/mascot-scene`；Lab 仅保留编辑器和配置导入导出。`DraftConversation` 已直接使用 `MascotSceneV1Adapter`，没有 feature flag 或运行时 fallback；`Composer` 在本阶段暂时继续使用旧 `Mascot`。生产 Draft 的 48px、56px、64px 容器断点均使用现有 `.pudding-draft-mascot` 尺寸事实源完成回归，层级、绘制边界和点击反馈正常。

## 23. Composer 迁移

`Composer` 已直接使用 `MascotSceneV1Adapter`，沿用原有 `pointer / center` 注视输入、`idle / thinking / error` 状态选择和 `headShakeSignal` 错误手势。3rem 锚点、三点 Thinking、Error 归正与摇头、点击反馈均通过生产 DOM 与 CSS 结构回归。至此两个生产调用点都使用 V1 场景；旧 `Mascot` 只剩 Lab 的只读对照用途。

## 24. 删除旧路径

两个生产调用点迁移完成后，删除旧 `components/Mascot.tsx`、Lab 的旧版对照组件以及对应的全局 SVG 状态选择器和 `pudding-mascot-*` 动画。旧版专用的状态灯与阴影颜色变量同步删除；新版仍使用的机身、轮廓、面罩和表情颜色变量继续由全局主题提供。Lab 现在只渲染一个 V1 场景，不再保留旧实现、feature flag 或 fallback。
