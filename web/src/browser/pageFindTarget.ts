export type FindTarget = {
  id: string;
  open: () => void;
  close: () => void;
};

let activeBrowserPageFind: FindTarget | null = null;
let openFindTarget: FindTarget | null = null;
let activeFindRegion: "browser" | "conversation" = "conversation";

export function registerBrowserPageFind(target: FindTarget) {
  activeBrowserPageFind = target;
  return () => {
    if (activeBrowserPageFind !== target) return;
    activeBrowserPageFind = null;
    closeFind(target.id);
    activeFindRegion = "conversation";
  };
}

export function openActiveBrowserPageFind() {
  if (activeFindRegion !== "browser" || !activeBrowserPageFind) return false;
  openFind(activeBrowserPageFind);
  return true;
}

export function openFind(target: FindTarget) {
  if (openFindTarget?.id !== target.id) {
    const previous = openFindTarget;
    openFindTarget = null;
    previous?.close();
  }
  target.open();
  openFindTarget = target;
}

export function closeFind(targetID: string) {
  if (openFindTarget?.id !== targetID) return;
  closeOpenFind();
}

export function closeOpenFind() {
  if (!openFindTarget) return;
  const target = openFindTarget;
  openFindTarget = null;
  target.close();
}

export function activateBrowserPageFindRegion() {
  activeFindRegion = "browser";
}

export function activateConversationFindRegion() {
  activeFindRegion = "conversation";
}
