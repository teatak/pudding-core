# Unicorn AI Mobile：App 与 Skill 架构设计

> 状态：产品与技术设计草案
> 平台：iOS-first，Android 持续验证并后续发布  
> 定位：Unicorn 面向酒店行业的可定制 AI Worker 手机端

## 1. 项目与参考目录

Mobile 独立项目：

```text
/Users/yanggang/workspace/gitlab.lumous.cn/unicorn/unicorn-ai-mobile
```

Pudding Desktop 参考实现：

```text
/Users/yanggang/workspace/github.com/teatak/pudding-core
```

重点参考：

```text
pudding-core/internal/app/                         # App 定义、加载、连接与包管理
pudding-core/internal/skill/                       # Skill 索引、读取和校验
pudding-core/internal/tool/app_load.go              # App 加载工具
pudding-core/internal/tool/app_save.go              # 大模型保存 App 包
pudding-core/internal/skill/embed/app-creator/       # App 创建规则
pudding-core/docs/apps.md                           # App、Endpoint 和 Connection 说明
pudding-core/docs/builtin-apps-design.md             # App 按需加载设计
```

Mobile 是独立项目，不向 `pudding-core` 增加移动端条件分支；只参考和迁移 App/Skill 的概念及纯 TypeScript 数据契约。

## 2. 核心结论

Unicorn AI Mobile 继续采用 Pudding 现有 App 模式：

```text
App    = 接口、认证方式、Connection 和工具能力
Skill  = 使用这些接口完成业务的流程说明
LLM    = 按 Skill 组合工具完成任务
Canvas = 展示结构化结果
```

不新增独立的 `app-skills/unicorn/` 存储模型，也不在 Runtime 中写死“报表、定价、订单”等业务模块。

App 包仍由 `app.yaml`、App Skills、图标和必要参考资料组成。大模型可以按当前 `app-creator` 逻辑创建或更新完整 App 包。

Unicorn 不再提供独立登录页、宿主插件或登录小组件。统一通过 App OAuth 建立 Connection；OAuth 只解决身份和授权，不承担业务流程。

经营报表分析、间夜定价建议、特定订单查询仅作为 App Skill 示例，不构成产品或框架边界。

菜单如何显示、从哪里产生以及是否关联 App/Skill，后续单独设计。

## 3. Pudding 现有 App 逻辑

### 3.1 App Definition

当前 `App Definition` 声明：

- ID、名称、版本、描述和图标
- REST、GraphQL 或 MCP endpoint
- 认证方式
- Connection 字段和注入规则
- App Skills 路径
- 工具能力摘要

安装 App 的常规接口工具由 endpoint 类型推导：

- REST → `builtin_rest_request`
- GraphQL → `builtin_graphql_request`
- GraphQL → introspect/search 工具
- MCP → 运行时发现远程工具

因此，App 负责“可以调用什么”，不负责“具体业务怎样做”。

### 3.2 App Skill

App Skill 负责描述：

- 哪类用户请求应该触发该流程
- 使用哪个 endpoint
- 请求路径、方法和参数
- 多个接口的调用顺序
- 数据校验和缺失值处理
- 写操作或高风险操作的确认要求
- 结果如何解释并输出到 Canvas

业务流程是 `SKILL.md` 中的模型指令，不是 Runtime 中的固定 Workflow 代码。

### 3.3 加载流程

```text
Available Apps 注入精简索引
  → 模型判断需要某个 App/Skill
  → builtin_app_load(app_id, skill_id?)
  → Runtime 返回完整 SKILL.md
  → session 记录 loadedAppIDs
  → 下一模型步骤获得 App 工具
  → 模型按 Skill 调用接口
```

`builtin_app_load` 同时加载 Skill 指令和 App 工具能力；App 是否加载是 session 状态，不是 UI 焦点。

### 3.4 模型创建 App

现有 `app-creator` 与 `builtin_app_save` 支持：

1. 模型读取 App 编写规范。
2. 创建或检查 `app.yaml`、App Skills 和资源文件。
3. 生成完整 App 包，而不是局部 patch。
4. 在隔离候选目录完成校验。
5. 校验成功后原子安装或替换。
6. 认证凭据始终保存在 Connection，不写进 App 包。

Mobile 应复用这套语义。

## 4. Mobile 总体架构

