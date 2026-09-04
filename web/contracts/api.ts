// REST payload 的 web 侧镜像。Go 侧来源:internal/store/store.go(实体)
// 与 internal/api/server.go(请求/响应);字段名一一对应。
import { z } from "zod";

export const session = z.object({
  id: z.string(),
  title: z.string(),
  provider: z.string(), // provider profile 名;session 创建时必须显式写入
  model: z.string(),
  reasoningEffort: z.string().optional(),
  reasoningModelKey: z.string().optional(),
  activeMode: z.enum(["chat", "work", "code"]),
  modeLease: z.enum(["none", "session"]),
  projectID: z.string().optional(),
  loadedAppIDs: z.array(z.string()).optional(),
  pinned: z.boolean(),
  pinnedOrder: z.number(),
  createdAt: z.string(), // RFC3339
  updatedAt: z.string(),
  lastActivityAt: z.string(),
  archivedAt: z.string().optional(),
  running: z.boolean(), // 读取时从 turns 派生,rail 运行态指示
  backgroundProcessCount: z.number().int().nonnegative(),
});
export type Session = z.infer<typeof session>;

export const approveApprovalResponse = z.object({
  status: z.literal("approved"),
  session,
});
export type ApproveApprovalResponse = z.infer<typeof approveApprovalResponse>;

export const approvalMode = z.enum(["ask", "auto", "full"]);
export type ApprovalMode = z.infer<typeof approvalMode>;

export const project = z.object({
  id: z.string(),
  name: z.string(),
  rootDirs: z.array(z.string()),
  approvalMode,
  createdAt: z.string(),
  updatedAt: z.string(),
  lastActivityAt: z.string().optional(),
});
export type Project = z.infer<typeof project>;

export const listProjectsResponse = z.object({
  projects: z.array(project),
});

export const createProjectRequest = z.object({
  name: z.string().optional(),
  rootDirs: z.array(z.string()),
  approvalMode: approvalMode.optional(),
});

export const patchProjectRequest = z.object({
  name: z.string().optional(),
  rootDirs: z.array(z.string()).optional(),
  approvalMode: approvalMode.optional(),
});

export const mergeProjectRequest = z.object({
  sourceProjectID: z.string().min(1),
  name: z.string().min(1),
  rootDirs: z.array(z.string()),
});

export const projectBrowserRoot = z.object({
  id: z.string(),
  name: z.string(),
  path: z.string(),
  temporary: z.boolean().optional(),
});
export type ProjectBrowserRoot = z.infer<typeof projectBrowserRoot>;

export const projectBrowserRootsResponse = z.object({
  projectID: z.string(),
  roots: z.array(projectBrowserRoot),
  temporary: z.boolean().optional(),
});

export const projectTreeEntry = z.object({
  name: z.string(),
  path: z.string(),
  type: z.enum(["dir", "file", "symlink", "other"]),
  size: z.number().int().nonnegative().optional(),
  mtime: z.string().optional(),
});
export type ProjectTreeEntry = z.infer<typeof projectTreeEntry>;

export const projectTreeResponse = z.object({
  rootID: z.string(),
  path: z.string(),
  entries: z.array(projectTreeEntry),
  truncated: z.boolean(),
  totalCount: z.number().int().nonnegative(),
});
export type ProjectTreeResponse = z.infer<typeof projectTreeResponse>;

export const projectSearchMatch = z.object({
  rootID: z.string(),
  path: z.string(),
  line: z.number().int().positive(),
  lineStart: z.number().int().positive(),
  lineEnd: z.number().int().positive(),
  text: z.string(),
  excerpt: z.string(),
  truncated: z.boolean(),
});
export type ProjectSearchMatch = z.infer<typeof projectSearchMatch>;

export const projectSearchResponse = z.object({
  query: z.string(),
  matches: z.array(projectSearchMatch),
  matchCount: z.number().int().nonnegative(),
  filesScanned: z.number().int().nonnegative(),
  resultsCapped: z.boolean(),
  caseSensitive: z.boolean(),
});
export type ProjectSearchResponse = z.infer<typeof projectSearchResponse>;

export const projectFile = z.object({
  rootID: z.string(),
  path: z.string(),
  name: z.string(),
  content: z.string(),
  mime: z.string(),
  size: z.number().int().nonnegative(),
  mtime: z.string(),
  revision: z.string().min(1),
});
export type ProjectFile = z.infer<typeof projectFile>;

export const projectGitStatusFile = z.object({
  path: z.string(),
  originalPath: z.string().optional(),
  kind: z.enum(["modified", "added", "deleted", "renamed", "copied", "type_changed", "untracked", "conflicted", "changed"]),
  indexStatus: z.string().length(1),
  worktreeStatus: z.string().length(1),
});
export type ProjectGitStatusFile = z.infer<typeof projectGitStatusFile>;

export const projectGitStatus = z.object({
  rootID: z.string(),
  available: z.boolean(),
  head: z.string().optional(),
  branch: z.string().optional(),
  upstream: z.string().optional(),
  detached: z.boolean(),
  ahead: z.number().int().nonnegative(),
  behind: z.number().int().nonnegative(),
  clean: z.boolean(),
  files: z.array(projectGitStatusFile),
  fileCount: z.number().int().nonnegative(),
  stagedCount: z.number().int().nonnegative(),
  unstagedCount: z.number().int().nonnegative(),
  untrackedCount: z.number().int().nonnegative(),
  conflictedCount: z.number().int().nonnegative(),
});
export type ProjectGitStatus = z.infer<typeof projectGitStatus>;

