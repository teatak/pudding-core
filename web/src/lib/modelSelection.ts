import type { ProviderModel, ProviderProfile } from "@/api/client";

export type ResolvedModelSelection = {
  provider: string;
  model: string;
  providerBrand?: string;
  providerProtocol?: ProviderProfile["protocol"];
  modelConfig?: ProviderModel;
};

export function resolveModelSelection(
  profiles: ProviderProfile[],
  value: { provider?: string; model?: string },
): ResolvedModelSelection | null {
  const profile = profiles.find((item) => item.id === value.provider);
  const modelConfig = profile?.models.find((item) => item.id === value.model);
  if (!profile || !modelConfig) {
    return null;
  }
  return {
    provider: profile.id,
    model: modelConfig.id,
    providerBrand: profile.brand,
    providerProtocol: profile.protocol,
    modelConfig,
  };
}

export function modelSelectionKey(selection: { provider: string; model: string }): string {
  return `${selection.provider}:${selection.model}`;
}
