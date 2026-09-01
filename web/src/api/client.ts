import {
  listBuiltinToolsResponse,
  approveApprovalResponse,
  listBrowserMCPSessionsResponse,
  listAppConnectionsResponse,
  listCanvasItemsResponse,
  listClosedCanvasItemsResponse,
  listSavedCanvasItemsResponse,
  listAppsResponse,
  listSkillsResponse,
  compactResponse,
  conflictResponse,
  listModelsResponse,
  createProviderRequest,
  listMessagesResponse,
  listPendingApprovalsResponse,
  listProvidersResponse,
  listProjectsResponse,
  listQueuedInputsResponse,
  listSessionsResponse,
  searchMessagesInSessionRequest,
  searchMessagesInSessionResponse,
  searchSessionMessagesRequest,
  searchSessionMessagesResponse,
  listTurnsResponse,
  message,
  mergeProjectRequest,
  patchQueuedInputRequest,
  patchProviderRequest,
  patchProjectRequest,
  projectBrowserRootsResponse,
  projectEntryMutation,
  projectFile,
  projectGitDiff,
  projectGitBranches,
  projectGitBranchRequest,
  projectGitCommitRequest,
  projectGitPathsRequest,
  projectGitRootRequest,
  projectGitStatus,
  projectSearchResponse,
  projectTreeResponse,
  createProjectEntryRequest,
  renameProjectEntryRequest,
  transferProjectEntryRequest,
  saveProjectFileRequest,
  patchCanvasItemRequest,
  probeProviderModelsRequest,
  putCanvasItemRequest,
  putClosedCanvasItemRequest,
  patchWebToolsRequest,
  providerProfile,
  project,
  queuedInput,
  session,
  sessionUsage,
  settingsResponse,
  attachment,
  submitRequest,
  submitResponse,
  steerRequest,
  steerQueuedInputRequest,
  steerResponse,
  conversationTurn,
  turnFileChange,
  turnFileChangeActionResponse,
  dailyUsageResponse,
  desktopAboutResponse,
  userPromptResponse,
  appDefinition,
  appConnection,
  appMCPConfigRequest,
  appMCPConfigResponse,
  appMCPOverride,
  appMCPOverrideResponse,
  appMCPStatusResponse,
  canvasItem,
  canvasSaveResult,
  closedCanvasItem,
  appSkillDetail,
  audioBindingRequest,
  audioBindingResponse,
  audioBindingsResponse,
  audioConfig,
  audioConfigResponse,
  audioRuntimeStatus,
  clearASRRecordingsResponse,
  createProjectRequest,
  browserActionResult,
  browserClickRequest,
  browserObservation,
  browserObserveRequest,
  browserOpenRequest,
  browserScrollRequest,
  browserScreenshot,
  browserScreenshotRequest,
  listBrowserHistoryResponse,
  browserSyncRequest,
  browserTab,
  browserTypeRequest,
  installAppRequest,
  importMCPAppsResponse,
  listBrowserTabsResponse,
  listBackgroundProcessesResponse,
  backgroundProcessLog,
  startAppOAuthRequest,
  startAppOAuthResponse,
  completeAppOAuthRequest,
  webToolsConfig,
  type AppConnection,
  type ApproveApprovalResponse,
  type AppDefinition,
  type AppMCPEndpointStatus,
  type AppMCPOverride,
  type AppMCPOverrideResponse,
  type AppMCPStatusResponse,
  type AppMCPTool,
  type AppSkillDetail,
  type AudioBindings,
  type AudioInputMode,
  type AudioConfig,
  type AudioConfigResponse,
  type AudioRuntimeStatus,
  type ClearASRRecordingsResponse,
  type Attachment,
  type BuiltinTool,
  type BrowserActionResult,
  type BrowserObservation,
  type BrowserMCPSession,
  type BrowserScreenshot,
  type BrowserHistoryEntry,
  type BrowserTab,
  type BackgroundProcess,
  type BackgroundProcessLog,
  type CanvasItem,
  type CanvasSaveResult,
  type ClosedCanvasItem,
  type SavedCanvasItem,
  type ContentPart,
  type DailyUsageStat,
  type DesktopAboutSection,
  type LocalFolder,
  type Message,
  type PendingApproval,
  type ConversationTurn,
  type TurnFileChange,
  type TurnFileChangeActionResponse,
  type ProviderModel,
  type ProviderProfile,
  type Project,
  type ProjectBrowserRoot,
  type ProjectEntryMutation,
  type ProjectFile,
  type ProjectGitDiff,
  type ProjectGitBranch,
  type ProjectGitBranches,
  type ProjectGitStatus,
  type ProjectGitStatusFile,
  type ProjectSearchMatch,
  type ProjectSearchResponse,
  type ProjectReference,
  type ProjectTreeEntry,
  type ProjectTreeResponse,
  type QueuedInput,
  type Session,
  type SessionUsage,
  type Skill,
  type WebToolsConfig,
} from "@/contracts/api";
import { z } from "zod";

import { apiURL } from "@/state/apiBase";
import { runtimeRequestHeaders } from "@/state/runtime";

export class APIError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    public readonly detail?: string,
  ) {
    super(detail || code);
  }
}

const createSessionRequest = z.object({
  title: z.string().optional(),
  provider: z.string().min(1),
  model: z.string().min(1),
  projectID: z.string().optional(),
});

const cloneSessionRequest = z.object({
  throughMessageID: z.string().min(1),
  titleSuffix: z.string().min(1),
});

const sessionPatchRequest = z.object({
  title: z.string().optional(),
  provider: z.string().optional(),
  model: z.string().optional(),
  reasoningEffort: z.string().optional(),
  activeMode: z.enum(["chat", "work", "code"]).optional(),
  modeLease: z.enum(["none", "session"]).optional(),
  projectID: z.string().optional(),
  pinned: z.boolean().optional(),
  pinnedOrder: z.number().optional(),
});
const mobilePairingClaimResponse = z.object({
  token: z.string().min(1),
  device: z.object({
    id: z.string(),
    name: z.string(),
    createdAt: z.string(),
  }),
});
const mobilePairingClaims = new Map<
  string,
  Promise<z.infer<typeof mobilePairingClaimResponse>>
>();