export const projectGitBranch = z.object({
  name: z.string(),
  upstream: z.string().optional(),
  current: z.boolean(),
  remote: z.boolean(),
});
export type ProjectGitBranch = z.infer<typeof projectGitBranch>;

export const projectGitBranches = z.object({
  rootID: z.string(),
  branches: z.array(projectGitBranch),
});
export type ProjectGitBranches = z.infer<typeof projectGitBranches>;

export const projectGitDiff = z.object({
  rootID: z.string(),
  path: z.string(),
  originalPath: z.string().optional(),
  staged: z.boolean(),
  oldContent: z.string(),
  newContent: z.string(),
  binary: z.boolean(),
  tooLarge: z.boolean(),
});
export type ProjectGitDiff = z.infer<typeof projectGitDiff>;

export const projectGitRootRequest = z.object({
  rootID: z.string().min(1),
});

export const projectGitPathsRequest = projectGitRootRequest.extend({
  paths: z.array(z.string().min(1)).min(1).max(512),
});

export const projectGitCommitRequest = projectGitRootRequest.extend({
  message: z.string().trim().min(1).max(16 * 1024),
  stageAll: z.boolean().default(false),
});

export const projectGitBranchRequest = projectGitRootRequest.extend({
  name: z.string().trim().min(1).max(1024),
});

export const projectEntryMutation = z.object({
  rootID: z.string(),
  name: z.string(),
  path: z.string(),
  type: z.enum(["dir", "file"]),
});
export type ProjectEntryMutation = z.infer<typeof projectEntryMutation>;

export const createProjectEntryRequest = z.object({
  rootID: z.string().min(1),
  parentPath: z.string().min(1),
  name: z.string().min(1),
  type: z.enum(["dir", "file"]),
});

export const renameProjectEntryRequest = z.object({
  rootID: z.string().min(1),
  path: z.string().min(1),
  name: z.string().min(1),
});

export const transferProjectEntryRequest = z.object({
  sourceRootID: z.string().min(1),
  sourcePath: z.string().min(1),
  targetRootID: z.string().min(1),
  targetParentPath: z.string().min(1),
  name: z.string().min(1).optional(),
  unique: z.boolean().optional(),
});

export const saveProjectFileRequest = z.object({
  rootID: z.string().min(1),
  path: z.string().min(1),
  content: z.string(),
  expectedRevision: z.string().min(1),
});

export const canvasItem = z.object({
  id: z.string(),
  sessionID: z.string(),
  canvasID: z.string(),
  sourceSessionID: z.string().optional(),
  createdBySessionID: z.string().optional(),
  updatedBySessionID: z.string().optional(),
  kind: z.string(),
  title: z.string().optional(),
  item: z.unknown(),
  window: z.unknown().optional(),
  sourceSavedItemID: z.string().optional(),
  baseSavedRevision: z.number().int().optional(),
  savedDirty: z.boolean().optional(),
  visible: z.boolean(),
  createdAt: z.string(),
  updatedAt: z.string(),
});
export type CanvasItem = z.infer<typeof canvasItem>;

export const listCanvasItemsResponse = z.object({
  items: z.array(canvasItem),
});

export const closedCanvasItem = z.object({
  id: z.string(),
  sessionID: z.string(),
  sourceItemID: z.string(),
  actorSessionID: z.string().optional(),
  kind: z.string(),
  title: z.string().optional(),
  item: z.unknown(),
  window: z.unknown().optional(),
  closedAt: z.string(),
  createdAt: z.string(),
  updatedAt: z.string(),
});
export type ClosedCanvasItem = z.infer<typeof closedCanvasItem>;

export const listClosedCanvasItemsResponse = z.object({
  items: z.array(closedCanvasItem),
});

export const savedCanvasItem = z.object({
  id: z.string(),
  sourceSessionID: z.string().optional(),
  sourceItemID: z.string().optional(),
  kind: z.string(),
  title: z.string().optional(),
  item: z.unknown(),
  window: z.unknown().optional(),
  revision: z.number().int(),
  createdAt: z.string(),
  updatedAt: z.string(),
});
export type SavedCanvasItem = z.infer<typeof savedCanvasItem>;

export const listSavedCanvasItemsResponse = z.object({
  items: z.array(savedCanvasItem),
});

export const canvasSaveResult = z.object({
  item: canvasItem,
  savedItem: savedCanvasItem,
});
export type CanvasSaveResult = z.infer<typeof canvasSaveResult>;

export const putCanvasItemRequest = z.object({
  id: z.string().optional(),
  sourceSessionID: z.string().optional(),
  kind: z.string().min(1),
  title: z.string().optional(),
  item: z.unknown(),
  window: z.unknown().optional(),
});

export const patchCanvasItemRequest = z.object({
  window: z.unknown(),
});

export const putClosedCanvasItemRequest = z.object({
  id: z.string().optional(),
  sourceItemID: z.string().min(1),
  kind: z.string().min(1),
  title: z.string().optional(),
  item: z.unknown(),
  window: z.unknown().optional(),
  closedAt: z.string().optional(),
});

export const browserMCPTool = z.object({
  name: z.string(),
  description: z.string().optional(),
  capability: z.enum(["chat", "work", "code"]).optional(),
  appID: z.string().optional(),
});
export type BrowserMCPTool = z.infer<typeof browserMCPTool>;

