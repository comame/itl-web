import React from "react";
import { createRoot } from "react-dom/client";
import {
  createBrowserRouter,
  createHashRouter,
  RouterProvider,
} from "react-router-dom";

import { useJSONAPI } from "./hook/useAPI";
import { getPlaylists, getTracks } from "./api";

import Index from "./index.page";
import Albums from "./albums.page";
import Playlists from "./playlists.page";
import Playlist from "./playlist.page";
import Album from "./album.page";
import { PlaylistsContext, TracksContext } from "./hook/useTracks";

import "@charcoal-ui/icons";

import "./index.css";
import Settings from "./settings.page";
import { ErrorBoundary, RouterErrorBoundary } from "./error_boundary";

declare global {
  export namespace JSX {
    interface IntrinsicElements {
      "pixiv-icon": {
        name: string;
        scale: 1 | 2 | 3 | "1" | "2" | "3";
        "unsafe-non-guideline-scale"?: number | string;
      };
    }
  }
}

function Page() {
  const router = createHashRouter([
    {
      path: "",
      element: <Index />,
      errorElement: <RouterErrorBoundary />,
      children: [
        {
          path: "",
          element: <Albums />,
        },
        {
          path: "playlists",
          element: <Playlists />,
        },
        {
          path: "playlist/:id",
          element: <Playlist />,
          loader: mapParamToLoader,
        },
        {
          path: "album/:id",
          element: <Album />,
          loader: mapParamToLoader,
        },
        {
          path: "settings",
          element: <Settings />,
        },
      ],
    },
  ]);

  const [tracks, errTracks] = useJSONAPI(getTracks);
  const [playlists, errPlaylists] = useJSONAPI(getPlaylists);

  if (errTracks || errPlaylists) {
    return <div>Failed to load library</div>;
  }

  return (
    <React.StrictMode>
      <React.Suspense fallback={<p>Loading...</p>}>
        <TracksContext.Provider value={tracks}>
          <PlaylistsContext.Provider value={playlists}>
            <RouterProvider router={router} />
          </PlaylistsContext.Provider>
        </TracksContext.Provider>
      </React.Suspense>
    </React.StrictMode>
  );
}

function mapParamToLoader({ params }: any): Record<string, string> {
  return params;
}

createRoot(document.getElementById("app")!).render(
  <ErrorBoundary>
    <Page />
  </ErrorBoundary>
);

if ("serviceWorker" in navigator) {
  navigator.serviceWorker.register("/sw.js", {
    scope: "/",
  });
}