export type SubmitResult = z.infer<typeof submitResponse>;
export type SubmitPayload = z.input<typeof submitRequest>;
export type CompactResult = z.infer<typeof compactResponse>;
export type UserPrompt = z.infer<typeof userPromptResponse>;
export type { AudioConfig, AudioConfigResponse, AudioInputMode, AudioRuntimeStatus, ClearASRRecordingsResponse };
export type AppConnectionPayload = {
  appID: string;
  name?: string;
  authMethodID?: string;
  authType: "none" | "bearer" | "token" | "basic" | "header" | "oauth2" | "token_exchange";
  token?: string;
  prefix?: string;
  header?: string;
  username?: string;
  password?: string;
  fields?: Record<string, string>;
  endpointURLs?: Record<string, string>;
};
export type { AppMCPOverride, AppMCPOverrideResponse };
export type {
  ProjectBrowserRoot,
  ProjectEntryMutation,
  ProjectFile,
  ProjectGitBranch,
  ProjectGitBranches,
  ProjectGitDiff,
  ProjectGitStatus,
  ProjectGitStatusFile,
  ProjectSearchMatch,
  ProjectSearchResponse,
  ProjectTreeEntry,
  ProjectTreeResponse,
};
export type CanvasItemPayload = z.infer<typeof putCanvasItemRequest>;
export type CanvasItemWindowPayload = z.infer<typeof patchCanvasItemRequest>;
export type ClosedCanvasItemPayload = z.infer<typeof putClosedCanvasItemRequest>;

function authHeaders(token: string) {
  return {
    Authorization: `Bearer ${token}`,
    ...runtimeRequestHeaders(),
  };
}

async function readJSON(response: Response) {
  if (response.status === 204) {
    return null;
  }
  return response.json();
}

async function request<T>(
  token: string,
  path: string,
  schema: z.ZodType<T>,
  init?: RequestInit,
): Promise<T> {
  const response = await fetch(apiURL(path), {
    ...init,
    headers: {
      ...authHeaders(token),
      "Content-Type": "application/json",
      ...init?.headers,
    },
  });
  const payload = await readJSON(response);
  if (!response.ok) {
    const parsedConflict = conflictResponse.safeParse(payload);
    const code = parsedConflict.success
      ? parsedConflict.data.error
      : typeof payload?.error === "string"
        ? payload.error
        : response.statusText;
    const detail = typeof payload?.detail === "string" ? payload.detail : undefined;
    throw new APIError(response.status, code, detail);
  }
  return schema.parse(payload);
}

async function publicRequest<T>(
  path: string,
  schema: z.ZodType<T>,
  init?: RequestInit,
): Promise<T> {
  const response = await fetch(apiURL(path), {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...init?.headers,
    },
  });
  const payload = await readJSON(response);
  if (!response.ok) {
    const code = typeof payload?.error === "string" ? payload.error : response.statusText;
    const detail = typeof payload?.detail === "string" ? payload.detail : undefined;
    throw new APIError(response.status, code, detail);
  }
  return schema.parse(payload);
}

export function claimMobilePairing(
  code: string,
  body: { deviceName?: string } = {},
): Promise<z.infer<typeof mobilePairingClaimResponse>> {
  const existing = mobilePairingClaims.get(code);
  if (existing) {
    return existing;
  }
  const claim = publicRequest(
    `/mobile/pairings/${encodeURIComponent(code)}/claim`,
    mobilePairingClaimResponse,
    {
      method: "POST",
      body: JSON.stringify(body),
    },
  );
  mobilePairingClaims.set(code, claim);
  return claim;
}

export function listSessions(token: string): Promise<{ sessions: Session[] }> {
  return request(token, "/sessions", listSessionsResponse);
}

export function listArchivedSessions(token: string, query = ""): Promise<{ sessions: Session[] }> {
  const params = new URLSearchParams({ scope: "archived" });
  if (query.trim()) {
    params.set("query", query.trim());
  }
  return request(token, `/sessions?${params.toString()}`, listSessionsResponse);
}

export function searchSessionMessages(
  token: string,
  body: z.infer<typeof searchSessionMessagesRequest>,
): Promise<{ messages: Message[]; matchTerms: string[] }> {
  return request(token, "/sessions/search", searchSessionMessagesResponse, {
    method: "POST",
    body: JSON.stringify(searchSessionMessagesRequest.parse(body)),
  });
}

export function searchMessagesInSession(
  token: string,
  sessionID: string,
  body: z.infer<typeof searchMessagesInSessionRequest>,
): Promise<{ messages: Message[]; matchTerms: string[] }> {
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/messages/search`,
    searchMessagesInSessionResponse,
    {
      method: "POST",
      body: JSON.stringify(searchMessagesInSessionRequest.parse(body)),
    },
  );
}

export function listProjects(token: string): Promise<{ projects: Project[] }> {
  return request(token, "/projects", listProjectsResponse);
}

export function getProject(token: string, projectID: string): Promise<Project> {
  return request(token, `/projects/${encodeURIComponent(projectID)}`, project);
}

export function listProjectBrowserRoots(token: string, sessionID: string): Promise<{ projectID: string; roots: ProjectBrowserRoot[]; temporary?: boolean }> {
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/project/tree`,
    projectBrowserRootsResponse,
  );
}

export function listProjectTree(
  token: string,
  sessionID: string,
  rootID: string,
  path: string,
): Promise<ProjectTreeResponse> {
  const query = new URLSearchParams({ rootID, path });
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/project/tree?${query.toString()}`,
    projectTreeResponse,
  );
}

export function searchProjectFiles(
  token: string,
  sessionID: string,
  query: string,
  signal?: AbortSignal,
): Promise<ProjectSearchResponse> {
  const params = new URLSearchParams({ q: query, limit: "200" });
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/project/search?${params.toString()}`,
    projectSearchResponse,
    { signal },
  );
}

export function getProjectFile(token: string, sessionID: string, rootID: string, path: string): Promise<ProjectFile> {
  const query = new URLSearchParams({ rootID, path });
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/project/file?${query.toString()}`,
    projectFile,
  );
}

export function getProjectGitStatus(token: string, sessionID: string, rootID: string): Promise<ProjectGitStatus> {
  const query = new URLSearchParams({ rootID });
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/project/git/status?${query.toString()}`,
    projectGitStatus,
  );
}