export const browserMCPSession = z.object({
  id: z.string(),
  runtimeID: z.string(),
  runtime: z.string(),
  connectedAt: z.string(),
  serverName: z.string().optional(),
  serverVersion: z.string().optional(),
  tools: z.array(browserMCPTool),
});
export type BrowserMCPSession = z.infer<typeof browserMCPSession>;

export const listBrowserMCPSessionsResponse = z.object({
  sessions: z.array(browserMCPSession),
});

export const browserState = z.object({
  hasState: z.boolean(),
  sessionID: z.string(),
  tabID: z.string().optional(),
  url: z.string().optional(),
  title: z.string().optional(),
  faviconURL: z.string().optional(),
  mode: z.enum(["headless", "webview", "external"]).optional(),
  processMode: z.enum(["headless", "webview", "external"]).optional(),
  recoverable: z.boolean().optional(),
  createdAt: z.string().optional(),
  updatedAt: z.string().optional(),
});
export type BrowserState = z.infer<typeof browserState>;

export const browserHistoryEntry = z.object({
  id: z.string(),
  url: z.string(),
  title: z.string().optional(),
  faviconURL: z.string().optional(),
  visitedAt: z.string(),
  createdAt: z.string(),
  updatedAt: z.string(),
});
export type BrowserHistoryEntry = z.infer<typeof browserHistoryEntry>;

export const listBrowserHistoryResponse = z.object({
  history: z.array(browserHistoryEntry),
});

// provider profile 的设置视图:apiKey 来自本地配置,编辑时可回显;apiKeySet 用于列表状态。
export const providerProtocol = z.enum(["openai-compatible", "openai-responses", "google", "anthropic"]);

export const providerModelLimits = z.object({
  maxOutputTokens: z.number().optional(),
  maxToolLoops: z.number().optional(),
});

export const providerModelOptions = z.object({
  openai: z.record(z.string(), z.unknown()).optional(),
  google: z.record(z.string(), z.unknown()).optional(),
  anthropic: z.record(z.string(), z.unknown()).optional(),
});

export const providerModel = z.object({
  id: z.string().trim().min(1),
  displayName: z.string().optional(),
  contextWindow: z.number().optional(),
  capabilities: z
    .object({
      image: z.boolean().optional(),
      audio: z.boolean().optional(),
      tools: z.boolean().optional(),
    })
    .optional(),
  limits: providerModelLimits.optional(),
  providerOptions: providerModelOptions.optional(),
});
export type ProviderModel = z.infer<typeof providerModel>;

export const providerProfile = z.object({
  id: z.string(),
  displayName: z.string(),
  brand: z.string().optional(),
  group: z.string().optional(),
  protocol: providerProtocol,
  baseURL: z.string(),
  apiKey: z.string().optional(),
  apiKeySet: z.boolean(),
  // 配置的可选模型清单;选择器只显示这里的内容,没有默认模型语义。
  models: z.array(providerModel),
});
export type ProviderProfile = z.infer<typeof providerProfile>;

export const createProviderRequest = z.object({
  id: z.string().min(1),
  displayName: z.string().min(1),
  brand: z.string().optional(),
  group: z.string().optional(),
  protocol: providerProtocol,
  baseURL: z.string().optional(),
  apiKey: z.string().optional(),
  models: z.array(providerModel).optional(),
});

// apiKey 传非空才覆盖;清除走 DELETE 后重建
export const patchProviderRequest = z.object({
  displayName: z.string().optional(),
  brand: z.string().optional(),
  group: z.string().optional(),
  protocol: providerProtocol.optional(),
  baseURL: z.string().optional(),
  apiKey: z.string().optional(),
  models: z.array(providerModel).optional(),
});

export const listProvidersResponse = z.object({ providers: z.array(providerProfile) });

export const listModelsResponse = z.object({ models: z.array(z.string()) });

export const probeProviderModelsRequest = z.object({
  protocol: providerProtocol,
  baseURL: z.string().optional(),
  apiKey: z.string().optional(),
  brand: z.string().optional(),
});

export const attachment = z.object({
  id: z.string(),
  name: z.string(),
  attachmentKey: z.string(),
  url: z.string(),
  mime: z.string(),
  size: z.number(),
  origin: z.string().optional(),
  sourcePath: z.string().optional(),
  createdAt: z.string().optional(),
  audioTranscript: z.string().optional(),
});
export type Attachment = z.infer<typeof attachment>;

export const localFolder = z.object({
  id: z.string(),
  name: z.string(),
  path: z.string(),
  origin: z.string().optional(),
});
export type LocalFolder = z.infer<typeof localFolder>;

export const projectReference = z.object({
  id: z.string(),
  name: z.string(),
  path: z.string(),
  sourcePath: z.string(),
  rootID: z.string(),
  kind: z.enum(["file", "dir"]),
  startLine: z.number().int().positive().optional(),
  startColumn: z.number().int().positive().optional(),
  endLine: z.number().int().positive().optional(),
  endColumn: z.number().int().positive().optional(),
});
export type ProjectReference = z.infer<typeof projectReference>;

