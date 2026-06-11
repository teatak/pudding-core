import {
  conflictResponse,
  listMessagesResponse,
  listSessionsResponse,
  message,
  session,
  settingsResponse,
  submitResponse,
  type Message,
  type Session,
} from "@/contracts/api";
import { z } from "zod";

export class APIError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
  ) {
    super(code);
  }
}

const sessionRequest = z.object({
  title: z.string().optional(),
  model: z.string().optional(),
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
  const response = await fetch(path, {
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
  body: z.infer<typeof sessionRequest>,
): Promise<Session> {
  return request(token, "/sessions", session, {
    method: "POST",
    body: JSON.stringify(sessionRequest.parse(body)),
  });
}

export function updateSession(
  token: string,
  sessionID: string,
  body: z.infer<typeof sessionRequest>,
): Promise<Session> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}`, session, {
    method: "PATCH",
    body: JSON.stringify(sessionRequest.parse(body)),
  });
}

export async function deleteSession(token: string, sessionID: string): Promise<void> {
  await request(token, `/sessions/${encodeURIComponent(sessionID)}`, z.null(), {
    method: "DELETE",
  });
}

export function listMessages(token: string, sessionID: string): Promise<{ messages: Message[] }> {
  return request(token, `/sessions/${encodeURIComponent(sessionID)}/messages`, listMessagesResponse);
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

export function getSettings(token: string): Promise<{ settings: Record<string, string> }> {
  return request(token, "/settings", settingsResponse);
}

export async function putSettings(token: string, settings: Record<string, string>): Promise<void> {
  await request(token, "/settings", z.null(), {
    method: "PUT",
    body: JSON.stringify(settings),
  });
}

export type { Message, Session };
export { message, session };