export function getProjectGitDiff(
  token: string,
  sessionID: string,
  rootID: string,
  path: string,
  staged: boolean,
): Promise<ProjectGitDiff> {
  const query = new URLSearchParams({ rootID, path, staged: String(staged) });
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/project/git/diff?${query.toString()}`,
    projectGitDiff,
  );
}

export function getProjectGitBranches(token: string, sessionID: string, rootID: string): Promise<ProjectGitBranches> {
  const query = new URLSearchParams({ rootID });
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/project/git/branches?${query.toString()}`,
    projectGitBranches,
  );
}

export function initializeProjectGit(token: string, sessionID: string, rootID: string): Promise<ProjectGitStatus> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/project/git/init`, projectGitStatus, {
    method: "POST",
    body: JSON.stringify(projectGitRootRequest.parse({ rootID })),
  });
}

export function stageProjectGit(token: string, sessionID: string, rootID: string, paths: string[]): Promise<ProjectGitStatus> {
  return mutateProjectGitPaths(token, sessionID, "stage", rootID, paths);
}

export function unstageProjectGit(token: string, sessionID: string, rootID: string, paths: string[]): Promise<ProjectGitStatus> {
  return mutateProjectGitPaths(token, sessionID, "unstage", rootID, paths);
}

export function discardProjectGit(token: string, sessionID: string, rootID: string, paths: string[]): Promise<ProjectGitStatus> {
  return mutateProjectGitPaths(token, sessionID, "discard", rootID, paths);
}

function mutateProjectGitPaths(
  token: string,
  sessionID: string,
  operation: "stage" | "unstage" | "discard",
  rootID: string,
  paths: string[],
): Promise<ProjectGitStatus> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/project/git/${operation}`, projectGitStatus, {
    method: "POST",
    body: JSON.stringify(projectGitPathsRequest.parse({ paths, rootID })),
  });
}

export function commitProjectGit(
  token: string,
  sessionID: string,
  rootID: string,
  message: string,
  stageAll = false,
): Promise<ProjectGitStatus> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/project/git/commit`, projectGitStatus, {
    method: "POST",
    body: JSON.stringify(projectGitCommitRequest.parse({ message, rootID, stageAll })),
  });
}

export function syncProjectGit(token: string, sessionID: string, rootID: string): Promise<ProjectGitStatus> {
  return mutateProjectGitRoot(token, sessionID, "sync", rootID);
}

export function publishProjectGit(token: string, sessionID: string, rootID: string): Promise<ProjectGitStatus> {
  return mutateProjectGitRoot(token, sessionID, "publish", rootID);
}

function mutateProjectGitRoot(
  token: string,
  sessionID: string,
  operation: "publish" | "sync",
  rootID: string,
): Promise<ProjectGitStatus> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/project/git/${operation}`, projectGitStatus, {
    method: "POST",
    body: JSON.stringify(projectGitRootRequest.parse({ rootID })),
  });
}

export function createProjectGitBranch(token: string, sessionID: string, rootID: string, name: string): Promise<ProjectGitStatus> {
  return mutateProjectGitBranch(token, sessionID, "", rootID, name);
}

export function switchProjectGitBranch(token: string, sessionID: string, rootID: string, name: string): Promise<ProjectGitStatus> {
  return mutateProjectGitBranch(token, sessionID, "switch", rootID, name);
}

export function renameProjectGitBranch(token: string, sessionID: string, rootID: string, name: string): Promise<ProjectGitStatus> {
  return mutateProjectGitBranch(token, sessionID, "rename", rootID, name);
}

export function deleteProjectGitBranch(token: string, sessionID: string, rootID: string, name: string): Promise<ProjectGitStatus> {
  return mutateProjectGitBranch(token, sessionID, "delete", rootID, name);
}

function mutateProjectGitBranch(
  token: string,
  sessionID: string,
  operation: "" | "delete" | "rename" | "switch",
  rootID: string,
  name: string,
): Promise<ProjectGitStatus> {
  const suffix = operation ? `/${operation}` : "";
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/project/git/branches${suffix}`, projectGitStatus, {
    method: "POST",
    body: JSON.stringify(projectGitBranchRequest.parse({ name, rootID })),
  });
}

export function createProjectEntry(
  token: string,
  sessionID: string,
  body: z.infer<typeof createProjectEntryRequest>,
): Promise<ProjectEntryMutation> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/project/entries`, projectEntryMutation, {
    method: "POST",
    body: JSON.stringify(createProjectEntryRequest.parse(body)),
  });
}

export function renameProjectEntry(
  token: string,
  sessionID: string,
  body: z.infer<typeof renameProjectEntryRequest>,
): Promise<ProjectEntryMutation> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/project/entries`, projectEntryMutation, {
    method: "PATCH",
    body: JSON.stringify(renameProjectEntryRequest.parse(body)),
  });
}

export function copyProjectEntry(
  token: string,
  sessionID: string,
  body: z.infer<typeof transferProjectEntryRequest>,
): Promise<ProjectEntryMutation> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/project/entries/copy`, projectEntryMutation, {
    method: "POST",
    body: JSON.stringify(transferProjectEntryRequest.parse(body)),
  });
}

export function moveProjectEntry(
  token: string,
  sessionID: string,
  body: z.infer<typeof transferProjectEntryRequest>,
): Promise<ProjectEntryMutation> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/project/entries/move`, projectEntryMutation, {
    method: "POST",
    body: JSON.stringify(transferProjectEntryRequest.parse(body)),
  });
}

export function saveProjectFile(
  token: string,
  sessionID: string,
  body: z.infer<typeof saveProjectFileRequest>,
): Promise<ProjectFile> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/project/file`, projectFile, {
    method: "PUT",
    body: JSON.stringify(saveProjectFileRequest.parse(body)),
  });
}

export async function deleteProjectEntry(token: string, sessionID: string, rootID: string, path: string): Promise<void> {
  const query = new URLSearchParams({ rootID, path });
  await request(token, `/sessions/${encodeURIComponent(sessionID)}/project/entries?${query.toString()}`, z.null(), {
    method: "DELETE",
  });
}

export function projectResourceURL(token: string, sessionID: string, rootID: string, path: string) {
  const encodedPath = path
    .split("/")
    .filter(Boolean)
    .map((part) => encodeURIComponent(part))
    .join("/");
  const href = apiURL(
    `/sessions/${encodeURIComponent(sessionID)}/project/resources/${encodeURIComponent(rootID)}/${encodedPath}`,
  );
  if (!token) {
    return href;
  }
  try {
    const url = new URL(href, window.location.href);
    url.searchParams.set("token", token);
    return url.toString();
  } catch {
    return `${href}?token=${encodeURIComponent(token)}`;
  }
}