export const contentPart = z.discriminatedUnion("type", [
  z.object({ type: z.literal("text"), text: z.string() }),
  z.object({ type: z.literal("thought"), text: z.string() }),
  z.object({
    type: z.literal("ui_context"),
    surface: z.enum(["project", "canvas", "browser", "terminal", "file_preview"]),
    resource: z.enum(["project_file", "project_diff", "canvas_item", "browser_tab", "terminal", "file"]).optional(),
    id: z.string().optional(),
    name: z.string().optional(),
    path: z.string().optional(),
    url: z.string().optional(),
    kind: z.string().optional(),
    rootID: z.string().optional(),
    selectionText: z.string().max(16 * 1024).optional(),
  }),
  z.object({
    type: z.literal("tool_use"),
    id: z.string().optional(),
    name: z.string().optional(),
    args: z.unknown().optional(),
  }),
  z.object({
    type: z.literal("tool_result"),
    id: z.string().optional(),
    name: z.string().optional(),
    ok: z.boolean().optional(),
    content: z.string().optional(),
    summaryKind: z.string().optional(),
    summaryCount: z.number().optional(),
    attachments: z.array(attachment).optional(),
  }),
  z.object({
    type: z.literal("form_result"),
    title: z.string(),
    schema: z.record(z.string(), z.unknown()),
    result: z.record(z.string(), z.unknown()),
  }),
  attachment.extend({ type: z.literal("attachment") }),
  localFolder.extend({ type: z.literal("local_folder") }),
  projectReference.extend({ type: z.literal("project_reference") }),
]);
export type ContentPart = z.infer<typeof contentPart>;

export const message = z
  .object({
    id: z.string(),
    sessionID: z.string(),
    turnID: z.string(),
    role: z.enum(["user", "assistant", "tool", "system", "summary"]),
    kind: z.enum(["text", "thought", "tool_use", "tool_result", "summary"]),
    text: z.string(),
    parts: z.array(contentPart),
    turnIndex: z.number().int(),
    metadata: z.unknown().optional(),
    clientMessageID: z.string().optional(), // 仅 user message,overlay 对账键
    interrupted: z.boolean().optional(),
    createdAt: z.string(),
  })
  .superRefine((value, ctx) => {
    if (value.text.trim() && value.parts.length === 0) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "message.parts is required when message.text is present",
        path: ["parts"],
      });
    }
  });
export type Message = z.infer<typeof message>;

export const turnFileChange = z.object({
  id: z.string(),
  sessionID: z.string(),
  turnID: z.string(),
  rootPath: z.string(),
  path: z.string(),
  originalPath: z.string().optional(),
  kind: z.enum(["added", "modified", "deleted", "renamed"]),
  // command_observed marks explicit targets observed around a foreground command.
  origin: z.enum(["structured", "command_observed"]).default("structured"),
  additions: z.number().int().nonnegative(),
  deletions: z.number().int().nonnegative(),
  binary: z.boolean(),
  tooLarge: z.boolean(),
  oldSize: z.number().int().nonnegative(),
  newSize: z.number().int().nonnegative(),
  oldContent: z.string().optional(),
  newContent: z.string().optional(),
  snapshotVersion: z.number().int().nonnegative().default(0),
  reversible: z.boolean().default(false),
  createdAt: z.string(),
});
export type TurnFileChange = z.infer<typeof turnFileChange>;

export const searchSessionMessagesRequest = z.object({
  sessionIDs: z.array(z.string().min(1)).min(1).max(200),
  query: z.string().trim().min(1),
  limit: z.number().int().min(1).max(100).optional(),
});

export const searchSessionMessagesResponse = z.object({
  messages: z.array(message),
  matchTerms: z.array(z.string()),
});

export const searchMessagesInSessionRequest = z.object({
  query: z.string().trim().min(1),
});

export const searchMessagesInSessionResponse = searchSessionMessagesResponse;

export const conversationTurn = z.object({
  id: z.string(),
  sessionID: z.string(),
  clientMessageID: z.string(),
  status: z.enum(["running", "completed", "failed", "cancelled"]),
  provider: z.string().optional(),
  model: z.string().optional(),
  mode: z.enum(["chat", "work", "code"]).optional(),
  error: z.string().optional(),
  createdAt: z.string(),
  updatedAt: z.string(),
  messages: z.array(message),
  fileChanges: z.array(turnFileChange).optional(),
  fileChangeState: z.enum(["applied", "undone"]).optional(),
});
export type ConversationTurn = z.infer<typeof conversationTurn>;

export const turnFileChangeActionResponse = z.object({
  state: z.enum(["applied", "undone"]),
});
export type TurnFileChangeActionResponse = z.infer<typeof turnFileChangeActionResponse>;

export const queuedInputStatus = z.enum(["queued", "editing", "cancelled", "promoted"]);

export const queuedInput = z.object({
  sessionID: z.string(),
  clientMessageID: z.string(),
  text: z.string(),
  parts: z.array(contentPart).optional(),
  status: queuedInputStatus,
  provider: z.string().optional(),
  model: z.string().optional(),
  mode: z.enum(["chat", "work", "code"]).optional(),
  modelConfig: z.unknown().optional(),
  turnID: z.string().optional(),
  createdAt: z.string(),
  updatedAt: z.string(),
});
export type QueuedInput = z.infer<typeof queuedInput>;

export const submitRequest = z
  .object({
    clientMessageID: z.string().min(1),
    kind: z.enum(["user", "system"]).optional(),
    reasoningEffort: z.string().optional(),
    text: z.string().optional().default(""),
    parts: z.array(contentPart).optional(),
  })
  .superRefine((value, ctx) => {
    if (value.kind === "system") {
      if (!value.text.trim() || (value.parts?.length ?? 0) > 0) {
        ctx.addIssue({ code: z.ZodIssueCode.custom, message: "system submit requires text only" });
      }
      return;
    }
    if ((value.parts?.length ?? 0) === 0) {
      ctx.addIssue({ code: z.ZodIssueCode.custom, message: "submit requires parts" });
    }
  });

