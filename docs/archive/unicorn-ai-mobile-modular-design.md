# Unicorn AI Mobile：App 接口与 Skill 业务设计

> 状态:外部产品概念稿,已归档。本文不属于 Pudding Electron Desktop 当前主线。  
> 平台：iOS-first，Android 持续验证并后续发布  
> 定位：Unicorn 面向酒店行业的可定制 AI Worker 手机端

## 1. 核心结论

Unicorn AI Mobile 沿用当前 Pudding 的 App/Skill 分工：

```text
App   = 提供可调用接口
Skill = 定义业务流程
LLM   = 按 Skill 组合接口完成任务
Canvas = 展示结构化结果
```

Unicorn 登录成功后自动建立内置 Connector。Connector 对 Runtime 表现为一个可加载的 Unicorn App，负责提供已认证的业务接口；具体酒店业务不写进 App Runtime，而由可创建、可修改的 Skills 描述。

经营报表分析、间夜定价建议、特定订单查询只是 Skill 示例，不是写死的功能模块。未来可以由用户、酒店或大模型继续创建其他业务 Skill。

本设计暂不讨论主菜单如何展示，也不假设菜单与 App 或 Skill 的绑定关系。

## 2. 现有 Pudding App 逻辑核对

当前代码中的实际职责如下。

### 2.1 App Definition

`internal/app/types.go` 中的 App Definition 主要声明：

- ID、名称、版本和图标
- 认证方式
- Connection 字段
- REST、GraphQL 或 MCP endpoint
- App Skills 引用
- 可用工具摘要

安装 App 的 REST/GraphQL 工具不是逐个业务工具写死，而是根据 endpoint 类型自动推导：

- REST endpoint → `builtin_rest_request`
- GraphQL endpoint → `builtin_graphql_request`
- GraphQL endpoint → schema introspect/search 工具
- MCP endpoint → 运行时发现远程工具

因此，App 的主要作用确实是提供连接、认证和调用接口。

### 2.2 App Skill

App Skill 负责告诉模型：

- 什么请求应该使用这个 App
- 使用哪个 endpoint
- 调用哪些路径或 GraphQL query
- 参数如何组合
- 多个调用的先后顺序
- 写操作前需要做什么检查
- 返回结果如何解释和展示

这部分是自然语言流程说明，不是 Runtime 中写死的 Workflow 代码。

### 2.3 加载流程

当前流程是：

```text
Available Apps 只注入精简索引
  → 模型判断需要某个 App
  → builtin_app_load(app_id, skill_id?)
  → 返回完整 App Skill 内容
  → session 记录 loadedAppIDs
  → 下一模型步骤获得该 App 的接口工具
  → 模型按 Skill 调用接口
```

`builtin_app_load` 同时完成两件事：

1. 把指定 Skill 的完整说明返回给模型。
2. 将 App 标记为当前 session 已加载，使接口工具可用。

### 2.4 大模型定制

现有 `app-creator` 和 `builtin_app_save` 已支持模型生成完整 App 包，并在隔离目录验证后原子替换。App 包中可以同时包含 endpoint 定义和 App Skills。

但 Unicorn Mobile 有一个不同点：Unicorn Connector 是产品内置且由登录建立，不应该允许模型修改；可定制的主要对象应该是业务 Skill。

## 3. Unicorn Mobile 目标模型

```text
Unicorn 登录
  → 自动建立 Connector Context
  → Unicorn App 提供业务接口
  → 模型按需加载某个业务 Skill
  → Skill 指导模型调用 Unicorn App
  → 模型生成分析和 Canvas
```

职责边界：

| 层 | 负责 | 不负责 |
| --- | --- | --- |
| Unicorn Connector | 登录、Token、租户、酒店、权限 | 业务流程 |
| Unicorn App | API endpoint、调用工具、schema/能力索引 | 报表分析或定价步骤 |
| Skill | 业务目标、步骤、参数、校验、结果解释 | 认证和网络实现 |
| AgentEngine | Skill 加载、工具调用、流式输出、取消 | 写死酒店业务 |
| Canvas | 表格、图表、卡片等结果展示 | 决定业务流程 |

## 4. 总体架构

```text
Expo / React Native
│
├── App Shell
│   ├── Unicorn 登录
│   ├── 酒店/租户选择
│   ├── 会话
│   └── 设置
│
├── Connector Runtime
│   ├── 登录态与 Token 刷新
│   ├── 当前租户、酒店和权限
│   └── 已认证 HTTP Transport
│
├── App Runtime
│   ├── Unicorn App Definition
│   ├── Endpoint / Capability Catalog
│   ├── App Load
│   └── Generic API Tools
│
├── Skill Runtime
│   ├── Skill Index
│   ├── Skill Read
│   ├── Skill Validate
│   ├── Skill Save
│   └── App-scoped Skill Store
│
├── AgentEngine
│   ├── OpenAI Provider
│   ├── App/Skill 上下文装配
│   ├── Tool Loop
│   └── Streaming / Cancel
│
└── Canvas Runtime
    ├── Markdown
    ├── 指标卡
    ├── 表格
    ├── 图表
    ├── 时间线
    └── Grid
```

