import { useEffect, useSyncExternalStore } from "react";

import { getEndpointURL } from "../api";
import { trackArtworkURL } from "../lib/library";

const bc = new BroadcastChannel("cache_done");

export function useOffline(): {
  cachedTrackIDs: string[];
  save(trackIDs: string[]): Promise<void>;
} {
  useSyncExternalStore(store.subscribe, store.getSnapshot);

  const update = () =>
    caches
      .open("v1")
      .then((cache) => cache.keys())
      .then((keys) => keys.filter((r) => r.method === "GET").map((r) => r.url))
      .then((urls) => urls.map((u) => new URL(u).pathname))
      .then((paths) => paths.filter((p) => p.startsWith("/api/track/")))
      .then((paths) => paths.map((p) => p.slice("/api/track/".length)))
      .then((ids) => {
        store.saved = ids;
      });

  if (!store.init) {
    // Android の Chrome でなぜか Cache API が遅くてロードが終わらないので、完了を待たない
    store.init = true;
    update().then(() => {
      store.dispath();
    });
  }

  useEffect(() => {
    const f = () => {
      update().then(() => {
        store.dispath();
      });
    };

    // 1度だけ受け取りたいので、addEventLister ではなく、onmessage に代入する
    bc.onmessage = f;
  }, []);

  return {
    get cachedTrackIDs() {
      return store.saved;
    },
    async save(trackIDs: string[]) {
      // fetch すれば Service Worker でキャッシュされる
      for (const id of trackIDs) {
        const u = getEndpointURL(`/api/track/${id}`);
        await fetch(u);
        const a = trackArtworkURL(id);
        await fetch(a);
      }
    },
  };
}

const store = {
  _listeners: [] as (() => unknown)[],
  init: false,
  saved: [] as string[],

  subscribe(listener: () => unknown) {
    store._listeners = [...store._listeners, listener];
    return () => {
      store._listeners = store._listeners.filter((l) => l != listener);
    };
  },
  getSnapshot() {
    return store.saved.toString();
  },

  dispath() {
    for (const l of store._listeners) {
      l();
    }
  },
};