// 202 新 turn 或 queued input;200 + duplicate=true 幂等重放
export const submitResponse = z.object({
  duplicate: z.boolean().optional(),
  queued: z.boolean().optional(),
  turnID: z.string().optional(),
  userMessageID: z.string().optional(),
  status: queuedInputStatus.optional(),
  clientMessageID: z.string().optional(),
});

export const steerRequest = z.object({
  clientMessageID: z.string().min(1),
  text: z.string().optional().default(""),
  parts: z.array(contentPart).min(1),
});

export const steerResponse = z.object({
  duplicate: z.boolean().optional(),
  turnID: z.string(),
  userMessageID: z.string(),
});

export const steerQueuedInputRequest = z.object({
  turnID: z.string().min(1),
});

export const compactResponse = z.object({
  turnID: z.string(),
  summaryMessageID: z.string(),
  status: z.literal("completed"),
  sourceMessages: z.number(),
  tailMessages: z.number(),
  summaryChars: z.number(),
});

export const pendingApproval = z.object({
  id: z.string(),
  sessionID: z.string(),
  turnID: z.string(),
  callID: z.string().optional(),
  approvalKind: z.string(),
  targetMode: z.enum(["work", "code"]).optional(),
  title: z.string().optional(),
  reason: z.string().optional(),
  risk: z.string().optional(),
  payload: z.unknown().optional(),
  createdAt: z.string(),
});
export type PendingApproval = z.infer<typeof pendingApproval>;

export const listPendingApprovalsResponse = z.object({ approvals: z.array(pendingApproval) });

export const sessionUsage = z.object({
  sessionID: z.string(),
  contextWindow: z.number(),
  contextEstimatedTokens: z.number(),
  contextRawEstimatedTokens: z.number(),
  inputCalibrationFactor: z.number(),
  inputCalibrationSamples: z.number(),
  messageEstimatedTokens: z.number(),
  promptOverheadEstimatedTokens: z.number(),
  systemPromptEstimatedTokens: z.number(),
  toolsSchemaEstimatedTokens: z.number(),
  autoCompactThresholdTokens: z.number(),
  requestCount: z.number(),
  lastPromptTokens: z.number(),
  lastInputUncachedTokens: z.number(),
  lastInputCachedTokens: z.number(),
  lastCacheCreationTokens: z.number(),
  lastOutputContentTokens: z.number(),
  lastOutputReasoningTokens: z.number(),
  lastOutputTokens: z.number(),
  cumulativeInputUncachedTokens: z.number(),
  cumulativeInputCachedTokens: z.number(),
  cumulativeCacheCreationTokens: z.number(),
  cumulativeOutputContentTokens: z.number(),
  cumulativeOutputReasoningTokens: z.number(),
  cumulativeInputTokens: z.number(),
  cumulativeOutputTokens: z.number(),
  cumulativeTotalTokens: z.number(),
  updatedAt: z.string().optional(),
});
export type SessionUsage = z.infer<typeof sessionUsage>;

export const dailyUsageStat = z.object({
  date: z.string(),
  requestCount: z.number(),
  inputUncachedTokens: z.number(),
  inputCachedTokens: z.number(),
  cacheCreationTokens: z.number(),
  outputContentTokens: z.number(),
  outputReasoningTokens: z.number(),
  totalTokens: z.number(),
});
export type DailyUsageStat = z.infer<typeof dailyUsageStat>;
export const dailyUsageResponse = z.object({ days: z.array(dailyUsageStat) });

export const audioInputMode = z.enum(["transcribe", "raw"]);
export type AudioInputMode = z.infer<typeof audioInputMode>;
export const audioBindings = z.object({
  inputOwner: z.string(),
  inputMode: z.union([audioInputMode, z.literal("")]).default(""),
  inputLevel: z.number().default(0),
});
export type AudioBindings = z.infer<typeof audioBindings>;
export const audioBindingsResponse = z.object({ bindings: audioBindings });
export const audioBindingRequest = z.object({ enabled: z.boolean(), mode: audioInputMode.optional() });
export const audioBindingResponse = z.object({
  ok: z.boolean(),
  bindings: audioBindings,
});