## 5. Unicorn Connector

Connector 由登录自动建立，不要求用户再次安装、连接或填写 Token。

```ts
type ConnectorContext = {
  tenantID: string;
  hotelID: string;
  userID: string;
  permissions: string[];
  accessTokenRef: string;
};
```

规则：

- Token 和刷新凭据只保存在 SecureStore。
- App、Skill 和模型都不能读取原始 Token。
- Connector 自动向业务请求注入认证和酒店上下文。
- 当前 `hotelID` 必须显式进入请求上下文，不能依赖隐藏 focus。
- 切换酒店后重新计算权限。
- 退出登录时清除 Connector Context 和本地凭据。

## 6. Unicorn App

Unicorn App 是接口提供者，不是业务模块。

概念定义：

```yaml
id: unicorn
name: Unicorn
description: 访问当前登录用户有权限使用的 Unicorn 酒店业务接口。

endpoints:
  unicorn_rest:
    kind: rest
    url: <由产品环境提供，不允许 Skill 修改>
```

与普通安装 App 不同：

- Unicorn App 随产品内置，不允许卸载。
- 登录成功后自动连接，不展示 Connection 配置表单。
- endpoint 地址由产品环境控制。
- 认证由 Connector Runtime 注入。
- Skill 不能新增 endpoint 或修改服务地址。
- 大模型不能更新 Unicorn App Definition。

## 7. 接口能力

### 7.1 复用当前方式

最接近当前 App 的实现是提供通用 REST 工具：

```text
builtin_rest_request(
  endpoint = "unicorn_rest",
  method,
  path,
  query?,
  body_json?
)
```

业务 Skill 描述具体路径、参数和调用顺序。

### 7.2 Mobile 需要增加的约束

Unicorn 数据包含经营信息和订单隐私，不能只依赖 Skill 文本约束。接口层还应维护可调用能力目录：

```ts
type Capability = {
  id: string;
  method: "GET" | "POST";
  pathPattern: string;
  inputSchema: JSONSchema;
  outputSchema: JSONSchema;
  requiredPermissions: string[];
  sensitivity: "normal" | "business" | "pii" | "write";
};
```

能力目录属于 Unicorn App 的接口描述，不属于业务流程。它负责：

- 限制允许调用的路径和方法
- 校验输入输出
- 检查当前用户权限
- 标记 PII 和写操作
- 向 Skill 作者提供可靠的接口参考

## 8. 业务 Skill

业务 Skill 是可定制业务能力的核心。

示例目录：

```text
app-skills/
└── unicorn/
    ├── operating-report-analysis/
    │   ├── SKILL.md
    │   └── references/
    ├── nightly-pricing/
    │   └── SKILL.md
    └── order-lookup/
        └── SKILL.md
```

以上三个目录只用于举例；Runtime 不识别或写死这些 ID。

### 8.1 Skill 内容

```markdown
---
name: operating-report-analysis
description: 分析酒店经营数据、趋势和异常，并生成经营建议。
app: unicorn
---

# 经营报表分析

## Procedure

1. 确认酒店和分析日期范围。
2. 使用 Unicorn 报表接口读取经营指标。
3. 需要对比时读取上期或去年同期数据。
4. 校验报表口径和缺失字段，不自行补造数据。
5. 计算变化率并识别异常。
6. 使用 Canvas 输出指标卡、趋势图、对比表和建议。

## Boundaries

- 所有指标事实以 Unicorn 接口结果为准。
- 数据不足时明确说明，不生成虚假结论。
```

### 8.2 Skill 可以定义

- 适用场景和触发语句
- 所需 Unicorn 接口
- 信息收集顺序
- 多接口调用步骤
- 参数和结果校验
- 计算和判断规则
- 失败与缺失数据处理
- 是否需要用户确认
- Canvas 展示要求

### 8.3 Skill 不可以定义

- 登录和 Token
- 任意服务地址
- Catalog 之外的接口
- 绕过用户权限
- 任意 JavaScript、shell 或动态代码
- 自动扩大数据访问范围

## 9. App Skill 与 Global Skill

当前 Pudding 已区分：

- Global Skill：通用流程，不绑定 App。
- App Skill：描述如何使用特定 App endpoint。

Unicorn 的酒店业务流程明显属于 App Skill，因为它依赖 Unicorn 接口。

