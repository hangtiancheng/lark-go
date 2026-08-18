import { useSyncExternalStore } from "react";

export interface LocationSnapshot {
  pathname: string;
  search: string;
  hash: string;
  href: string;
}

function getSnapshot(): LocationSnapshot {
  const { pathname, search, hash, href } = window.location;
  return { pathname, search, hash, href };
}

let cached: LocationSnapshot = getSnapshot();

function getSnapshotCached(): LocationSnapshot {
  const { pathname, search, hash, href } = window.location;
  if (
    cached.pathname === pathname &&
    cached.search === search &&
    cached.hash === hash &&
    cached.href === href
  ) {
    return cached;
  }
  cached = { pathname, search, hash, href };
  return cached;
}

function subscribe(onChange: () => void): () => void {
  window.addEventListener("popstate", onChange);

  const originalPushState = history.pushState.bind(history);
  const originalReplaceState = history.replaceState.bind(history);

  history.pushState = (...args: Parameters<History["pushState"]>) => {
    originalPushState(...args);
    onChange();
  };
  history.replaceState = (...args: Parameters<History["replaceState"]>) => {
    originalReplaceState(...args);
    onChange();
  };

  return () => {
    window.removeEventListener("popstate", onChange);
    history.pushState = originalPushState;
    history.replaceState = originalReplaceState;
  };
}

export function useLocation(): LocationSnapshot {
  return useSyncExternalStore(subscribe, getSnapshotCached, getSnapshot);
}