export const audioDriverConfig = z.object({
  type: z.string(),
  captureSampleRate: z.number(),
  playbackSampleRate: z.number(),
  channels: z.number(),
  periodMillis: z.number(),
});
export const audioASRVADConfig = z.object({
  modelPath: z.string(),
  threshold: z.number(),
  minEnergy: z.number(),
  minSilenceMillis: z.number(),
  minSpeechMillis: z.number(),
  windowSize: z.number(),
  prerollMillis: z.number(),
});
export const audioASRConfig = z.object({
  enabled: z.boolean().optional(),
  saveAudio: z.boolean().optional(),
  engine: z.string(),
  modelPath: z.string(),
  tokensPath: z.string(),
  language: z.string(),
  useITN: z.boolean().optional(),
  numThreads: z.number(),
  provider: z.string(),
  vad: audioASRVADConfig,
});
export const audioAECConfig = z.object({
  enabled: z.boolean().optional(),
  model: z.string(),
});
export const audioNSConfig = z.object({
  enabled: z.boolean().optional(),
  model: z.string(),
  level: z.string(),
});
export const audioConfig = z.object({
  version: z.number(),
  driver: audioDriverConfig,
  asr: audioASRConfig,
  aec: audioAECConfig,
  ns: audioNSConfig,
});
export type AudioConfig = z.infer<typeof audioConfig>;
export const audioConfigResponse = z.object({
  path: z.string(),
  config: audioConfig,
});
export type AudioConfigResponse = z.infer<typeof audioConfigResponse>;
export const clearASRRecordingsResponse = z.object({
  ok: z.boolean(),
  attachments: z.number(),
  messages: z.number(),
  queuedInputs: z.number(),
  deleteErrors: z.number().optional(),
});
export type ClearASRRecordingsResponse = z.infer<typeof clearASRRecordingsResponse>;
export const audioRuntimeFile = z.object({
  label: z.string(),
  path: z.string(),
  kind: z.string(),
  exists: z.boolean(),
});
export const audioRuntimeStatus = z.object({
  ok: z.boolean(),
  installed: z.boolean(),
  disabled: z.boolean().optional(),
  running: z.boolean(),
  state: z.string(),
  release: z.string(),
  profile: z.string(),
  platformKey: z.string(),
  currentAsset: z.string().optional(),
  assetIndex: z.number().optional(),
  assetTotal: z.number().optional(),
  bytesDownloaded: z.number().optional(),
  bytesTotal: z.number().optional(),
  message: z.string().optional(),
  error: z.string().optional(),
  required: z.array(audioRuntimeFile),
  missing: z.array(audioRuntimeFile),
});
export type AudioRuntimeStatus = z.infer<typeof audioRuntimeStatus>;

export const desktopAboutRow = z.object({
  key: z.string(),
  value: z.string(),
});
export const desktopAboutSection = z.object({
  id: z.string(),
  title: z.string(),
  rows: z.array(desktopAboutRow),
});
export type DesktopAboutSection = z.infer<typeof desktopAboutSection>;
export const desktopAboutResponse = z.object({
  sections: z.array(desktopAboutSection),
});

export const browserTab = z.object({
  id: z.string(),
  sessionID: z.string(),
  targetID: z.string().optional(),
  url: z.string(),
  title: z.string(),
  faviconURL: z.string().optional(),
  mode: z.enum(["headless", "webview", "external"]).optional(),
  canGoBack: z.boolean().optional(),
  canGoForward: z.boolean().optional(),
  createdAt: z.string(),
  updatedAt: z.string(),
});
export type BrowserTab = z.infer<typeof browserTab>;

export const observedBrowserElement = z.object({
  index: z.number(),
  tag: z.string(),
  text: z.string().optional(),
  href: z.string().optional(),
  role: z.string().optional(),
  ariaLabel: z.string().optional(),
  selector: z.string().optional(),
  inputType: z.string().optional(),
  disabled: z.boolean().optional(),
});

export const browserObservation = z.object({
  tab: browserTab,
  title: z.string(),
  url: z.string(),
  readyState: z.string(),
  text: z.string(),
  textChars: z.number(),
  truncated: z.boolean(),
  elements: z.array(observedBrowserElement),
});
export type BrowserObservation = z.infer<typeof browserObservation>;

export const browserScreenshot = z.object({
  tab: browserTab,
  mime: z.string(),
  dataBase64: z.string(),
  size: z.number(),
  width: z.number().optional(),
  height: z.number().optional(),
  viewportWidth: z.number().optional(),
  viewportHeight: z.number().optional(),
  deviceScaleFactor: z.number().optional(),
  capturedAt: z.string(),
});
export type BrowserScreenshot = z.infer<typeof browserScreenshot>;

export const browserActionResult = z.object({
  tab: browserTab,
  action: z.string(),
  result: z.record(z.string(), z.unknown()).nullable(),
});
export type BrowserActionResult = z.infer<typeof browserActionResult>;

export const listBrowserTabsResponse = z.object({
  tabs: z.array(browserTab),
  processMode: z.enum(["headless", "webview", "external"]).optional(),
});

export const backgroundProcess = z.object({
  processID: z.string(),
  turnID: z.string().optional(),
  callID: z.string().optional(),
  status: z.string(),
  running: z.boolean(),
  cwd: z.string(),
  command: z.string(),
  shell: z.string().optional(),
  exitCode: z.number().optional(),
  startedAt: z.string(),
  finishedAt: z.string().optional(),
  reason: z.string().optional(),
  error: z.string().optional(),
  execution: z.enum(["sandbox", "host"]),
  sandboxKind: z.string().optional(),
  sandboxDenied: z.boolean().optional(),
  tty: z.boolean().optional(),
});
export type BackgroundProcess = z.infer<typeof backgroundProcess>;

export const listBackgroundProcessesResponse = z.object({
  processes: z.array(backgroundProcess),
});

export const backgroundProcessOutputChunk = z.object({
  offset: z.number().int().nonnegative(),
  stream: z.enum(["stdout", "stderr"]),
  content: z.string(),
});

export const backgroundProcessLog = z.object({
  process: backgroundProcess,
  output: z.array(backgroundProcessOutputChunk),
  oldestOffset: z.number().int().nonnegative(),
  nextOffset: z.number().int().nonnegative(),
  tailOffset: z.number().int().nonnegative(),
  truncated: z.boolean(),
  hasMore: z.boolean(),
});
export type BackgroundProcessLog = z.infer<typeof backgroundProcessLog>;