但当前实现把 App Skill 和 App manifest 放在同一个 App 包中，更新时需要替换整个安装包；内置 App 又不允许模型更新。Mobile 因此需要一个小扩展：

```text
内置 Unicorn App：只读
Unicorn App Skills：独立可写、可校验、可版本化
```

App Runtime 在列出 Unicorn Definition 时，合并：

1. 随产品发布的官方 Skills。
2. 酒店或管理员下发的 Skills。
3. 用户或模型创建的本地 Skills。

同 ID 冲突时不静默覆盖，必须使用版本或显式 fork。

## 10. Skill 加载流程

建议保持现有 `builtin_app_load` 语义：

```text
模型看到 Unicorn App 与可用 Skill 元数据
  → 识别与请求匹配的 Skill
  → builtin_app_load(app_id="unicorn", skill_id="...")
  → Runtime 返回完整 SKILL.md
  → 当前 session 标记 Unicorn App 已加载
  → 下一模型步骤获得 Unicorn API 工具
  → 模型按 Skill 执行业务流程
```

只将 Skill 的名称和描述放入索引，不把所有完整 Skill 常驻系统提示词。

一个 session 可以多次读取不同 Unicorn Skills。`loadedAppIDs` 只决定接口是否可用，Skill 内容决定当前任务如何使用接口。

## 11. 大模型创建和修改 Skill

大模型不创建 Unicorn App，而是创建或修改 Unicorn App Skill。

建议工具：

```text
app_skill_list
app_skill_read
app_skill_validate
app_skill_save
app_skill_delete
unicorn_capability_search
```

标准流程：

```text
用户描述业务需求
  → 模型搜索 Unicorn Capability Catalog
  → 读取相关接口 schema 和已有 Skill
  → 编写或修改 SKILL.md
  → 校验 Skill、接口引用和权限
  → 向用户展示摘要及新增数据权限
  → 用户确认
  → 原子保存
  → Skill 进入可用索引
```

保存规则：

- 创建操作不能覆盖已有 Skill。
- 更新操作必须基于明确的 Skill ID 和版本。
- 候选目录全部校验通过后再原子替换。
- 新增 PII 或写能力时必须重新确认。
- 模型只能读取 Skill 文件和接口文档，不能读取 Token、Key 或凭据。
- 保留创建者、更新时间、版本和权限差异。

## 12. 官方、酒店与个人 Skill

| 类型 | 来源 | 修改方式 |
| --- | --- | --- |
| 官方 Skill | 随 Unicorn Mobile 发布 | 官方升级；用户修改时 fork |
| 酒店 Skill | 酒店或集团管理员提供 | 管理员更新 |
| 个人 Skill | 用户或模型创建 | 用户可修改和删除 |

所有类型使用同一种 Skill 规范。官方示例不能成为 Runtime 的特殊分支。

## 13. Canvas

Canvas 是独立的 Runtime-provided App 或内置 UI 工具，不属于 Unicorn App，也不承载业务流程。

Skill 只描述希望如何展示结果，模型通过 Canvas 工具生成：

- Markdown
- 指标卡
- 表格
- 图表
- 图册
- 时间线
- Grid

移动端使用卡片流、单项全屏、横向表格和系统分享，不保留自由窗口。

## 14. 会话状态

```ts
type Session = {
  id: string;
  hotelID: string;
  loadedAppIDs: string[];
  messages: CanonicalMessage[];
  canvasItems: CanvasItem[];
};
```

规则：

- 所有接口调用显式携带 `sessionID` 和 `hotelID`。
- App 是否加载是 session 状态，不是菜单或页面焦点。
- Skill 内容通过工具结果进入 canonical context。
- delta 只存在于内存，turn 完成后再写 canonical message。
- App、Skill 和菜单展示相互独立。

## 15. 模型接入

v1 由用户配置模型：

- OpenAI Responses API
- OpenAI-compatible Chat Completions API
- 暂不单独实现 Gemini 和 Claude 协议
- API Key 存 SecureStore
- 模型、Base URL 和非敏感配置存版本化 JSON

模型承担两类任务：

1. 读取业务 Skill，调用 Unicorn 接口完成酒店业务。
2. 使用 Skill authoring 工具创建和改进业务 Skill。

## 16. 安全边界

- Unicorn App 和 Connector 由产品控制，模型不能修改。
- Skill 只能使用 Capability Catalog 中的接口。
- 实际权限取“当前用户权限 ∩ 接口要求”。
- Token、模型 Key 和认证 Header不进入 Skill 或模型上下文。
- PII 返回前脱敏，发送第三方模型前再次最小化。
- 写操作必须由接口目录标记，并在调用前确认。
- 日志不记录完整订单、Token、Key 或完整 Prompt。
- Skill 更新可审计并可回退。

## 17. Mobile 相对当前实现的改造

保留：

