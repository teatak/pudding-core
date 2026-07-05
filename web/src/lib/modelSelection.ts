import type { ProviderModel, ProviderProfile } from "@/api/client";

export type ResolvedModelSelection = {
  provider: string;
  model: string;
  providerBrand?: string;
  providerProtocol?: ProviderProfile["protocol"];
  modelConfig?: ProviderModel;
};