export function createProject(
  token: string,
  body: z.infer<typeof createProjectRequest>,
): Promise<Project> {
  return request(token, "/projects", project, {
    method: "POST",
    body: JSON.stringify(createProjectRequest.parse(body)),
  });
}

export function updateProject(
  token: string,
  projectID: string,
  body: z.infer<typeof patchProjectRequest>,
): Promise<Project> {
  return request(token, `/projects/${encodeURIComponent(projectID)}`, project, {
    method: "PATCH",
    body: JSON.stringify(patchProjectRequest.parse(body)),
  });
}

export function mergeProjects(
  token: string,
  targetProjectID: string,
  body: z.infer<typeof mergeProjectRequest>,
): Promise<Project> {
  return request(token, `/projects/${encodeURIComponent(targetProjectID)}/merge`, project, {
    method: "POST",
    body: JSON.stringify(mergeProjectRequest.parse(body)),
  });
}

export async function deleteProject(token: string, projectID: string): Promise<void> {
  await request(token, `/projects/${encodeURIComponent(projectID)}`, z.null(), {
    method: "DELETE",
  });
}

export function createSession(
  token: string,
  body: z.infer<typeof createSessionRequest>,
): Promise<Session> {
  return request(token, "/sessions", session, {
    method: "POST",
    body: JSON.stringify(createSessionRequest.parse(body)),
  });
}

export function cloneSessionAtMessage(
  token: string,
  sessionID: string,
  throughMessageID: string,
  titleSuffix: string,
): Promise<Session> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/clone`, session, {
    method: "POST",
    body: JSON.stringify(cloneSessionRequest.parse({ throughMessageID, titleSuffix })),
  });
}

export function getSession(token: string, sessionID: string): Promise<Session> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}`, session);
}

export function updateSession(
  token: string,
  sessionID: string,
  body: z.infer<typeof sessionPatchRequest>,
): Promise<Session> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}`, session, {
    method: "PATCH",
    body: JSON.stringify(sessionPatchRequest.parse(body)),
  });
}

export async function deleteSession(token: string, sessionID: string): Promise<void> {
  await request(token, `/sessions/${encodeURIComponent(sessionID)}`, z.null(), {
    method: "DELETE",
  });
}

export function archiveSession(token: string, sessionID: string): Promise<Session> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/archive`, session, {
    method: "POST",
  });
}

export function restoreSession(token: string, sessionID: string): Promise<Session> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/restore`, session, {
    method: "POST",
  });
}

export function unloadSessionApp(token: string, sessionID: string, appID: string): Promise<Session> {
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/apps/${encodeURIComponent(appID)}`,
    session,
    { method: "DELETE" },
  );
}

export function listMessages(
  token: string,
  sessionID: string,
  params: { before?: string; limit?: number } = {},
): Promise<{ messages: Message[]; hasMore: boolean }> {
  const query = new URLSearchParams();
  if (params.before) {
    query.set("before", params.before);
  }
  if (params.limit) {
    query.set("limit", String(params.limit));
  }
  const suffix = query.size > 0 ? `?${query.toString()}` : "";
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/messages${suffix}`, listMessagesResponse);
}

export function listTurns(
  token: string,
  sessionID: string,
  params: { before?: string; limit?: number } = {},
): Promise<{ turns: ConversationTurn[]; hasMore: boolean }> {
  const query = new URLSearchParams();
  if (params.before) {
    query.set("before", params.before);
  }
  if (params.limit) {
    query.set("limit", String(params.limit));
  }
  const suffix = query.size > 0 ? `?${query.toString()}` : "";
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/turns${suffix}`, listTurnsResponse);
}

export function getTurn(token: string, sessionID: string, turnID: string): Promise<ConversationTurn> {
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/turns/${encodeURIComponent(turnID)}`,
    conversationTurn,
  );
}

export function getTurnFileChange(token: string, sessionID: string, turnID: string, changeID: string): Promise<TurnFileChange> {
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/turns/${encodeURIComponent(turnID)}/file-changes/${encodeURIComponent(changeID)}`,
    turnFileChange,
  );
}

export function undoTurnFileChanges(token: string, sessionID: string, turnID: string): Promise<TurnFileChangeActionResponse> {
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/turns/${encodeURIComponent(turnID)}/file-changes/undo`,
    turnFileChangeActionResponse,
    { method: "POST" },
  );
}

export function redoTurnFileChanges(token: string, sessionID: string, turnID: string): Promise<TurnFileChangeActionResponse> {
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/turns/${encodeURIComponent(turnID)}/file-changes/redo`,
    turnFileChangeActionResponse,
    { method: "POST" },
  );
}

export function getSessionUsage(token: string, sessionID: string): Promise<SessionUsage> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/usage`, sessionUsage);
}

export function getDailyUsage(token: string, days = 365): Promise<{ days: DailyUsageStat[] }> {
  return request(token, `/usage/daily?days=${encodeURIComponent(String(days))}`, dailyUsageResponse);
}

export function listQueuedInputs(token: string, sessionID: string): Promise<{ queuedInputs: QueuedInput[] }> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/queued-inputs`, listQueuedInputsResponse);
}

export function getAudioBindings(token: string, sessionID: string): Promise<{ bindings: AudioBindings }> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/audio/bindings`, audioBindingsResponse);
}

export function listBrowserTabs(token: string, sessionID: string): Promise<{ tabs: BrowserTab[]; processMode?: "headless" | "webview" | "external" }> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/tabs`, listBrowserTabsResponse);
}

export async function clearBrowserState(token: string, sessionID: string): Promise<void> {
  await request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/state`, z.null(), {
    method: "DELETE",
  });
}

export async function closeBrowserSession(token: string, sessionID: string): Promise<void> {
  await request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/close`, z.null(), {
    method: "POST",
  });
}

export function createBrowserTab(token: string, sessionID: string): Promise<BrowserTab> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/tabs`, browserTab, {
    method: "POST",
  });
}

export function listBackgroundProcesses(token: string, sessionID: string): Promise<{ processes: BackgroundProcess[] }> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/processes`, listBackgroundProcessesResponse);
}