- App Definition
- App Skill
- Available Apps 精简索引
- `app_load(app_id, skill_id)` 语义
- loaded App 的 session 状态
- App 提供工具、Skill 提供流程
- Skill 按需读取
- 模型创建/修改 Skill

改造：

- Go daemon 实现改为进程内 TypeScript Runtime。
- Unicorn 登录自动建立 Connector，不再手工配置 App Connection。
- SQLite `loadedAppIDs` 改为 session JSON。
- 禁用 Mobile MCP、stdio 和任意远程 App endpoint。
- 增加内置 App 的可写 Skill overlay。
- 增加 Unicorn Capability Catalog 和权限校验。
- Skill 保存改为 Mobile 文件系统中的原子替换。

## 18. v1 不做

- 把经营报表、定价、订单查询写死进 Runtime
- 在 App 包中定义业务页面和固定 Workflow DSL
- 让大模型修改 Unicorn App、Connector 或认证
- Skill 自定义 endpoint
- 动态执行 JavaScript、TypeScript、shell 或二进制
- MCP、stdio、Work/Code、Git、LSP 和终端
- 自由 Canvas 窗口
- 菜单与 App/Skill 的绑定设计
- 云端会话同步
- 平台统一模型计费

## 19. 平台、签名与分发策略

### 19.1 开发策略

采用 **iOS-first 开发、双端持续验证**：

- 日常主要使用 Xcode iOS Simulator 和 iPhone Development Build 调试。
- UI、AgentEngine、App/Skill、Canvas 和数据层保持 React Native/TypeScript 双端共享。
- 第一周即建立 Android Development Build，不等 iOS 完成后再移植。
- 登录、SecureStore、流式请求、文件选择、Canvas 和分享每完成一项，都在 Android 真机验证。
- 平台差异通过 `.ios.tsx`、`.android.tsx` 或平台 adapter 隔离，禁止在业务 Skill 中出现平台分支。

iOS-first 的原因：

- 当前开发环境是 Mac，iOS Simulator 调试链路更直接。
- 团队已有 Apple Developer 账号，可以进行真机签名、TestFlight 和 App Store 发布。
- iOS 设备和系统组合相对集中，适合先稳定交互与核心 Runtime。

Android 仍需从项目早期持续验证，重点包括：

- 系统返回键和返回手势
- 键盘弹出与窗口 resize
- 文件 `content://` URI
- 相机、相册和文件权限
- SecureStore、深链接和系统分享
- 不同屏幕尺寸和厂商后台限制

### 19.2 Apple 证书

Unicorn AI Mobile 与 Pudding Desktop 可以使用同一个 Apple Developer 账号和 Team，但使用不同类型的签名证书：

| 产品 | 分发方式 | 证书 |
| --- | --- | --- |
| Pudding Desktop | macOS 站外 DMG/ZIP + notarization | `Developer ID Application` |
| Unicorn AI Mobile | iOS 真机、TestFlight、App Store | `Apple Distribution` + Provisioning Profile |

两者不是同一张证书。现有 Apple Developer 账号已经解决账号和 Team 资格问题；iOS 项目仍需单独创建 Bundle ID、App Store Connect App、Distribution 证书和 Provisioning Profile。证书可以交给 Xcode 或 EAS Credentials 管理。

### 19.3 分发顺序

1. iOS Simulator：日常开发。
2. iPhone Development Build：登录、Keychain、相机、文件和深链接真机测试。
3. TestFlight Internal Testing：团队内部体验。
4. TestFlight External Testing：酒店试点用户。
5. App Store：iOS 正式发布。
6. Android APK/Internal Build：开发期间持续测试和客户预览。
7. Google Play：Android 适配稳定后发布。

## 20. 实施顺序

1. Unicorn 登录、Connector Context 和酒店权限。
2. TypeScript App Runtime 与内置 Unicorn App。
3. REST/API 工具、Capability Catalog 和 schema 校验。
4. App-scoped Skill Store、索引、读取和加载。
5. AgentEngine、流式生成、取消和 session JSON。
6. Canvas Runtime。
7. App Skill 创建、校验、原子保存和回退。
8. 使用若干示例 Skill 验证业务表达能力。
9. iOS 权限、隐私、TestFlight 和 App Store 发布。
10. Android 平台加固与 Google Play 发布；Android 验证贯穿以上所有阶段。

## 21. 最终边界

```text
Connector 决定“以谁的身份访问”
App 决定“有哪些接口可以调用”
Skill 决定“如何组合接口完成业务”
LLM 负责“理解需求并执行 Skill”
Canvas 负责“如何展示结果”
```

具体酒店业务可以通过 Skill 持续增加和被大模型定制，而 Unicorn Connector、App Runtime 和 AgentEngine 不随业务流程变化。
