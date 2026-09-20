import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { createRoot } from "react-dom/client";

import { type AudioBindings, type AudioInputMode, type ProviderProfile, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ModelReasoningPicker } from "@/components/ModelReasoningPicker";
import { SessionAudioControls } from "@/components/SessionAudioControls";
import { SteppedSlider } from "@/components/SteppedSlider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { setLocale } from "@/i18n";
import { useSessionModelSettings } from "@/hooks/useSessionModelSettings";
import type { ResolvedModelSelection } from "@/lib/modelSelection";
import { useReasoningEffortPreferenceStore } from "@/state/reasoningEffortPreferenceStore";
import { applyTheme } from "@/theme/theme";
import "@/styles.css";

const mode = new URLSearchParams(window.location.search).get("mode") || "draft";
const providers: ProviderProfile[] = [
  {
    id: "anthropic-smoke",
    displayName: "Anthropic smoke",
    protocol: "anthropic",
    brand: "anthropic",
    apiKeySet: false,
    baseURL: "https://example.invalid",
    models: [
      { id: "claude-opus-4-6", displayName: "Opus smoke" },
      { id: "claude-haiku-4-5", displayName: "Haiku smoke" },
    ],
  },
  {
    id: "google-smoke",
    displayName: "Google smoke",
    protocol: "google",
    brand: mode === "sync" ? "openrouter" : "google",
    apiKeySet: false,
    baseURL: "https://example.invalid",
    models: [{ id: "gemini-3.1-pro-preview", displayName: "Gemini smoke" }],
  },
];

const initialModel = {
  provider: "anthropic-smoke",
  model: mode === "haiku" ? "claude-haiku-4-5" : "claude-opus-4-6",
};
const originalKey = "anthropic-smoke:claude-opus-4-6";
const targetKey = "google-smoke:gemini-3.1-pro-preview";
const initialSession: Session = {
  id: "reasoning-smoke",
  title: "Reasoning smoke",
  ...initialModel,
  reasoningEffort: "max",
  reasoningModelKey: originalKey,
  activeMode: "chat",
  modeLease: "none",
  pinned: false,
  pinnedOrder: 0,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  lastActivityAt: "2026-01-01T00:00:00Z",
  running: false,
  backgroundProcessCount: 0,
};
const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: Infinity, retry: false }, mutations: { retry: false } },
});
queryClient.setQueryData(queryKeys.providers(), { providers });
queryClient.setQueryData(queryKeys.sessions(), { sessions: [initialSession] });
queryClient.setQueryData(queryKeys.session(initialSession.id), initialSession);
const initialAudioBindings: AudioBindings = {
  inputOwner: mode === "audio-active" ? initialSession.id : "",
  inputMode: mode === "audio-active" ? "transcribe" : "",
  inputLevel: 0,
};
queryClient.setQueryData(queryKeys.audioBindings(), { bindings: initialAudioBindings });
setLocale("en");
useReasoningEffortPreferenceStore.setState({ byModel: { [originalKey]: "max", [targetKey]: "low" } });

type Patch = { provider?: string; model?: string; reasoningEffort?: string };
const patches: Patch[] = [];
const reasoningChanges: { modelKey: string; effort: string }[] = [];
const audioRequests: { enabled: boolean; mode?: AudioInputMode }[] = [];
let serverSession = initialSession;
let serverAudioBindings = initialAudioBindings;
let holdResponses = false;
let failNextResponse = false;
let lastError = "";
const heldResponses: (() => void)[] = [];
const heldSyncResponses: (() => void)[] = [];
let syncRequests = 0;
let serverProviders = providers;