export const browserOpenRequest = z.object({ url: z.string().min(1) });
export const browserSyncRequest = z.object({
  targetID: z.string().optional(),
  url: z.string(),
  title: z.string().optional(),
  faviconURL: z.string().optional(),
  canGoBack: z.boolean().optional(),
  canGoForward: z.boolean().optional(),
  historyVisit: z.boolean().optional(),
});
export const browserObserveRequest = z.object({
  maxTextChars: z.number().optional(),
  maxElements: z.number().optional(),
});
export const browserScreenshotRequest = z.object({ fullPage: z.boolean().optional() });
export const browserClickRequest = z.object({
  selector: z.string().optional(),
  x: z.number().optional(),
  y: z.number().optional(),
  method: z.enum(["auto", "pointer"]).optional(),
});
export const browserTypeRequest = z.object({
  selector: z.string().optional(),
  text: z.string().min(1),
  clear: z.boolean().optional(),
});
export const browserScrollRequest = z.object({
  selector: z.string().optional(),
  deltaX: z.number().optional(),
  deltaY: z.number().optional(),
});

export const listSessionsResponse = z.object({ sessions: z.array(session) });
export const listMessagesResponse = z.object({ messages: z.array(message), hasMore: z.boolean() });
export const listTurnsResponse = z.object({ turns: z.array(conversationTurn), hasMore: z.boolean() });
export const listQueuedInputsResponse = z.object({ queuedInputs: z.array(queuedInput) });
export const patchQueuedInputRequest = z.object({
  text: z.string().min(1).optional(),
  status: z.enum(["queued", "editing", "cancelled"]).optional(),
});
export const settingsResponse = z.object({ settings: z.record(z.string(), z.string()) });
export const userPromptResponse = z.object({
  path: z.string(),
  content: z.string(),
  exists: z.boolean(),
});

export const builtinTool = z.object({
  id: z.string(),
  description: z.string(),
  capability: z.enum(["chat", "work", "code"]),
  inputSchema: z.unknown().optional(),
});
export type BuiltinTool = z.infer<typeof builtinTool>;

export const listBuiltinToolsResponse = z.object({ tools: z.array(builtinTool) });

export const skill = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string().optional(),
  scope: z.literal("global"),
  source: z.enum(["builtin", "user"]),
  system: z.boolean(),
  path: z.string().optional(),
  iconPath: z.string().optional(),
});
export type Skill = z.infer<typeof skill>;

export const listSkillsResponse = z.object({ skills: z.array(skill) });

export const webToolProvider = z.object({
  name: z.string(),
  apiKey: z.string().optional(),
  apiKeySet: z.boolean(),
});
export type WebToolProvider = z.infer<typeof webToolProvider>;

export const webToolsConfig = z.object({
  searchProvider: z.string().optional(),
  fetchProvider: z.string().optional(),
  providers: z.array(webToolProvider),
});
export type WebToolsConfig = z.infer<typeof webToolsConfig>;

export const appEndpointPlatformOverride = z.object({
  url: z.string().optional(),
  command: z.string().optional(),
  args: z.array(z.string()).optional(),
  env: z.record(z.string(), z.string()).optional(),
  headers: z.record(z.string(), z.string()).optional(),
});

export const appEndpointURLConfig = z.object({
  label: z.string(),
  description: z.string().optional(),
  placeholder: z.string().optional(),
  required: z.boolean().optional(),
});

export const appEndpoint = z.object({
  kind: z.enum(["rest", "graphql", "mcp"]),
  transport: z.enum(["stdio", "streamable_http"]).optional(),
  url: z.string().optional(),
  urlConfig: appEndpointURLConfig.optional(),
  command: z.string().optional(),
  args: z.array(z.string()).optional(),
  env: z.record(z.string(), z.string()).optional(),
  headers: z.record(z.string(), z.string()).optional(),
  platforms: z.record(z.string(), appEndpointPlatformOverride).optional(),
  description: z.string().optional(),
});
export type AppEndpoint = z.infer<typeof appEndpoint>;

export const appMCPOverride = z.object({
  transport: z.enum(["stdio", "streamable_http"]).optional(),
  url: z.string().optional(),
  command: z.string().optional(),
  args: z.array(z.string()).optional(),
  env: z.record(z.string(), z.string()).optional(),
  headers: z.record(z.string(), z.string()).optional(),
});
export type AppMCPOverride = z.infer<typeof appMCPOverride>;

export const appMCPOverrideResponse = z.object({
  configured: z.boolean(),
  override: appMCPOverride,
});
export type AppMCPOverrideResponse = z.infer<typeof appMCPOverrideResponse>;

export const appSkillRef = z.object({
  id: z.string().optional(),
  name: z.string().optional(),
  description: z.string().optional(),
  path: z.string(),
});
export type AppSkillRef = z.infer<typeof appSkillRef>;

export const appSkillDetail = appSkillRef.extend({
  content: z.string(),
});
export type AppSkillDetail = z.infer<typeof appSkillDetail>;

export const appToolRef = z.object({
  name: z.string(),
  description: z.string().optional(),
});
export type AppToolRef = z.infer<typeof appToolRef>;

export const appIconThemeColor = z.object({
  light: z.string().optional(),
  dark: z.string().optional(),
});
export type AppIconThemeColor = z.infer<typeof appIconThemeColor>;

export const appIconSpec = z.object({
  svg: z.string().optional(),
  color: appIconThemeColor.optional(),
  background: appIconThemeColor.optional(),
});
export type AppIconSpec = z.infer<typeof appIconSpec>;