```text
Expo / React Native
│
├── App Shell
│   ├── OAuth 授权回调
│   ├── 酒店/租户上下文
│   ├── 会话
│   ├── Canvas
│   └── 设置
│
├── App Runtime
│   ├── App Package Loader
│   ├── App Definition Validator
│   ├── App Connection Store
│   ├── OAuth Authorization Code
│   ├── Endpoint Resolver
│   ├── REST / GraphQL Tools
│   └── App Load / Save
│
├── Skill Runtime
│   ├── App Skill Index
│   ├── Skill Read
│   └── Skill Validation
│
├── AgentEngine
│   ├── OpenAI Provider
│   ├── App/Skill Prompt Assembly
│   ├── Tool Loop
│   └── Streaming / Cancel
│
└── Local Runtime
    ├── SecureStore
    ├── JSON FileSystem
    ├── Attachments
    └── Share Sheet
```

Mobile 不使用 Go daemon、SQLite、loopback REST、SSE 或 Desktop runtime routing。以上能力均由 App 进程内 TypeScript Runtime 实现。

## 5. App 包结构

沿用现有 Pudding App 结构：

```text
apps/
└── <app-id>/
    ├── app.yaml
    ├── assets/
    │   └── icon.svg
    └── skills/
        ├── <skill-a>/
        │   ├── SKILL.md
        │   └── references/
        └── <skill-b>/
            └── SKILL.md
```

一个 App 可以包含多个 App Skills。App 是接口和认证边界，Skill 是业务流程边界。

App 的拆分依据应是接口、权限或服务边界，而不是菜单：

- 多个流程共享同一组 endpoint 和 Connection 时，可以放在同一个 App 的不同 Skills 中。
- 接口域、认证方式或权限差异很大时，可以拆成多个 Apps。
- Runtime 不应识别任何具体业务 App ID 或 Skill ID。

## 6. App Manifest

示例仅用于说明现有结构：

```yaml
id: unicorn-hotel
name: Unicorn Hotel
version: 0.1.0
description: Access authorized Unicorn hotel business APIs.

auth:
  required: true
  methods:
    - id: oauth
      type: oauth2
      label: 授权 Unicorn
      default: true

connection:
  fields:
    - id: hotelCode
      label: 酒店代码
      required: true
      inject:
        - target: header
          name: X-Hotel-Code

endpoints:
  unicorn_rest:
    kind: rest
    url: https://example.invalid/api

skills:
  - skills/operating-report-analysis/SKILL.md
  - skills/nightly-pricing/SKILL.md
  - skills/order-lookup/SKILL.md
```

示例中的三个 Skills 不是固定要求，URL 和认证字段也必须在真实 Unicorn API 明确后替换。

## 7. OAuth 与 Connection

Unicorn 统一使用 App OAuth，不实现独立登录页、宿主登录插件、登录小组件或账号密码 Token Exchange。

### 7.1 授权流程

采用传统 OAuth 2.0 Authorization Code：

1. 用户在 Unicorn App Connection 中点击“授权 Unicorn”。
2. Mobile 使用系统浏览器打开 Unicorn Authorization Endpoint。
3. 用户在 Unicorn 授权页完成身份验证和授权。
4. Unicorn 通过 Universal Link 或自定义 deep link 返回 Mobile。
5. Runtime 校验 `state`，并使用一次性 authorization code 换取 Token。
6. access token、refresh token 和过期时间写入 SecureStore。
7. Runtime 创建或更新 App Connection。
8. App endpoint 调用从 Connection 自动注入 Bearer Token。

### 7.2 App Manifest OAuth 配置

App 包只声明 OAuth 元数据，不保存 client secret 或用户 Token：

```yaml
auth:
  required: true
  methods:
    - id: unicorn-oauth
      type: oauth2
      label: 授权 Unicorn
      default: true
      provider: unicorn
```

Authorization Endpoint、Token Endpoint、Client ID、redirect URI 和 scopes 由产品环境或受信配置提供。移动端不内置 client secret；如果 Unicorn Token Endpoint 强制要求 client secret，应由 Unicorn 受信服务完成 code 换 Token。

### 7.3 Token 生命周期

- Token 仅进入 SecureStore 和私有 Connection Store。
- access token 到期前按需刷新。
- 刷新失败时将 Connection 标记为需要重新授权，不静默循环重试。
- 用户撤销连接时清除 access token、refresh token 和相关 Connection 状态。
- App Skill、模型和日志均看不到 Token 或认证 Header。
- 当前酒店必须显式进入请求上下文或 Connection 字段。

### 7.4 安全校验

- 必须校验 OAuth `state`；使用 OIDC 时同时校验 `nonce`。
- authorization code 必须短期有效、只能使用一次。
- redirect URI 必须精确匹配受信配置。
- 授权回调只接受预期 scheme/domain 和当前未完成的授权事务。
- scopes 采用最小权限，并在新增 scope 时重新请求用户授权。
- OAuth 错误、取消和超时必须回到可恢复的 Connection 状态。