export function getBackgroundProcess(
  token: string,
  sessionID: string,
  processID: string,
  tailBytes = 65_536,
): Promise<BackgroundProcessLog> {
  const params = new URLSearchParams({ tail_bytes: String(tailBytes) });
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/processes/${encodeURIComponent(processID)}?${params.toString()}`,
    backgroundProcessLog,
  );
}

export async function stopBackgroundProcess(token: string, sessionID: string, processID: string): Promise<void> {
  await request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/processes/${encodeURIComponent(processID)}`,
    z.null(),
    { method: "DELETE" },
  );
}

export function openBrowserURL(
  token: string,
  sessionID: string,
  body: z.infer<typeof browserOpenRequest>,
): Promise<BrowserTab> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/open`, browserTab, {
    method: "POST",
    body: JSON.stringify(browserOpenRequest.parse(body)),
  });
}

export async function listBrowserHistory(
  token: string,
  sessionID: string,
  query = "",
  limit = 20,
): Promise<{ history: BrowserHistoryEntry[] }> {
  const params = new URLSearchParams({ limit: String(limit) });
  if (query.trim()) {
    params.set("q", query.trim());
  }
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/browser/history?${params.toString()}`,
    listBrowserHistoryResponse,
  );
}

export async function deleteBrowserHistoryEntry(token: string, sessionID: string, historyID: string): Promise<void> {
  await request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/browser/history/${encodeURIComponent(historyID)}`,
    z.null(),
    { method: "DELETE" },
  );
}

export async function clearBrowserHistory(token: string, sessionID: string): Promise<void> {
  await request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/history`, z.null(), { method: "DELETE" });
}

export function openBrowserTab(
  token: string,
  sessionID: string,
  tabID: string,
  body: z.infer<typeof browserOpenRequest>,
): Promise<BrowserTab> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/tabs/${encodeURIComponent(tabID)}/open`, browserTab, {
    method: "POST",
    body: JSON.stringify(browserOpenRequest.parse(body)),
  });
}

export function syncBrowserTab(
  token: string,
  sessionID: string,
  tabID: string,
  body: z.infer<typeof browserSyncRequest>,
): Promise<BrowserTab> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/tabs/${encodeURIComponent(tabID)}/sync`, browserTab, {
    method: "POST",
    body: JSON.stringify(browserSyncRequest.parse(body)),
  });
}

export function adoptBrowserTab(token: string, sessionID: string, tabID: string): Promise<BrowserTab> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/tabs/${encodeURIComponent(tabID)}/adopt`, browserTab, {
    method: "POST",
  });
}

export function backBrowserTab(token: string, sessionID: string, tabID: string): Promise<BrowserTab> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/tabs/${encodeURIComponent(tabID)}/back`, browserTab, {
    method: "POST",
  });
}

export function forwardBrowserTab(token: string, sessionID: string, tabID: string): Promise<BrowserTab> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/tabs/${encodeURIComponent(tabID)}/forward`, browserTab, {
    method: "POST",
  });
}

export function reloadBrowserTab(token: string, sessionID: string, tabID: string): Promise<BrowserTab> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/tabs/${encodeURIComponent(tabID)}/reload`, browserTab, {
    method: "POST",
  });
}

export function recoverBrowserTab(token: string, sessionID: string, tabID: string): Promise<BrowserTab> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/tabs/${encodeURIComponent(tabID)}/recover`, browserTab, {
    method: "POST",
  });
}

export function observeBrowserTab(
  token: string,
  sessionID: string,
  tabID: string,
  body: z.infer<typeof browserObserveRequest> = {},
): Promise<BrowserObservation> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/tabs/${encodeURIComponent(tabID)}/observe`, browserObservation, {
    method: "POST",
    body: JSON.stringify(browserObserveRequest.parse(body)),
  });
}

export function screenshotBrowserTab(
  token: string,
  sessionID: string,
  tabID: string,
  body: z.infer<typeof browserScreenshotRequest> = {},
): Promise<BrowserScreenshot> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/tabs/${encodeURIComponent(tabID)}/screenshot`, browserScreenshot, {
    method: "POST",
    body: JSON.stringify(browserScreenshotRequest.parse(body)),
  });
}

export function clickBrowserTab(
  token: string,
  sessionID: string,
  tabID: string,
  body: z.infer<typeof browserClickRequest>,
): Promise<BrowserActionResult> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/tabs/${encodeURIComponent(tabID)}/click`, browserActionResult, {
    method: "POST",
    body: JSON.stringify(browserClickRequest.parse(body)),
  });
}

export function typeBrowserTab(
  token: string,
  sessionID: string,
  tabID: string,
  body: z.infer<typeof browserTypeRequest>,
): Promise<BrowserActionResult> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/tabs/${encodeURIComponent(tabID)}/type`, browserActionResult, {
    method: "POST",
    body: JSON.stringify(browserTypeRequest.parse(body)),
  });
}

export function scrollBrowserTab(
  token: string,
  sessionID: string,
  tabID: string,
  body: z.infer<typeof browserScrollRequest> = {},
): Promise<BrowserActionResult> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/tabs/${encodeURIComponent(tabID)}/scroll`, browserActionResult, {
    method: "POST",
    body: JSON.stringify(browserScrollRequest.parse(body)),
  });
}

export async function releaseBrowserTab(token: string, sessionID: string, tabID: string): Promise<void> {
  await request(token, `/sessions/${encodeURIComponent(sessionID)}/browser/tabs/${encodeURIComponent(tabID)}/release`, z.null(), {
    method: "POST",
  });
}

export function bindAudioInput(
  token: string,
  sessionID: string,
  enabled: boolean,
  mode?: AudioInputMode,
): Promise<{ ok: boolean; bindings: AudioBindings }> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/audio/input`, audioBindingResponse, {
    method: "POST",
    body: JSON.stringify(audioBindingRequest.parse({ enabled, mode })),
  });
}

export function getAudioRuntime(token: string): Promise<AudioRuntimeStatus> {
  return request(token, "/settings/audio/runtime", audioRuntimeStatus);
}

export function startAudioRuntimeInstall(token: string): Promise<AudioRuntimeStatus> {
  return request(token, "/settings/audio/runtime/install", audioRuntimeStatus, {
    method: "POST",
  });
}

export function cancelAudioRuntimeInstall(token: string): Promise<AudioRuntimeStatus> {
  return request(token, "/settings/audio/runtime/cancel", audioRuntimeStatus, {
    method: "POST",
  });
}

export function listCanvasItems(token: string, sessionID: string): Promise<{ items: CanvasItem[] }> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/canvas/items`, listCanvasItemsResponse);
}