export const appAuthMethod = z.object({
  id: z.string().optional(),
  type: z.string(),
  provider: z.string().optional(),
  label: z.string().optional(),
  default: z.boolean().optional(),
  prefix: z.string().optional(),
  header: z.string().optional(),
  tokenExchange: z
    .object({
      url: z.string(),
      bodyFields: z.record(z.string(), z.string()),
      accessTokenField: z.string(),
      expiresInField: z.string().optional(),
      tokenType: z.string().optional(),
    })
    .optional(),
});
export const appAuthConfig = z.object({
  required: z.boolean().optional(),
  methods: z.array(appAuthMethod).optional(),
});

export const appConnectionFieldInject = z.object({
  target: z.string(),
  name: z.string().optional(),
  methods: z.array(z.string()).optional(),
});

export const appConnectionField = z.object({
  id: z.string(),
  label: z.string().optional(),
  description: z.string().optional(),
  placeholder: z.string().optional(),
  required: z.boolean().optional(),
  secret: z.boolean().optional(),
  inject: z.array(appConnectionFieldInject).optional(),
});
export const appConnectionConfig = z.object({
  fields: z.array(appConnectionField).optional(),
});

export const appDefinition = z.object({
  kind: z.enum(["app", "mcp"]),
  id: z.string(),
  name: z.string(),
  version: z.string().optional(),
  description: z.string().optional(),
  icon: appIconSpec.optional(),
  auth: appAuthConfig.optional(),
  connection: appConnectionConfig.optional(),
  endpoints: z.record(z.string(), appEndpoint).optional(),
  skills: z.array(appSkillRef).optional(),
  tools: z.array(appToolRef).optional(),
  path: z.string().optional(),
  sourceURL: z.string().optional(),
  packageSHA256: z.string().optional(),
  source: z.enum(["builtin", "installed"]),
  runtime: z.string().optional(),
  enabled: z.boolean(),
  canUninstall: z.boolean(),
  requiredMode: z.enum(["chat", "work", "code"]),
  defaultSkillID: z.string().optional(),
});
export type AppDefinition = z.infer<typeof appDefinition>;

export const installAppRequest = z.object({
  packageJSON: z.string().min(1),
  packageSHA256: z.string().optional(),
  sourceURL: z.string().optional(),
});

export const appConnection = z.object({
  id: z.string(),
  name: z.string().optional(),
  appID: z.string(),
  authType: z.string().optional(),
  authMethodID: z.string().optional(),
  authVariant: z.string().optional(),
  tokenSet: z.boolean(),
  account: z.object({
    id: z.string(),
    login: z.string(),
    name: z.string().optional(),
    avatarURL: z.string().optional(),
    type: z.string().optional(),
  }).optional(),
  reauthorizationRequired: z.boolean().optional(),
  header: z.string().optional(),
  fields: z.record(z.string(), z.string()).optional(),
  endpointURLs: z.record(z.string(), z.string()).optional(),
  prefix: z.string().optional(),
  token: z.string().optional(),
  username: z.string().optional(),
  password: z.string().optional(),
  createdAt: z.string().optional(),
  updatedAt: z.string().optional(),
});
export type AppConnection = z.infer<typeof appConnection>;

export const appMCPTool = z.object({
  name: z.string(),
  providerName: z.string().optional(),
  title: z.string().optional(),
  description: z.string().optional(),
  inputSchema: z.unknown().optional(),
});
export type AppMCPTool = z.infer<typeof appMCPTool>;

export const appMCPEndpointStatus = z.object({
  appID: z.string(),
  endpointName: z.string(),
  connectionID: z.string().optional(),
  transport: z.string().optional(),
  configured: z.boolean().optional(),
  status: z.string(),
  error: z.string().optional(),
  tools: z.array(appMCPTool).optional(),
});
export type AppMCPEndpointStatus = z.infer<typeof appMCPEndpointStatus>;

export const appMCPStatusResponse = z.object({
  appID: z.string(),
  endpoints: z.array(appMCPEndpointStatus),
});
export type AppMCPStatusResponse = z.infer<typeof appMCPStatusResponse>;

export const listAppsResponse = z.object({ apps: z.array(appDefinition) });
export const appMCPConfigRequest = z.object({
  configJSON: z.string().min(1),
  name: z.string().optional(),
});
export const appMCPConfigResponse = z.object({ configJSON: z.string() });
export const importMCPAppsResponse = z.object({ apps: z.array(appDefinition) });
export const listAppConnectionsResponse = z.object({ connections: z.array(appConnection) });

export const startAppOAuthRequest = z.object({
  appID: z.string().min(1),
  authMethodID: z.string().optional(),
  connectionID: z.string().optional(),
  connectionName: z.string().optional(),
  fields: z.record(z.string(), z.string()).optional(),
  endpointURLs: z.record(z.string(), z.string()).optional(),
});
export const startAppOAuthResponse = z.object({
  authorizationURL: z.string().url(),
});

export const completeAppOAuthRequest = z.object({
  provider: z.string().min(1),
  ticket: z.string().optional(),
  state: z.string().min(1),
  error: z.string().optional(),
});

export const patchWebToolsRequest = z.object({
  searchProvider: z.string().optional(),
  fetchProvider: z.string().optional(),
  providers: z
    .record(
      z.string(),
      z.object({
        apiKey: z.string().optional(),
      }),
    )
    .optional(),
});

// 409 响应体:submit → turn_running;cancel → no_running_turn;
// POST /providers 重名 → profile_exists
export const conflictResponse = z.object({
  error: z.enum(["turn_running", "no_running_turn", "profile_exists"]),
});
