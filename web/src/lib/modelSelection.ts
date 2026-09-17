import type { ProviderModel, ProviderProfile } from "@/api/client";

export type ModelSelectionValue = { provider?: string; model?: string };

export type ResolvedModelSelection = {
  provider: string;
  model: string;
  providerBrand?: string;
  providerProtocol?: ProviderProfile["protocol"];
  modelConfig?: ProviderModel;
};

export function modelSelectionKey(value: ModelSelectionValue) {
  return value.provider && value.model ? `${value.provider}:${value.model}` : "";
}

export function resolveModelSelection(
  profiles: ProviderProfile[],
  value: ModelSelectionValue,
): ResolvedModelSelection | null {
  if (!value.provider || !value.model) return null;
  const profile = profiles.find((item) => item.id === value.provider);
  const model = profile?.models.find((item) => item.id === value.model);
  if (!profile || !model) {
    return null;
  }
  return {
    provider: profile.id,
    model: model.id,
    providerBrand: profile.brand,
    providerProtocol: profile.protocol,
    modelConfig: model,
  };
}