export function createCanvasItem(token: string, sessionID: string, body: CanvasItemPayload): Promise<CanvasItem> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/canvas/items`, canvasItem, {
    method: "POST",
    body: JSON.stringify(putCanvasItemRequest.parse(body)),
  });
}

export function putCanvasItem(token: string, sessionID: string, itemID: string, body: CanvasItemPayload): Promise<CanvasItem> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/canvas/items/${encodeURIComponent(itemID)}`, canvasItem, {
    method: "PUT",
    body: JSON.stringify(putCanvasItemRequest.parse(body)),
  });
}

export function patchCanvasItemWindow(
  token: string,
  sessionID: string,
  itemID: string,
  body: CanvasItemWindowPayload,
): Promise<CanvasItem> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/canvas/items/${encodeURIComponent(itemID)}`, canvasItem, {
    method: "PATCH",
    body: JSON.stringify(patchCanvasItemRequest.parse(body)),
  });
}

export async function deleteCanvasItem(token: string, sessionID: string, itemID: string): Promise<void> {
  await request(token, `/sessions/${encodeURIComponent(sessionID)}/canvas/items/${encodeURIComponent(itemID)}`, z.null(), {
    method: "DELETE",
  });
}

export function listSavedCanvasItems(token: string, sessionID: string): Promise<{ items: SavedCanvasItem[] }> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/canvas/saved`, listSavedCanvasItemsResponse);
}

export function saveCanvasItem(token: string, sessionID: string, itemID: string): Promise<CanvasSaveResult> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/canvas/items/${encodeURIComponent(itemID)}/save`, canvasSaveResult, {
    method: "POST",
  });
}

export function openSavedCanvasItem(token: string, sessionID: string, savedItemID: string): Promise<CanvasItem> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/canvas/saved/${encodeURIComponent(savedItemID)}/open`, canvasItem, {
    method: "POST",
  });
}

export async function deleteSavedCanvasItem(token: string, sessionID: string, savedItemID: string): Promise<void> {
  await request(token, `/sessions/${encodeURIComponent(sessionID)}/canvas/saved/${encodeURIComponent(savedItemID)}`, z.null(), {
    method: "DELETE",
  });
}

export function listClosedCanvasItems(
  token: string,
  sessionID: string,
  limit = 20,
): Promise<{ items: ClosedCanvasItem[] }> {
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/canvas/closed?limit=${encodeURIComponent(String(limit))}`,
    listClosedCanvasItemsResponse,
  );
}

export function createClosedCanvasItem(
  token: string,
  sessionID: string,
  body: ClosedCanvasItemPayload,
): Promise<ClosedCanvasItem> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/canvas/closed`, closedCanvasItem, {
    method: "POST",
    body: JSON.stringify(putClosedCanvasItemRequest.parse(body)),
  });
}

export async function deleteClosedCanvasItem(token: string, sessionID: string, closedID: string): Promise<void> {
  await request(token, `/sessions/${encodeURIComponent(sessionID)}/canvas/closed/${encodeURIComponent(closedID)}`, z.null(), {
    method: "DELETE",
  });
}

export async function clearClosedCanvasItems(token: string, sessionID: string): Promise<void> {
  await request(token, `/sessions/${encodeURIComponent(sessionID)}/canvas/closed`, z.null(), {
    method: "DELETE",
  });
}

export function updateQueuedInput(
  token: string,
  sessionID: string,
  clientMessageID: string,
  body: z.infer<typeof patchQueuedInputRequest>,
): Promise<QueuedInput> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/queued-inputs/${encodeURIComponent(clientMessageID)}`, queuedInput, {
    method: "PATCH",
    body: JSON.stringify(patchQueuedInputRequest.parse(body)),
  });
}

export function submitMessage(
  token: string,
  sessionID: string,
  body: SubmitPayload,
): Promise<SubmitResult> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/submit`, submitResponse, {
    method: "POST",
    body: JSON.stringify(submitRequest.parse(body)),
  });
}

export function steerTurn(
  token: string,
  sessionID: string,
  turnID: string,
  body: SubmitPayload,
): Promise<z.infer<typeof steerResponse>> {
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/turns/${encodeURIComponent(turnID)}/steer`,
    steerResponse,
    {
      method: "POST",
      body: JSON.stringify(steerRequest.parse(body)),
    },
  );
}

export function steerQueuedInput(
  token: string,
  sessionID: string,
  clientMessageID: string,
  turnID: string,
): Promise<z.infer<typeof steerResponse>> {
  return request(
    token,
    `/sessions/${encodeURIComponent(sessionID)}/queued-inputs/${encodeURIComponent(clientMessageID)}/steer`,
    steerResponse,
    {
      method: "POST",
      body: JSON.stringify(steerQueuedInputRequest.parse({ turnID })),
    },
  );
}

export async function uploadAttachment(token: string, sessionID: string, file: File, options?: { origin?: "temp"; sourcePath?: string }): Promise<Attachment> {
  const form = new FormData();
  form.append("file", file);
  if (options?.origin) {
    form.append("origin", options.origin);
  }
  if (options?.sourcePath) {
    form.append("sourcePath", options.sourcePath);
  }
  const response = await fetch(apiURL(`/sessions/${encodeURIComponent(sessionID)}/attachments`), {
    method: "POST",
    headers: authHeaders(token),
    body: form,
  });
  const payload = await readJSON(response);
  if (!response.ok) {
    const code = typeof payload?.error === "string" ? payload.error : response.statusText;
    const detail = typeof payload?.detail === "string" ? payload.detail : undefined;
    throw new APIError(response.status, code, detail);
  }
  return attachment.parse(payload);
}

const desktopScreenshotResponse = z.object({
  attachments: z.array(attachment),
});

export async function captureDesktopScreenshot(token: string, sessionID: string): Promise<Attachment[]> {
  const result = await request(token, `/sessions/${encodeURIComponent(sessionID)}/desktop/screenshot`, desktopScreenshotResponse, {
    method: "POST",
  });
  return result.attachments;
}

export function captureDesktopPhoto(token: string, sessionID: string): Promise<Attachment> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/desktop/photo`, attachment, {
    method: "POST",
  });
}