## 8. App Skills 与业务流程

业务 Skill 仍放在所属 App 包内，不使用独立 `app-skills/` 目录。

示例：

```text
apps/unicorn-hotel/
├── app.yaml
└── skills/
    ├── operating-report-analysis/
    │   ├── SKILL.md
    │   └── references/
    ├── nightly-pricing/
    │   └── SKILL.md
    └── order-lookup/
        └── SKILL.md
```

以上名称只是业务流程示例。App 可以包含任意数量和类型的 Skills。

Skill 示例：

```markdown
---
name: operating-report-analysis
description: 分析酒店经营数据、趋势和异常，并生成经营建议。
---

# 经营报表分析

## Procedure

1. 确认酒店和分析日期范围。
2. 使用 endpoint `unicorn_rest` 读取经营报表。
3. 需要对比时读取上期或去年同期数据。
4. 校验数据口径和缺失字段，不自行补造事实。
5. 识别趋势和异常。
6. 使用 Canvas 输出指标卡、图表、对比表和建议。
```

Skill 可以定义：

- 触发场景
- 接口调用顺序
- 请求参数和结果校验
- 业务判断规则
- 用户追问和确认节点
- 异常与缺失数据处理
- Canvas 展示要求

Skill 不应包含：

- Token、密码或模型 Key
- 未在 App manifest 中声明的 endpoint
- 绕过用户权限的方法
- JavaScript、shell 或任意动态执行代码

## 9. 大模型定制 App 与 Skill

沿用现有完整 App 包 authoring 模式：

```text
用户描述业务需求
  → 模型读取 app-creator 规范
  → 模型检查现有 App 包和接口资料
  → 创建或修改 App Skill
  → 生成完整候选 App 包
  → 校验 manifest、Skill、路径和权限变化
  → 用户确认
  → 原子创建或替换 App
```

建议保留的工具语义：

```text
builtin_app_load
builtin_app_save
builtin_app_unload
builtin_app_delete
builtin_rest_request
builtin_graphql_request
```

规则：

- `create` 不覆盖已有 App ID。
- `update` 必须基于完整现有包，未修改文件也要保留。
- endpoint、认证、Connection 字段或权限变化必须单独展示。
- 仅修改 Skill 时，不得意外改动 `app.yaml`。
- 包在隐藏候选目录完成校验后再原子替换。
- 凭据和私有 Connection 文件不允许被模型读取或写入。
- App 更新保留版本与审计记录，并支持回退。

如果官方 App 需要保持不可修改，可让用户“复制并定制”为新的 installed App；不要在 Runtime 中为官方业务流程写特殊分支。

## 10. Canvas

Canvas 是独立的 UI Runtime 能力，不属于某个 Unicorn App，也不承担业务流程。

App Skill 描述期望的展示方式，模型调用 Canvas 工具生成：

- Markdown
- 指标卡
- 表格
- 图表
- 图册
- 时间线
- Grid

移动端使用卡片流、单项全屏、横向表格和系统分享，不保留 Desktop 自由窗口。

## 11. 会话与本地数据

```ts
type Session = {
  id: string;
  hotelID?: string;
  loadedAppIDs: string[];
  messages: CanonicalMessage[];
  canvasItems: CanvasItem[];
};
```

规则：

- App 是否加载属于 session 状态，不是菜单或页面焦点。
- endpoint 调用显式携带 `sessionID`，需要酒店上下文时显式携带 `hotelID`。
- Skill 内容通过 `app_load` 工具结果进入 canonical context。
- delta 只存在于内存，turn 完成后再保存 assistant message。
- session、App 包和非敏感配置使用版本化 JSON。
- OAuth Token 和 Provider Key 使用 SecureStore。
- 不使用 SQLite。

## 12. Provider

v1 由用户配置模型：

- OpenAI Responses API
- OpenAI-compatible Chat Completions API
- 暂不单独实现 Gemini 和 Claude 协议
- API Key 存 SecureStore
- 模型、Base URL 和非敏感配置存版本化 JSON

模型既可以执行 App Skill，也可以在用户授权后创建或改进 App 包。

## 13. 安全边界

- App endpoint 只能访问声明的 HTTP/HTTPS base URL。
- Runtime 禁止重定向到其他域，并限制请求和响应大小。
- 实际数据权限最终由 Unicorn 服务端校验。
- Token、认证 Header 和 Provider Key 不进入模型上下文。
- PII 在发送第三方模型前最小化或脱敏。
- 写操作和高风险请求必须确认。
- 日志不记录完整订单、Token、Key 或完整 Prompt。
- App 包不允许二进制、脚本执行、任意 JavaScript 或 shell。
- App 更新可审计并可回退。

