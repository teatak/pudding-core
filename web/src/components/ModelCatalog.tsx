import { useState } from "react";

import { type ProviderProfile } from "@/api/client";
import {
  appPopoverItemStateClassName,
  appPopoverSelectedItemStateClassName,
} from "@/components/AppPopover";
import { BrandIcon } from "@/components/BrandIcons";
import { Check } from "@/components/icons";
import { useI18n } from "@/i18n";
import { formatModelLabel } from "@/lib/model";
import { cn } from "@/lib/utils";

type ModelCatalogProps = {
  profiles: ProviderProfile[];
  selectedProvider: string;
  selectedModel: string;
  onSelect: (selection: { provider: string; model: string }) => void;
  disabled?: boolean;
  isLoading?: boolean;
};

export function ModelCatalog({
  profiles,
  selectedProvider,
  selectedModel,
  onSelect,
  disabled = false,
  isLoading = false,
}: ModelCatalogProps) {
  const { t } = useI18n();
  const [viewedProfileID, setViewedProfileID] = useState(selectedProvider);
  const viewedProfile = profiles.find((profile) => profile.id === viewedProfileID) ?? profiles[0];
  const currentProfile = profiles.find(
    (profile) => profile.id === selectedProvider && profile.models.some((model) => model.id === selectedModel),
  );
  const longestModelList = profiles.reduce(
    (longest, profile) => Math.max(longest, profile.models.filter((model) => model.id).length),
    0,
  );
  const paneHeight = Math.min(Math.max(Math.max(profiles.length, longestModelList) * 34 + 12, 160), 360);

  if (isLoading || !viewedProfile) {
    return (
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto p-1.5">
        <div className="px-2.5 py-1.5 text-xs text-muted-foreground">
          {t(isLoading ? "common.loading" : "picker.noModels")}
        </div>
      </div>
    );
  }

  return (
    <div
      className="flex min-h-0 flex-1 flex-col overflow-hidden"
      style={{ height: paneHeight, flexBasis: paneHeight }}
    >
      {profiles.length === 1 ? (
        <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-1.5 [scrollbar-gutter:stable]">
          <ProfileModels
            profile={viewedProfile}
            selectedModel={selectedProvider === viewedProfile.id ? selectedModel : ""}
            disabled={disabled}
            onPick={(model) => onSelect({ provider: viewedProfile.id, model })}
          />
        </div>
      ) : (
        <div className="grid min-h-0 flex-1 grid-cols-[8rem_minmax(0,1fr)] grid-rows-[minmax(0,1fr)] overflow-hidden">
          <div className="min-h-0 overflow-y-auto overscroll-contain border-r border-border/70 p-1.5">
            <div className="grid gap-0.5">
              {profiles.map((profile) => {
                const viewed = viewedProfile.id === profile.id;
                return (
                  <button
                    key={profile.id}
                    aria-current={viewed ? "true" : undefined}
                    className={cn(
                      "flex h-8 w-full min-w-0 cursor-default items-center gap-2 rounded-md px-2 text-left text-xs text-muted-foreground hover:text-foreground",
                      appPopoverItemStateClassName,
                      viewed && cn(appPopoverSelectedItemStateClassName, "text-foreground"),
                    )}
                    type="button"
                    onClick={() => setViewedProfileID(profile.id)}
                  >
                    <BrandIcon
                      className="size-5 shrink-0"
                      name={profile.brand || profile.displayName || profile.id}
                      shape="circle"
                    />
                    <span className="min-w-0 flex-1 truncate">{profile.displayName}</span>
                    {currentProfile?.id === profile.id ? (
                      <span className="size-1.5 shrink-0 rounded-full bg-success" />
                    ) : null}
                  </button>
                );
              })}
            </div>
          </div>
          <div className="min-h-0 overflow-y-auto overscroll-contain p-1.5 [scrollbar-gutter:stable]">
            <ProfileModels
              profile={viewedProfile}
              selectedModel={selectedProvider === viewedProfile.id ? selectedModel : ""}
              disabled={disabled}
              onPick={(model) => onSelect({ provider: viewedProfile.id, model })}
            />
          </div>
        </div>
      )}
    </div>
  );
}

function ProfileModels({
  profile,
  selectedModel,
  disabled,
  onPick,
}: {
  profile: ProviderProfile;
  selectedModel: string;
  disabled: boolean;
  onPick: (model: string) => void;
}) {
  return (
    <div className="grid gap-0.5">
      {profile.models.filter((model) => model.id).map((model) => (
        <button
          key={model.id}
          aria-current={selectedModel === model.id ? "true" : undefined}
          className={cn(
            "flex h-8 w-full min-w-0 cursor-default items-center gap-2 overflow-hidden rounded-md px-2 text-left text-[13px] disabled:pointer-events-none disabled:opacity-50",
            appPopoverItemStateClassName,
          )}
          disabled={disabled}
          type="button"
          onClick={() => onPick(model.id)}
        >
          <span className="min-w-0 flex-1 truncate">{formatModelLabel(model.id)}</span>
          {selectedModel === model.id ? <Check className="size-4 shrink-0" /> : null}
        </button>
      ))}
    </div>
  );
}
