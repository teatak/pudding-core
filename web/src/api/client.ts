import {
  conflictResponse,
  listModelsResponse,
  createProviderRequest,
  listMessagesResponse,
  listProvidersResponse,
  listSessionsResponse,
  listTurnsResponse,
  message,
  patchProviderRequest,
  providerProfile,
  session,
  settingsResponse,
  submitResponse,
  type ContentPart,
  type Message,
  type ConversationTurn,
  type ProviderModel,
  type ProviderProfile,
  type Session,
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
  pinned: z.boolean().optional(),
  pinnedOrder: z.number().optional(),
});

export type SubmitResult = z.infer<typeof submitResponse>;

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

export function submitMessage(
  token: string,
  sessionID: string,
  body: { clientMessageID: string; text: string },
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

export function listProviderModels(token: string, name: string): Promise<{ models: string[] }> {
  return request(token, `/providers/${encodeURIComponent(name)}/models`, listModelsResponse);
}

export function getSettings(token: string): Promise<{ settings: Record<string, string> }> {
  return request(token, "/settings", settingsResponse);
}

export async function putSettings(token: string, settings: Record<string, string>): Promise<void> {
  await request(token, "/settings", z.null(), {
    method: "PUT",
    body: JSON.stringify(settings),
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

export type { ContentPart, Message, ConversationTurn, ProviderModel, ProviderProfile, Session };
export { createProviderRequest, patchProviderRequest };
