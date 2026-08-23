export type ElectronProjectFileChange = {
  eventType: "change" | "rename";
  id: string;
  path: string;
  changedPath?: string;
};

type ElectronProjectFileBridge = {
  watch: (request: { id: string; kind?: "file" | "directory"; path: string }) => Promise<{ id: string; path: string }>;
  unwatch: (request: { id: string }) => Promise<boolean>;
  onChanged: (listener: (change: ElectronProjectFileChange) => void) => () => void;
};

declare global {
  interface Window {
    puddingElectronProjectFiles?: ElectronProjectFileBridge;
  }
}

let nextProjectFileSubscriptionID = 0;

export function watchElectronProjectFile(path: string, onChange: () => void) {
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
  }).catch(() => stopListening());

  return () => {
    disposed = true;
    stopListening();
    if (watching) {
      void bridge.unwatch({ id }).catch(() => undefined);
    }
  };
}

export function watchElectronProjectDirectories(paths: string[], onChange: (change: ElectronProjectFileChange) => void) {
  const bridge = typeof window === "undefined" ? undefined : window.puddingElectronProjectFiles;
  const uniquePaths = Array.from(new Set(paths.filter(Boolean)));
  if (!bridge || uniquePaths.length === 0) {
    return () => undefined;
  }
  const subscriptions = uniquePaths.map((path) => ({
    id: `project-directory-${++nextProjectFileSubscriptionID}`,
    path,
  }));
  const ids = new Set(subscriptions.map((subscription) => subscription.id));
  let disposed = false;
  const watching = new Set<string>();
  const stopListening = bridge.onChanged((change) => {
    if (ids.has(change.id)) {
      onChange(change);
    }
  });
  subscriptions.forEach((subscription) => {
    void bridge.watch({ ...subscription, kind: "directory" }).then(() => {
      watching.add(subscription.id);
      if (disposed) {
        void bridge.unwatch({ id: subscription.id }).catch(() => undefined);
      }
    }).catch(() => undefined);
  });

  return () => {
    disposed = true;
    stopListening();
    watching.forEach((id) => void bridge.unwatch({ id }).catch(() => undefined));
  };
}