export async function revealDesktopPath(token: string, path: string): Promise<void> {
  await request(token, "/desktop/reveal-file", z.object({ ok: z.boolean() }), {
    method: "POST",
    body: JSON.stringify({ path }),
  });
}

export async function cancelTurn(token: string, sessionID: string): Promise<void> {
  await request(token, `/sessions/${encodeURIComponent(sessionID)}/cancel`, z.object({ status: z.string() }), {
    method: "POST",
  });
}

export function compactSession(token: string, sessionID: string, body: { hint?: string } = {}): Promise<CompactResult> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/compact`, compactResponse, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export function listPendingApprovals(token: string, sessionID: string): Promise<{ approvals: PendingApproval[] }> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/approvals`, listPendingApprovalsResponse);
}

export function approveApproval(
  token: string,
  sessionID: string,
  approvalID: string,
  scope: "turn" | "session" = "turn",
  projectDirs: string[] = [],
): Promise<ApproveApprovalResponse> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/approvals/${encodeURIComponent(approvalID)}/approve`, approveApprovalResponse, {
    method: "POST",
    body: JSON.stringify({ scope, projectDirs }),
  });
}

export async function denyApproval(token: string, sessionID: string, approvalID: string, reason = ""): Promise<void> {
  await request(token, `/sessions/${encodeURIComponent(sessionID)}/approvals/${encodeURIComponent(approvalID)}/deny`, z.object({ status: z.string() }), {
    method: "POST",
    body: JSON.stringify({ reason }),
  });
}

export function listProviderModels(token: string, name: string): Promise<{ models: string[] }> {
  return request(token, `/providers/${encodeURIComponent(name)}/models`, listModelsResponse);
}

export function probeProviderModels(
  token: string,
  body: z.infer<typeof probeProviderModelsRequest>,
): Promise<{ models: string[] }> {
  return request(token, "/providers/models", listModelsResponse, {
    method: "POST",
    body: JSON.stringify(probeProviderModelsRequest.parse(body)),
  });
}

export function getSettings(token: string): Promise<{ settings: Record<string, string> }> {
  return request(token, "/settings", settingsResponse);
}

export function resetSettings(token: string): Promise<{ settings: Record<string, string> }> {
  return request(token, "/settings", settingsResponse, { method: "DELETE" });
}

export function getDesktopAbout(token: string): Promise<{ sections: DesktopAboutSection[] }> {
  return request(token, "/desktop/about", desktopAboutResponse);
}

export function getAudioConfig(token: string): Promise<AudioConfigResponse> {
  return request(token, "/settings/audio", audioConfigResponse);
}

export function resetAudioConfig(token: string): Promise<AudioConfigResponse> {
  return request(token, "/settings/audio", audioConfigResponse, { method: "DELETE" });
}

export function getUserPrompt(token: string): Promise<UserPrompt> {
  return request(token, "/settings/user-prompt", userPromptResponse);
}

export function listBuiltinTools(token: string): Promise<{ tools: BuiltinTool[] }> {
  return request(token, "/tools/builtin", listBuiltinToolsResponse);
}

export function listBrowserMCPSessions(token: string): Promise<{ sessions: BrowserMCPSession[] }> {
  return request(token, "/mcp/browser-sessions", listBrowserMCPSessionsResponse);
}

export function listApps(token: string): Promise<{ apps: AppDefinition[] }> {
  return request(token, "/apps", listAppsResponse);
}

export function installAppPackage(token: string, body: z.infer<typeof installAppRequest>): Promise<AppDefinition> {
  return request(token, "/apps/install", appDefinition, {
    method: "POST",
    body: JSON.stringify(installAppRequest.parse(body)),
  });
}

export function importMCPApps(token: string, configJSON: string, name?: string): Promise<{ apps: AppDefinition[] }> {
  return request(token, "/apps/mcp", importMCPAppsResponse, {
    method: "POST",
    body: JSON.stringify(appMCPConfigRequest.parse({ configJSON, name })),
  });
}

export function getMCPAppConfig(token: string, id: string): Promise<{ configJSON: string }> {
  return request(token, `/apps/${encodeURIComponent(id)}/mcp-config`, appMCPConfigResponse);
}

export function putMCPAppConfig(token: string, id: string, configJSON: string, name?: string): Promise<AppDefinition> {
  return request(token, `/apps/${encodeURIComponent(id)}/mcp-config`, appDefinition, {
    method: "PUT",
    body: JSON.stringify(appMCPConfigRequest.parse({ configJSON, name })),
  });
}

export async function deleteApp(token: string, id: string): Promise<void> {
  try {
    await request(token, `/apps/${encodeURIComponent(id)}`, z.null(), {
      method: "DELETE",
    });
  } catch (error) {
    // DELETE is idempotent from the desktop client's perspective. A retry may
    // finish stale connection/session cleanup after the App directory was
    // already removed by the first attempt.
    if (!(error instanceof APIError) || error.status !== 404) {
      throw error;
    }
  }
}

export function setAppEnabled(token: string, id: string, enabled: boolean): Promise<AppDefinition> {
  return request(token, `/apps/${encodeURIComponent(id)}/enabled`, appDefinition, {
    method: "PUT",
    body: JSON.stringify({ enabled }),
  });
}

export function listAppConnections(token: string): Promise<{ connections: AppConnection[] }> {
  return request(token, "/app-connections", listAppConnectionsResponse);
}

export function getAppMCPStatus(token: string, appID: string): Promise<AppMCPStatusResponse> {
  return request(token, `/apps/${encodeURIComponent(appID)}/mcp`, appMCPStatusResponse);
}

export function getAppMCPOverride(token: string, appID: string, endpointName: string): Promise<AppMCPOverrideResponse> {
  return request(token, `/apps/${encodeURIComponent(appID)}/mcp-overrides/${encodeURIComponent(endpointName)}`, appMCPOverrideResponse);
}

export function putAppMCPOverride(token: string, appID: string, endpointName: string, body: AppMCPOverride): Promise<AppMCPOverrideResponse> {
  return request(token, `/apps/${encodeURIComponent(appID)}/mcp-overrides/${encodeURIComponent(endpointName)}`, appMCPOverrideResponse, {
    method: "PUT",
    body: JSON.stringify(appMCPOverride.parse(body)),
  });
}

export async function deleteAppMCPOverride(token: string, appID: string, endpointName: string): Promise<void> {
  await request(token, `/apps/${encodeURIComponent(appID)}/mcp-overrides/${encodeURIComponent(endpointName)}`, z.null(), {
    method: "DELETE",
  });
}

export function getAppConnection(token: string, id: string): Promise<AppConnection> {
  return request(token, `/app-connections/${encodeURIComponent(id)}`, appConnection);
}

export function putAppConnection(token: string, id: string, body: AppConnectionPayload): Promise<AppConnection> {
  return request(token, `/app-connections/${encodeURIComponent(id)}`, appConnection, {
    method: "PUT",
    body: JSON.stringify(body),
  });
}

export async function deleteAppConnection(token: string, id: string): Promise<void> {
  await request(token, `/app-connections/${encodeURIComponent(id)}`, z.null(), {
    method: "DELETE",
  });
}

export function startAppOAuth(
  token: string,
  body: z.infer<typeof startAppOAuthRequest>,
): Promise<z.infer<typeof startAppOAuthResponse>> {
  return request(token, "/app-oauth/start", startAppOAuthResponse, {
    method: "POST",
    body: JSON.stringify(startAppOAuthRequest.parse(body)),
  });
}

export function completeAppOAuth(
  token: string,
  body: z.infer<typeof completeAppOAuthRequest>,
): Promise<AppConnection> {
  return request(token, "/app-oauth/complete", appConnection, {
    method: "POST",
    body: JSON.stringify(completeAppOAuthRequest.parse(body)),
  });
}

export function getAppSkill(token: string, appID: string, path: string): Promise<AppSkillDetail> {
  const skillPath = `${appID}/${path}`.split("/").map(encodeURIComponent).join("/");
  return request(token, `/app-skills/${skillPath}`, appSkillDetail);
}

export function appIconURL(
  token: string,
  app: { id: string; icon?: { svg?: string }; packageSHA256?: string },
): string | undefined {
  const raw = app.icon?.svg?.trim().replace(/^\.\//, "");
  if (!raw) {
    return undefined;
  }
  if (raw.startsWith("/") || /^[a-z][a-z0-9+.-]*:/i.test(raw)) {
    return undefined;
  }
  const path = `${app.id}/${raw}`.split("/").map(encodeURIComponent).join("/");
  const params = new URLSearchParams({ token });
  const packageSHA256 = app.packageSHA256?.trim();
  if (packageSHA256) {
    params.set("v", packageSHA256);
  }
  return apiURL(`/app-assets/${path}?${params.toString()}`);
}

export function listSkills(token: string): Promise<{ skills: Skill[] }> {
  return request(token, "/skills", listSkillsResponse);
}

export function skillIconURL(token: string, skill: { iconPath?: string }): string | undefined {
  const raw = skill.iconPath?.trim();
  if (!raw) {
    return undefined;
  }
  const path = raw.split("/").map(encodeURIComponent).join("/");
  return apiURL(`/skill-assets/${path}?token=${encodeURIComponent(token)}`);
}

export async function deleteSkill(token: string, id: string): Promise<void> {
  await request(token, `/skills/${encodeURIComponent(id)}`, z.null(), {
    method: "DELETE",
  });
}

export async function putSettings(token: string, settings: Record<string, string>): Promise<void> {
  await request(token, "/settings", z.null(), {
    method: "PUT",
    body: JSON.stringify(settings),
  });
}

export function putAudioConfig(token: string, body: AudioConfig): Promise<AudioConfigResponse> {
  return request(token, "/settings/audio", audioConfigResponse, {
    method: "PUT",
    body: JSON.stringify(audioConfig.parse(body)),
  });
}

export function clearASRRecordings(token: string, sessionID: string): Promise<ClearASRRecordingsResponse> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/audio/asr-recordings`, clearASRRecordingsResponse, {
    method: "DELETE",
  });
}

