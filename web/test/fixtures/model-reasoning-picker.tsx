import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import type { ProviderProfile } from "../../src/api/client";
import { ModelReasoningPicker } from "../../src/components/ModelReasoningPicker";
import { setLocale } from "../../src/i18n";
import { formatModelLabel } from "../../src/lib/model";
import "../../src/styles.css";

type Selection = { provider: string; model: string };
type FixtureState = {
  selection: Selection;
  modelLabel: string;
  reasoning: string;
  disabled: boolean;
  modelCommits: Selection[];
  reasoningCommits: string[];
};
declare global {
  interface Window {
    pickerFixture: {
      state: () => FixtureState;
      reset: (reasoning?: string) => void;
      setReasoning: (value: string) => void;
      setDisabled: (disabled: boolean) => void;
    };
  }
}

setLocale("en");

const profiles: ProviderProfile[] = [
  {
    id: "fixture-openai",
    displayName: "Fixture OpenAI",
    brand: "openai",
    protocol: "openai-responses",
    baseURL: "https://fixture.invalid",
    apiKeySet: false,
    models: Array.from({ length: 14 }, (_, index) => ({
      id: `openai-fixture-${String(index + 1).padStart(2, "0")}`,
      providerOptions: { openai: { reasoning_effort: "none" } },
    })),
  },
  {
    id: "fixture-anthropic",
    displayName: "Fixture Anthropic",
    brand: "anthropic",
    protocol: "anthropic",
    baseURL: "https://fixture.invalid",
    apiKeySet: false,
    models: Array.from({ length: 11 }, (_, index) => ({
      id: `anthropic-fixture-${String(index + 1).padStart(2, "0")}`,
      providerOptions: { anthropic: { output_config: { effort: "high" } } },
    })),
  },
];
const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: Infinity, gcTime: Infinity, retry: false, refetchOnWindowFocus: false },
  },
});
queryClient.setQueryData(["providers"], { providers: profiles });
const initialSelection: Selection = { provider: profiles[0].id, model: profiles[0].models[0].id };
const modelCommits: Selection[] = [];
const reasoningCommits: string[] = [];

function Fixture() {
  const [selection, setSelection] = useState(initialSelection);
  const [reasoning, setReasoning] = useState("");
  const [disabled, setDisabled] = useState(false);
  const [generation, setGeneration] = useState(0);
  const textarea = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    window.pickerFixture = {
      state: () => ({
        selection, reasoning, disabled,
        modelLabel: formatModelLabel(selection.model),
        modelCommits: [...modelCommits],
        reasoningCommits: [...reasoningCommits],
      }),
      reset(nextReasoning = "") {
        modelCommits.length = 0;
        reasoningCommits.length = 0;
        setSelection(initialSelection);
        setReasoning(nextReasoning);
        setDisabled(false);
        setGeneration((value) => value + 1);
      },
      setReasoning,
      setDisabled,
    };
  }, [selection, reasoning, disabled, generation]);

  return (
    <div style={{ position: "absolute", top: "50%", left: 80, display: "flex", alignItems: "center", gap: 24 }}>
      <ModelReasoningPicker
        key={generation}
        token="isolated-fixture"
        value={selection}
        reasoningValue={reasoning}
        disabled={disabled}
        onChange={(next) => {
          modelCommits.push(next);
          setSelection(next);
          setReasoning("");
        }}
        onReasoningChange={(next) => {
          reasoningCommits.push(next);
          setReasoning(next);
        }}
        onAfterClose={() => textarea.current?.focus()}
      />
      <textarea id="fixture-composer" ref={textarea} aria-label="Fixture composer" style={{ width: 240, height: 64 }} />
    </div>
  );
}

createRoot(document.getElementById("root")!).render(
  <QueryClientProvider client={queryClient}>
    <Fixture />
  </QueryClientProvider>,
);
