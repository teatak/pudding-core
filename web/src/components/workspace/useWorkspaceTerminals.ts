import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import { createTerminal, deleteTerminal, listTerminals, type Terminal } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { useI18n } from "@/i18n";
import {
  normalizeTerminalDimensions,
  type TerminalDimensions,
} from "@/terminal/terminalDimensions";

const SELECTED_TERMINAL_STORAGE_KEY = "pudding.terminal.selected.v1";

type TerminalData = { terminals: Terminal[] };

export function useWorkspaceTerminals({
  active,
  enabled,
  getInitialDimensions,
  onActivate,
  onDeactivate,
  sessionID,
  token,
}: {
  active: boolean;
  enabled: boolean;
  getInitialDimensions: () => TerminalDimensions;
  onActivate: () => void;
  onDeactivate: () => void;
  sessionID: string;
  token: string;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const selectedRef = useRef<Record<string, string>>(readSelectedTerminals());
  const initialDimensionsRef = useRef<Record<string, TerminalDimensions>>({});
  const [selectedBySession, setSelectedBySession] = useState(selectedRef.current);
  selectedRef.current = selectedBySession;

  const rememberSelected = useCallback((targetSessionID: string, terminalID?: string) => {
    const next = { ...selectedRef.current };
    if (terminalID) {
      next[targetSessionID] = terminalID;
    } else {
      delete next[targetSessionID];
    }
    selectedRef.current = next;
    setSelectedBySession(next);
    writeSelectedTerminals(next);
  }, []);

  const query = useQuery({
    enabled,
    queryKey: sessionID ? queryKeys.terminals(sessionID) : ["terminals", "missing-session"],
    queryFn: () => listTerminals(token, sessionID),
    staleTime: 10_000,
  });
  const terminals = useMemo(
    () => (query.data?.terminals || [])
      .filter((item) => item.sessionID === sessionID)
      .sort((left, right) => Date.parse(left.createdAt) - Date.parse(right.createdAt)),
    [query.data?.terminals, sessionID],
  );
  const selectedID = selectedBySession[sessionID];
  const activeTerminal = terminals.find((item) => item.id === selectedID) || terminals[0];

  useEffect(() => {
    if (sessionID && activeTerminal && selectedRef.current[sessionID] !== activeTerminal.id) {
      rememberSelected(sessionID, activeTerminal.id);
    }
  }, [activeTerminal?.id, sessionID]);

  useEffect(() => {
    if (active && enabled && !query.isFetching && terminals.length === 0) {
      onDeactivate();
    }
  }, [active, enabled, onDeactivate, query.isFetching, terminals.length]);

  const createMutation = useMutation({
    mutationFn: async (cwd?: string) => {
      const dimensions = normalizeTerminalDimensions(getInitialDimensions());
      const item = await createTerminal(token, sessionID, { ...dimensions, cwd });
      return { dimensions, item };
    },
    onSuccess: ({ dimensions, item }) => {
      initialDimensionsRef.current = {
        ...initialDimensionsRef.current,
        [item.id]: dimensions,
      };
      queryClient.setQueryData<TerminalData>(queryKeys.terminals(sessionID), (current) => ({
        terminals: upsertTerminal(current?.terminals || [], item),
      }));
      rememberSelected(sessionID, item.id);
      onActivate();
    },
    onError: () => toast.error(t("terminal.createFailed")),
  });

  const closeMutation = useMutation({
    mutationFn: (terminalID: string) => deleteTerminal(token, sessionID, terminalID),
    onMutate: async (terminalID) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.terminals(sessionID) });
      const previous = queryClient.getQueryData<TerminalData>(queryKeys.terminals(sessionID));
      const previousSelectedID = selectedRef.current[sessionID];
      const current = previous?.terminals || [];
      const closingIndex = current.findIndex((item) => item.id === terminalID);
      const remaining = current.filter((item) => item.id !== terminalID);
      const replacement = remaining[Math.min(Math.max(closingIndex, 0), Math.max(remaining.length - 1, 0))];
      queryClient.setQueryData<TerminalData>(queryKeys.terminals(sessionID), { terminals: remaining });
      if (previousSelectedID === terminalID || !remaining.some((item) => item.id === previousSelectedID)) {
        rememberSelected(sessionID, replacement?.id);
      }
      if (active && !replacement) {
        onDeactivate();
      }
      return { previous, previousSelectedID };
    },
    onSuccess: (_data, terminalID) => {
      const nextDimensions = { ...initialDimensionsRef.current };
      delete nextDimensions[terminalID];
      initialDimensionsRef.current = nextDimensions;
      void queryClient.invalidateQueries({ queryKey: queryKeys.terminals(sessionID) });
    },
    onError: (_error, _terminalID, context) => {
      if (context?.previous) {
        queryClient.setQueryData(queryKeys.terminals(sessionID), context.previous);
      }
      rememberSelected(sessionID, context?.previousSelectedID);
      if (active) {
        onActivate();
      }
      toast.error(t("terminal.closeFailed"));
    },
  });

  const updateStatus = useCallback((terminalID: string, status: Terminal["status"], exitCode?: number) => {
    queryClient.setQueryData<TerminalData>(queryKeys.terminals(sessionID), (current) => ({
      terminals: (current?.terminals || []).map((item) =>
        item.id === terminalID ? { ...item, status, exitCode, updatedAt: new Date().toISOString() } : item,
      ),
    }));
  }, [queryClient, sessionID]);

  const closeTerminal = useCallback((terminalID: string) => {
    closeMutation.mutate(terminalID);
  }, [closeMutation.mutate]);
  const createNewTerminal = useCallback(() => {
    if (enabled && sessionID && !createMutation.isPending) {
      createMutation.mutate(undefined);
    }
  }, [createMutation.isPending, createMutation.mutate, enabled, sessionID]);
  const createNewTerminalAt = useCallback((cwd: string) => {
    if (enabled && sessionID && cwd && !createMutation.isPending) {
      createMutation.mutate(cwd);
    }
  }, [createMutation.isPending, createMutation.mutate, enabled, sessionID]);
  const selectTerminal = useCallback((terminalID: string) => {
    if (terminals.some((item) => item.id === terminalID)) {
      rememberSelected(sessionID, terminalID);
      onActivate();
    }
  }, [onActivate, rememberSelected, sessionID, terminals]);

  return {
    activeTerminal,
    activeTerminalID: activeTerminal?.id,
    closeTerminal,
    closingTerminalID: closeMutation.isPending ? closeMutation.variables : undefined,
    createNewTerminal,
    createNewTerminalAt,
    creatingTerminal: createMutation.isPending,
    selectTerminal,
    terminals,
    terminalInitialDimensions: initialDimensionsRef.current,
    terminalsPending: query.isFetching || createMutation.isPending,
    updateTerminalStatus: updateStatus,
  };
}

function upsertTerminal(items: Terminal[], next: Terminal) {
  const index = items.findIndex((item) => item.id === next.id);
  if (index < 0) {
    return [...items, next];
  }
  const updated = [...items];
  updated[index] = next;
  return updated;
}

function readSelectedTerminals(): Record<string, string> {
  if (typeof window === "undefined") {
    return {};
  }
  try {
    const parsed = JSON.parse(window.localStorage.getItem(SELECTED_TERMINAL_STORAGE_KEY) || "{}") as Record<string, unknown>;
    return Object.fromEntries(Object.entries(parsed).filter((entry): entry is [string, string] => typeof entry[1] === "string"));
  } catch {
    return {};
  }
}

function writeSelectedTerminals(value: Record<string, string>) {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.setItem(SELECTED_TERMINAL_STORAGE_KEY, JSON.stringify(value));
  } catch {
    // Best-effort UI preference.
  }
}