export function putUserPrompt(token: string, content: string): Promise<UserPrompt> {
  return request(token, "/settings/user-prompt", userPromptResponse, {
    method: "PUT",
    body: JSON.stringify({ content }),
  });
}

export function getWebTools(token: string): Promise<WebToolsConfig> {
  return request(token, "/tools/web", webToolsConfig);
}

export function patchWebTools(
  token: string,
  body: z.infer<typeof patchWebToolsRequest>,
): Promise<WebToolsConfig> {
  return request(token, "/tools/web", webToolsConfig, {
    method: "PATCH",
    body: JSON.stringify(patchWebToolsRequest.parse(body)),
  });
}

export function listProviders(token: string): Promise<{ providers: ProviderProfile[] }> {
  return request(token, "/providers", listProvidersResponse);
}

export function createProvider(
  token: string,
  body: z.infer<typeof createProviderRequest>,
): Promise<ProviderProfile> {
  return request(token, "/providers", providerProfile, {
    method: "POST",
    body: JSON.stringify(createProviderRequest.parse(body)),
  });
}

export function patchProvider(
  token: string,
  name: string,
  body: z.infer<typeof patchProviderRequest>,
): Promise<ProviderProfile> {
  return request(token, `/providers/${encodeURIComponent(name)}`, providerProfile, {
    method: "PATCH",
    body: JSON.stringify(patchProviderRequest.parse(body)),
  });
}

export async function deleteProvider(token: string, name: string): Promise<void> {
  await request(token, `/providers/${encodeURIComponent(name)}`, z.null(), {
    method: "DELETE",
  });
}

export type { AppConnection, AppDefinition, AppMCPEndpointStatus, AppMCPStatusResponse, AppMCPTool, AppSkillDetail, Attachment, AudioBindings, BackgroundProcess, BackgroundProcessLog, BuiltinTool, BrowserActionResult, BrowserHistoryEntry, BrowserMCPSession, BrowserObservation, BrowserScreenshot, BrowserTab, ContentPart, DailyUsageStat, DesktopAboutSection, LocalFolder, Message, PendingApproval, ConversationTurn, Project, ProjectReference, ProviderModel, ProviderProfile, QueuedInput, Session, SessionUsage, Skill, TurnFileChange, WebToolsConfig };
export { createProjectRequest, createProviderRequest, mergeProjectRequest, patchProjectRequest, patchProviderRequest };
