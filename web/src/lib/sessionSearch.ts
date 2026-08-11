import type { Message, Project, Session } from "@/api/client";

export type SessionSearchResult = {
  session: Session;
  project?: Project;
  message?: Message;
  score: number;
};

export function buildSessionSearchResults(
  sessions: Session[],
  projects: Project[],
  messages: Message[],
  normalizedQuery: string,
): SessionSearchResult[] {
  const queryTerms = sessionSearchTerms(normalizedQuery);
  const projectByID = new Map(projects.map((project) => [project.id, project]));
  const messageBySessionID = new Map<string, Message>();
  for (const message of messages) {
    if (!messageBySessionID.has(message.sessionID)) {
      messageBySessionID.set(message.sessionID, message);
    }
  }

  return sessions
    .map((session): SessionSearchResult | null => {
      const project = session.projectID ? projectByID.get(session.projectID) : undefined;
      const message = messageBySessionID.get(session.id);
      const title = normalizeSessionSearchText(session.title);
      const projectText = normalizeSessionSearchText([project?.name, ...(project?.rootDirs || [])].filter(Boolean).join(" "));
      const modelText = normalizeSessionSearchText(`${session.provider} ${session.model}`);
      let score = 0;

      if (normalizedQuery) {
        if (title === normalizedQuery) {
          score = 0;
        } else if (title.startsWith(normalizedQuery)) {
          score = 1;
        } else if (containsSessionSearchTerms(title, queryTerms)) {
          score = 2;
        } else if (containsSessionSearchTerms(projectText, queryTerms)) {
          score = 3;
        } else if (containsSessionSearchTerms(modelText, queryTerms)) {
          score = 4;
        } else if (message) {
          score = 5;
        } else {
          return null;
        }
      }
      return { session, project, message, score };
    })
    .filter((result): result is SessionSearchResult => Boolean(result))
    .sort((left, right) => {
      if (left.score !== right.score) {
        return left.score - right.score;
      }
      return sessionActivityTime(right.session) - sessionActivityTime(left.session);
    });
}

export function normalizeSessionSearchText(value: string) {
  return value.trim().toLocaleLowerCase();
}

export function sessionSearchTerms(normalizedQuery: string) {
  return normalizedQuery.split(/\s+/).filter(Boolean);
}

export function sessionSearchExcerpt(text: string, terms: string[], maxLength = 120) {
  const compact = text.replace(/\s+/g, " ").trim();
  if (compact.length <= maxLength) {
    return compact;
  }
  const normalizedCompact = compact.toLocaleLowerCase();
  const matchIndexes = terms
    .map((term) => normalizedCompact.indexOf(term))
    .filter((index) => index >= 0);
  const matchIndex = matchIndexes.length > 0 ? Math.min(...matchIndexes) : -1;
  const start = Math.max(0, matchIndex < 0 ? 0 : matchIndex - Math.floor(maxLength / 3));
  const excerpt = compact.slice(start, start + maxLength).trim();
  return `${start > 0 ? "…" : ""}${excerpt}${start + maxLength < compact.length ? "…" : ""}`;
}

function containsSessionSearchTerms(text: string, terms: string[]) {
  return terms.length > 0 && terms.every((term) => text.includes(term));
}

function sessionActivityTime(session: Session) {
  return new Date(session.lastActivityAt || session.updatedAt || session.createdAt).getTime();
}