## 14. 相对 Pudding Desktop 的改造

保留：

- `app.yaml` App Definition
- App-owned Skills
- Available Apps 精简索引
- `app_load(app_id, skill_id)` 语义
- session-scoped `loadedAppIDs`
- REST/GraphQL 通用接口工具
- App Package 隔离校验和原子保存
- 大模型通过 `app-creator` 创建或更新 App

改造：

- Go daemon 改为进程内 TypeScript Runtime。
- SQLite `loadedAppIDs` 改为 session JSON。
- Desktop 文件目录改为 Mobile App 沙箱。
- Connection 凭据改用 SecureStore。
- 移除 stdio MCP、Code mode 和后台进程。
- Canvas 改成移动卡片流。
- Unicorn Connection 统一改为传统 OAuth Authorization Code。

## 15. 菜单暂不设计

本设计不定义：

- 菜单内容由谁产生
- 菜单是否展示 App 或 Skill
- 菜单是否固定或可配置
- 菜单、业务入口、AI Worker、App 和 Skill 的关系

App Runtime 只提供与菜单无关的 `list/load/enable/disable` 能力。菜单方案后续单独讨论。

## 16. 平台、签名与分发

采用 **iOS-first 开发、双端持续验证**：

- 日常使用 iOS Simulator 和 iPhone Development Build。
- 第一周建立 Android Development Build。
- OAuth、SecureStore、流式请求、文件、Canvas 和分享持续进行双端验证。
- 平台差异通过 `.ios.tsx`、`.android.tsx` 或 adapter 隔离。

团队已有 Apple Developer 账号。Pudding Desktop 与 Unicorn AI Mobile 可以使用同一 Apple Team，但证书不同：

| 产品 | 证书 |
| --- | --- |
| Pudding Desktop DMG/ZIP | `Developer ID Application` |
| Unicorn AI Mobile TestFlight/App Store | `Apple Distribution` + Provisioning Profile |

建议分发顺序：

1. iOS Simulator
2. iPhone Development Build
3. TestFlight Internal
4. TestFlight External
5. App Store
6. Android APK/Internal Build 持续验证
7. Google Play

## 17. v1 不做

- 把具体酒店业务写死进 Runtime
- 独立 `app-skills/` overlay 体系
- 固定 Workflow DSL
- Skill 自定义未声明 endpoint
- 任意 JavaScript、TypeScript、shell 或二进制执行
- stdio MCP、Work/Code、Git、LSP 和终端
- Desktop 自由 Canvas 窗口
- 菜单绑定方案
- 云端会话同步
- 平台统一模型计费

## 18. 实施顺序

1. 创建独立 Expo/React Native 项目和双端 Development Build。
2. 迁移 TypeScript AgentEngine、session JSON 和 Provider streaming。
3. 实现 App Definition、Package Loader、Validator 和 Store。
4. 实现 Connection Store、REST/GraphQL 工具和 `app_load`。
5. 实现 App-owned Skill 索引、读取和 prompt 装配。
6. 实现 Canvas 移动端 renderer。
7. 实现 `app-creator`、完整包保存、审批和回退。
8. 实现 Unicorn OAuth、deep link 回调、Token 刷新与撤销。
9. 使用若干示例 App Skills 验证业务表达能力。
10. 完成 iOS 隐私、TestFlight 和 App Store 发布。
11. 持续完成 Android 适配并发布 Google Play。

## 19. 开工前需要确认

- Unicorn Authorization Endpoint、Token Endpoint、Client ID、redirect URI 和 scopes。
- Token Endpoint 是否要求 client secret；若要求，由哪一侧受信服务完成换取。
- iOS Universal Link / URL Scheme 与 Android App Link / intent filter 配置。
- refresh token、撤销授权和重新授权协议。
- Unicorn REST/GraphQL endpoint 和接口文档。
- 酒店、租户和用户权限字段。
- App 是否以一个 Unicorn 综合 App 起步，还是按接口/权限域拆分。
- 哪些角色可以创建、修改、启停和删除 App。
- PII 和写操作的确认规则。
- OpenAI Responses 与 Chat Completions 的具体兼容范围。

## 20. 最终边界

```text
OAuth 决定“如何获得身份与授权”
App Connection 决定“以什么身份调用”
App 决定“有哪些接口可以调用”
Skill 决定“如何组合接口完成业务”
LLM 负责“理解需求并执行 Skill”
Canvas 负责“如何展示结果”
```

具体酒店业务通过 App Skills 持续增加和被大模型定制；App Runtime、AgentEngine 和 Canvas 不随具体业务流程变化。
