import { useTheme } from "next-themes";
import ReactDiffViewer, { DiffMethod } from "react-diff-viewer-continued";

export function TextDiffViewer({ newValue, oldValue }: { newValue: string; oldValue: string }) {
  const { resolvedTheme } = useTheme();
  return (
    <div className="min-w-[520px] text-[11px]">
      <ReactDiffViewer
        oldValue={oldValue}
        newValue={newValue}
        splitView={false}
        compareMethod={DiffMethod.WORDS_WITH_SPACE}
        showDiffOnly={false}
        hideSummary
        useDarkTheme={resolvedTheme === "dark"}
        disableWorker
        styles={diffViewerStyles}
      />
    </div>
  );
}

const diffViewerStyles = {
  variables: {
    light: {
      diffViewerBackground: "var(--background)", diffViewerColor: "var(--foreground)", diffViewerTitleBackground: "var(--muted)",
      diffViewerTitleColor: "var(--muted-foreground)", diffViewerTitleBorderColor: "var(--border)", gutterBackground: "var(--muted)",
      gutterColor: "var(--muted-foreground)", addedBackground: "rgba(16, 185, 129, 0.12)", addedColor: "var(--foreground)",
      removedBackground: "rgba(239, 68, 68, 0.12)", removedColor: "var(--foreground)", wordAddedBackground: "rgba(16, 185, 129, 0.24)",
      wordRemovedBackground: "rgba(239, 68, 68, 0.24)",
    },
    dark: {
      diffViewerBackground: "var(--background)", diffViewerColor: "var(--foreground)", diffViewerTitleBackground: "var(--muted)",
      diffViewerTitleColor: "var(--muted-foreground)", diffViewerTitleBorderColor: "var(--border)", gutterBackground: "var(--muted)",
      gutterColor: "var(--muted-foreground)", addedBackground: "rgba(16, 185, 129, 0.18)", addedColor: "var(--foreground)",
      removedBackground: "rgba(239, 68, 68, 0.18)", removedColor: "var(--foreground)", wordAddedBackground: "rgba(16, 185, 129, 0.32)",
      wordRemovedBackground: "rgba(239, 68, 68, 0.32)",
    },
  },
  diffContainer: { borderRadius: 0, fontSize: "11px" },
  contentText: { fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace", lineHeight: "20px" },
  lineNumber: { fontSize: "11px" },
  marker: { fontSize: "11px" },
};
