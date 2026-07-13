export type ElectronProjectFileChange = {
  eventType: "change" | "rename";
  id: string;
  path: string;
};

type ElectronProjectFileBridge = {
  watch: (request: { id: string; path: string }) => Promise<{ id: string; path: string }>;
  unwatch: (request: { id: string }) => Promise<boolean>;
  onChanged: (listener: (change: ElectronProjectFileChange) => void) => () => void;
};

declare global {
  interface Window {
    puddingElectronProjectFiles?: ElectronProjectFileBridge;
  }
}

let nextProjectFileSubscriptionID = 0;

export function watchElectronProjectFile(path: string, onChange: () => void, onReady?: () => void) {
  const bridge = typeof window === "undefined" ? undefined : window.puddingElectronProjectFiles;
  if (!bridge) {
    return () => undefined;
  }
  const id = `project-file-${++nextProjectFileSubscriptionID}`;
  let disposed = false;
  let watching = false;
  const stopListening = bridge.onChanged((change) => {
    if (change.id === id) {
      onChange();
    }
  });
  void bridge.watch({ id, path }).then(() => {
    watching = true;
    if (disposed) {
      void bridge.unwatch({ id }).catch(() => undefined);
      return;
    }
    onReady?.();
  }).catch(() => stopListening());

  return () => {
    disposed = true;
    stopListening();
    if (watching) {
      void bridge.unwatch({ id }).catch(() => undefined);
    }
  };
}