// Intercept only the fixture API. The picker still uses the real API payload
// schema, mutation, Query cache and preference store.
window.fetch = async (input, init) => {
  const url = new URL(input instanceof Request ? input.url : String(input), window.location.href);
  if (url.pathname === "/providers/google-smoke/sync" && init?.method === "POST") {
    const count = ++syncRequests;
    await new Promise<void>((resolve) => heldSyncResponses.push(resolve));
    serverProviders = serverProviders.map((profile) => profile.id === "google-smoke"
      ? { ...profile, models: profile.models.map((model) => ({ ...model, costMultiplier: count / 10 })) }
      : profile);
    return new Response(JSON.stringify(serverProviders.find((profile) => profile.id === "google-smoke")));
  }
  if (url.pathname === "/providers") {
    return new Response(JSON.stringify({ providers: serverProviders }));
  }
  if (url.pathname === `/sessions/${initialSession.id}` && init?.method === "PATCH") {
    const patch = JSON.parse(String(init.body)) as Patch;
    patches.push(patch);
    const shouldFail = failNextResponse;
    failNextResponse = false;
    if (holdResponses) {
      await new Promise<void>((resolve) => heldResponses.push(resolve));
    }
    if (shouldFail) {
      return new Response(JSON.stringify({ error: "smoke-failure" }), { status: 500 });
    }
    serverSession = {
      ...serverSession,
      ...patch,
      reasoningModelKey: patch.reasoningEffort
        ? `${patch.provider || serverSession.provider}:${patch.model || serverSession.model}`
        : "",
    };
    return new Response(JSON.stringify(serverSession), { headers: { "Content-Type": "application/json" } });
  }
  if (url.pathname === `/sessions/${initialSession.id}/audio/input` && init?.method === "POST") {
    const body = JSON.parse(String(init.body)) as { enabled: boolean; mode?: AudioInputMode };
    audioRequests.push(body);
    serverAudioBindings = {
      inputOwner: body.enabled ? initialSession.id : "",
      inputMode: body.enabled ? body.mode || "transcribe" : "",
      inputLevel: 0,
    };
    return new Response(JSON.stringify({ ok: true, bindings: serverAudioBindings }), {
      headers: { "Content-Type": "application/json" },
    });
  }
  throw new Error(`Unexpected fixture request: ${url.pathname}`);
};

type Snapshot = {
  selection: { provider: string; model: string };
  preferences: Record<string, string>;
  patches: Patch[];
  reasoningChanges: { modelKey: string; effort: string }[];
  pending: boolean;
  heldResponses: number;
  lastError: string;
  session: Session;
  audioRequests: { enabled: boolean; mode?: AudioInputMode }[];
  audioBindings: AudioBindings;
  syncRequests: number;
  providers: ProviderProfile[];
};
declare global {
  interface Window {
    modelReasoningSmoke: {
      snapshot: () => Snapshot;
      respond: (options: { hold?: boolean; failNext?: boolean }) => void;
      release: () => void;
      releaseSync: () => void;
      removeProfile: (id: string) => Promise<void>;
      setTheme: (theme: "light" | "dark") => void;
      update: (patch: { provider: string; model: string; reasoningEffort: string }) => Promise<Session | null>;
    };
  }
}

