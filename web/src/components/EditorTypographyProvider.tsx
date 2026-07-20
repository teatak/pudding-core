import { useQuery } from "@tanstack/react-query";
import { createContext, useContext, useMemo, type ReactNode } from "react";

import { getSettings } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { editorTypographySettings, type EditorTypography } from "@/lib/appSettings";

const EditorTypographyContext = createContext<EditorTypography>(editorTypographySettings());

export function EditorTypographyProvider({ children, token }: { children: ReactNode; token: string }) {
  const settingsQuery = useQuery({
    queryKey: queryKeys.settings(),
    queryFn: () => getSettings(token),
    enabled: Boolean(token),
  });
  const typography = useMemo(
    () => editorTypographySettings(settingsQuery.data?.settings),
    [settingsQuery.data?.settings],
  );

  return <EditorTypographyContext.Provider value={typography}>{children}</EditorTypographyContext.Provider>;
}

export function useEditorTypography() {
  return useContext(EditorTypographyContext);
}
