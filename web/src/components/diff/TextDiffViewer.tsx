import ReactDiffViewer, { DiffMethod } from "react-diff-viewer-continued";
import { useMemo } from "react";

import { useEditorTypography } from "@/components/EditorTypographyProvider";
import { useTheme } from "@/theme/theme";

export function TextDiffViewer({ newValue, oldValue }: { newValue: string; oldValue: string }) {
  const { resolved } = useTheme();
  const typography = useEditorTypography();
  const styles = useMemo(
    () => diffViewerStyles(typography.fontFamily, typography.fontSize, typography.resolvedLineHeight),
    [typography.fontFamily, typography.fontSize, typography.resolvedLineHeight],
  );
  return (
    <div
      className="min-w-[520px]"
      style={{
        fontFamily: typography.fontFamily,
        fontSize: typography.fontSize,
        lineHeight: `${typography.resolvedLineHeight}px`,
      }}
    >
      <ReactDiffViewer
        oldValue={oldValue}
        newValue={newValue}
        splitView={false}
        compareMethod={DiffMethod.WORDS_WITH_SPACE}
        showDiffOnly={true}
        hideSummary
        useDarkTheme={resolved === "dark"}
        disableWorker
        renderContent={renderDiffContent}
        styles={styles}
      />
    </div>
  );
}

function diffViewerStyles(fontFamily: string, fontSize: number, lineHeight: number) {
  return {
    variables: {
      light: {
        diffViewerBackground: "var(--background)", diffViewerColor: "var(--foreground)", diffViewerTitleBackground: "var(--muted)",
        diffViewerTitleColor: "var(--muted-foreground)", diffViewerTitleBorderColor: "var(--border)", gutterBackground: "var(--muted)",
        gutterColor: "var(--muted-foreground)", addedBackground: "rgba(16, 185, 129, 0.12)", addedColor: "var(--foreground)",
        removedBackground: "rgba(239, 68, 68, 0.12)", removedColor: "var(--foreground)", wordAddedBackground: "rgba(16, 185, 129, 0.24)",
        wordRemovedBackground: "rgba(239, 68, 68, 0.24)", emptyLineBackground: "var(--background)",
      },
      dark: {
        diffViewerBackground: "var(--background)", diffViewerColor: "var(--foreground)", diffViewerTitleBackground: "var(--muted)",
        diffViewerTitleColor: "var(--muted-foreground)", diffViewerTitleBorderColor: "var(--border)", gutterBackground: "var(--muted)",
        gutterColor: "var(--muted-foreground)", addedBackground: "rgba(16, 185, 129, 0.18)", addedColor: "var(--foreground)",
        removedBackground: "rgba(239, 68, 68, 0.18)", removedColor: "var(--foreground)", wordAddedBackground: "rgba(16, 185, 129, 0.32)",
        wordRemovedBackground: "rgba(239, 68, 68, 0.32)", emptyLineBackground: "var(--background)",
      },
    },
    diffContainer: { borderRadius: 0, fontSize: `${fontSize}px` },
    contentText: { fontFamily, lineHeight: `${lineHeight}px` },
    lineNumber: { fontSize: `${fontSize}px` },
    marker: { fontSize: `${fontSize}px` },
  };
}

function renderDiffContent(content: string) {
  return (
    <span aria-hidden={content ? undefined : true} className={content ? undefined : "select-none"}>
      {content || " "}
    </span>
  );
}
