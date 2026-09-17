import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

import { listProviders } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { resolveModelSelection } from "@/lib/modelSelection";

export function useResolvedModelSelection(
  token: string,
  value: { provider?: string; model?: string },
) {
  const providersQuery = useQuery({
    queryKey: queryKeys.providers(),
    queryFn: () => listProviders(token),
    enabled: Boolean(token),
  });
  return useMemo(
    () => resolveModelSelection(providersQuery.data?.providers ?? [], value),
    [providersQuery.data?.providers, value.provider, value.model],
  );
}