function Fixture() {
  const [draftModel, setDraftModel] = useState(initialModel);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const sessionQuery = useQuery<Session>({
    queryKey: queryKeys.session(initialSession.id),
    queryFn: async () => serverSession,
    enabled: false,
  });
  const settings = useSessionModelSettings("fixture-token", initialSession.id);
  const audioBindingsQuery = useQuery<{ bindings: AudioBindings }>({
    queryKey: queryKeys.audioBindings(),
    queryFn: async () => ({ bindings: serverAudioBindings }),
    enabled: false,
  });
  const update = (patch: { provider: string; model: string; reasoningEffort: string }) =>
    settings.update(patch).catch((error) => {
      lastError = String(error);
      return null;
    });
  const currentSession = sessionQuery.data || initialSession;
  const currentModel = mode === "session" ? currentSession : draftModel;
  const modelKey = `${currentModel.provider}:${currentModel.model}`;
  const preferences = useReasoningEffortPreferenceStore((state) => state.byModel);
  const reasoningValue = mode === "session"
    ? currentSession.reasoningModelKey === modelKey ? currentSession.reasoningEffort || "" : ""
    : preferences[modelKey] || "";

  window.modelReasoningSmoke = {
    snapshot: () => ({
      selection: { provider: currentModel.provider, model: currentModel.model },
      preferences: useReasoningEffortPreferenceStore.getState().byModel,
      patches,
      reasoningChanges,
      pending: settings.pending,
      heldResponses: heldResponses.length,
      lastError,
      session: queryClient.getQueryData<Session>(queryKeys.session(initialSession.id))!,
      audioRequests,
      audioBindings: queryClient.getQueryData<{ bindings: AudioBindings }>(queryKeys.audioBindings())!.bindings,
      syncRequests,
      providers: queryClient.getQueryData<{ providers: ProviderProfile[] }>(queryKeys.providers())!.providers,
    }),
    respond: (options) => {
      holdResponses = options.hold || false;
      failNextResponse = options.failNext || false;
    },
    release: () => heldResponses.shift()?.(),
    releaseSync: () => heldSyncResponses.shift()?.(),
    removeProfile: async (id) => {
      serverProviders = serverProviders.filter((profile) => profile.id !== id);
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
    },
    update,
    setTheme: applyTheme,
  };

  const onReasoningChange = (selection: ResolvedModelSelection, effort: string) => {
    const selectedKey = `${selection.provider}:${selection.model}`;
    reasoningChanges.push({ modelKey: selectedKey, effort });
    if (mode === "session") {
      void update({
        provider: selection.provider,
        model: selection.model,
        reasoningEffort: effort,
      });
    } else {
      useReasoningEffortPreferenceStore.getState().setForModel(selectedKey, effort);
    }
  };

  return (
    <>
      {mode === "palette" ? (
        <div className="grid grid-cols-3 gap-5 p-6">
          {[
            ["low", "medium", "high"],
            ["low", "medium", "high", "max"],
            ["low", "medium", "high", "xhigh", "max"],
          ].map((options) => (
            <section key={options.length} className="space-y-3">
              <h2>{options.length} levels</h2>
              {options.map((value) => (
                <div key={value} data-effort-example={`${options.length}-${value}`} className="rounded-xl border bg-popover p-4">
                  <SteppedSlider options={options} value={value} onChange={() => {}} />
                </div>
              ))}
            </section>
          ))}
        </div>
      ) : null}
      <div style={{ display: "flex", gap: 48, padding: 24, paddingTop: mode === "palette" ? 0 : 360 }}>
        <div style={{ width: 340 }}>
          <textarea id="composer-a" ref={textareaRef} aria-label="Composer A" style={{ border: "1px solid", width: "100%" }} />
          <ModelReasoningPicker
            token="fixture-token"
            session={mode === "session" ? currentSession : undefined}
            value={mode === "session" ? undefined : draftModel}
            reasoningValue={reasoningValue}
            onChange={setDraftModel}
            onReasoningChange={onReasoningChange}
            onAfterClose={() => requestAnimationFrame(() => textareaRef.current?.focus())}
          />
          {mode.startsWith("audio-") ? (
            <div id="audio-controls">
              <SessionAudioControls
                audioInputSupported={false}
                inputDisabled
                bindings={audioBindingsQuery.data?.bindings}
                token="fixture-token"
                sessionID={initialSession.id}
              />
            </div>
          ) : null}
        </div>
        <textarea id="composer-b" aria-label="Composer B" style={{ border: "1px solid", width: 260, height: 72 }} />
      </div>
    </>
  );
}

createRoot(document.getElementById("root")!).render(
  <QueryClientProvider client={queryClient}>
    <TooltipProvider>
      <Fixture />
    </TooltipProvider>
  </QueryClientProvider>,
);
