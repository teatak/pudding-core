import {
  listBuiltinToolsResponse,
  compactResponse,
  conflictResponse,
  listModelsResponse,
  createProviderRequest,
  listMessagesResponse,
  listPendingApprovalsResponse,
  listProvidersResponse,
  listQueuedInputsResponse,
  listSessionsResponse,
  listTurnsResponse,
  message,
  patchQueuedInputRequest,
  patchProviderRequest,
  probeProviderModelsRequest,
  patchWebToolsRequest,
  providerProfile,
  queuedInput,
  session,
  sessionUsage,
  settingsResponse,
  submitResponse,
  conversationTurn,
  dailyUsageResponse,
  userPromptResponse,
  webToolsConfig,
  type BuiltinTool,
  type ContentPart,
  type DailyUsageStat,
  type Message,
  type PendingApproval,
  type ConversationTurn,
  type ProviderModel,
  type ProviderProfile,
  type QueuedInput,
  type Session,
  type SessionUsage,
  type WebToolsConfig,
} from "@/contracts/api";
import { z } from "zod";

import { apiURL } from "@/state/apiBase";

export class APIError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
  ) {
    super(code);
  }
}

const createSessionRequest = z.object({
  title: z.string().optional(),
  provider: z.string().min(1),
  model: z.string().min(1),
});

const sessionPatchRequest = z.object({
  title: z.string().optional(),
  provider: z.string().optional(),
  model: z.string().optional(),
  reasoningEffort: z.string().optional(),
  activeMode: z.enum(["chat", "research", "workspace"]).optional(),
  modeLease: z.enum(["none", "session"]).optional(),
  workspaceDirs: z.array(z.string()).optional(),
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
const mobilePairingResponse = z.object({
  code: z.string(),
  url: z.string(),
  urls: z.array(z.string()),
  expiresAt: z.string(),
  qrDataURL: z.string().optional(),
});

export type SubmitResult = z.infer<typeof submitResponse>;
export type CompactResult = z.infer<typeof compactResponse>;
export type MobilePairing = z.infer<typeof mobilePairingResponse>;
export type UserPrompt = z.infer<typeof userPromptResponse>;

function authHeaders(token: string) {
  return {
    Authorization: `Bearer ${token}`,
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
    throw new APIError(response.status, code);
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
    throw new APIError(response.status, code);
  }
  return schema.parse(payload);
}

export function claimMobilePairing(
  code: string,
  body: { deviceName?: string } = {},
): Promise<z.infer<typeof mobilePairingClaimResponse>> {
  return publicRequest(
    `/mobile/pairings/${encodeURIComponent(code)}/claim`,
    mobilePairingClaimResponse,
    {
      method: "POST",
      body: JSON.stringify(body),
    },
  );
}

export function createMobilePairing(token: string): Promise<MobilePairing> {
  return request(token, "/mobile/pairings", mobilePairingResponse, {
    method: "POST",
  });
}

export function listSessions(token: string): Promise<{ sessions: Session[] }> {
  return request(token, "/sessions", listSessionsResponse);
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

export function getSessionUsage(token: string, sessionID: string): Promise<SessionUsage> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/usage`, sessionUsage);
}

export function getDailyUsage(token: string, days = 365): Promise<{ days: DailyUsageStat[] }> {
  return request(token, `/usage/daily?days=${encodeURIComponent(String(days))}`, dailyUsageResponse);
}

export function listQueuedInputs(token: string, sessionID: string): Promise<{ queuedInputs: QueuedInput[] }> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/queued-inputs`, listQueuedInputsResponse);
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
  body: { clientMessageID: string; kind?: "system" | "user"; reasoningEffort?: string; text: string },
): Promise<SubmitResult> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/submit`, submitResponse, {
    method: "POST",
    body: JSON.stringify(body),
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

export async function approveApproval(
  token: string,
  sessionID: string,
  approvalID: string,
  scope: "turn" | "session" = "turn",
  workspaceDirs: string[] = [],
): Promise<void> {
  await request(token, `/sessions/${encodeURIComponent(sessionID)}/approvals/${encodeURIComponent(approvalID)}/approve`, z.object({ status: z.string() }), {
    method: "POST",
    body: JSON.stringify({ scope, workspaceDirs }),
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

export function getUserPrompt(token: string): Promise<UserPrompt> {
  return request(token, "/settings/user-prompt", userPromptResponse);
}

export function listBuiltinTools(token: string): Promise<{ tools: BuiltinTool[] }> {
  return request(token, "/tools/builtin", listBuiltinToolsResponse);
}

export async function putSettings(token: string, settings: Record<string, string>): Promise<void> {
  await request(token, "/settings", z.null(), {
    method: "PUT",
    body: JSON.stringify(settings),
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

export type { BuiltinTool, ContentPart, DailyUsageStat, Message, PendingApproval, ConversationTurn, ProviderModel, ProviderProfile, QueuedInput, Session, SessionUsage, WebToolsConfig };
export { createProviderRequest, patchProviderRequest };
