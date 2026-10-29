// @vitest-environment happy-dom
import { cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

function delay(ms: number) {
  const { promise, resolve } = Promise.withResolvers<void>();
  setTimeout(resolve, ms);
  return promise;
}
import { Player } from "./Player";
import type * as ApiModule from "./api";
import type { Anime, Episode, PlaybackSession, Provider, Settings } from "./api";

const { apiMock, hlsState } = vi.hoisted(() => ({
  apiMock: vi.fn(),
  hlsState: {
    supported: true,
    // One entry per constructed engine so tests can fire engine events.
    handlers: [] as { event: string; cb: (...args: unknown[]) => void }[],
  },
}));

vi.mock("./api", async (importOriginal) => {
  const original = await importOriginal<typeof ApiModule>();
  return { ...original, api: (...args: unknown[]) => apiMock(...args) };
});

vi.mock("hls.js", () => {
  class Hls {
    static isSupported() {
      return hlsState.supported;
    }
    static Events = { MANIFEST_PARSED: "hlsManifestParsed", AUDIO_TRACKS_UPDATED: "hlsAudioTracksUpdated", ERROR: "hlsError" };
    levels: unknown[] = [];
    currentLevel = -1;
    audioTracks: unknown[] = [];
    audioTrack = -1;
    on(event: string, cb: (...args: unknown[]) => void) {
      hlsState.handlers.push({ event, cb });
    }
    loadSource() {}
    attachMedia() {}
    startLoad() {}
    destroy() {}
  }
  return { default: Hls };
});

const providers: Provider[] = [
  { id: "hianime", enabled: true, modes: ["sub"], status: "unverified" },
  { id: "otakudesu", enabled: true, modes: ["sub"], status: "unverified" },
];

const settings: Settings = {
  subtitleLanguage: "off",
  quality: "auto",
  autoplay: false,
  preferredProvider: "auto",
  revision: 0,
};

const anime: Anime = { id: "anilist:1", title: "Test Anime" } as Anime;
// The episode lists only hianime, so otakudesu is "not offered" by the data.
const episode: Episode = {
  id: "anilist:1:episode:1",
  animeId: "anilist:1",
  label: "1",
  ordinal: 1,
  availability: ["hianime"],
} as Episode;

function session(id: string): PlaybackSession {
  return {
    id,
    masterUrl: `/media/${id}/master.m3u8`,
    actualProvider: "otakudesu",
    actualMode: "sub",
    variants: [],
    subtitles: [],
    expiresAt: "2030-01-01T00:00:00Z",
  } as unknown as PlaybackSession;
}

function installApi(records: { method: string; path: string; body?: unknown }[]) {
  apiMock.mockImplementation((path: string, init?: { method?: string; body?: unknown }) => {
    const method = init?.method ?? "GET";
    records.push({ method, path, body: init?.body });
    if (path.startsWith("/progress/") && method === "GET") return Promise.resolve(null);
    if (path === "/playback/sessions" && method === "POST") {
      return Promise.resolve(session(`s${records.length}`));
    }
    return Promise.resolve(null);
  });
}

function renderPlayer(onProviderChange = vi.fn()) {
  return render(
    <Player
      anime={anime}
      episode={episode}
      episodes={[episode]}
      settings={settings}
      provider="otakudesu"
      mode="sub"
      providers={providers}
      sessionSpeed={1}
      onSpeedChange={() => {}}
      onProviderChange={onProviderChange}
      onEpisode={() => {}}
      onClose={() => {}}
    />,
  );
}

afterEach(() => {
  cleanup();
  apiMock.mockReset();
  hlsState.handlers = [];
  hlsState.supported = true;
});

describe("explicit provider selection", () => {
  it("keeps the chosen provider instead of silently reverting to auto", async () => {
    const records: { method: string; path: string; body?: unknown }[] = [];
    installApi(records);
    const onProviderChange = vi.fn();
    renderPlayer(onProviderChange);

    await waitFor(() => {
      expect(records.some((r) => r.path === "/playback/sessions")).toBe(true);
    });
    // The request names the chosen source even though the episode inventory
    // does not list it; the failure path tells the viewer to pick another.
    const post = records.find((r) => r.path === "/playback/sessions");
    const body = typeof post?.body === "string" ? JSON.parse(post.body) : post?.body;
    expect((body as { provider?: string } | undefined)?.provider).toBe("otakudesu");
    expect(onProviderChange).not.toHaveBeenCalled();
  });
});

describe("automatic recovery while paused", () => {
  it("does not restart a paused player when the engine reports a fatal error", async () => {
    const records: { method: string; path: string; body?: unknown }[] = [];
    installApi(records);
    renderPlayer();

    await waitFor(() => {
      expect(hlsState.handlers.some((h) => h.event === "hlsError")).toBe(true);
    });
    const before = records.filter((r) => r.path === "/playback/sessions").length;

    const video = document.querySelector("video") as HTMLVideoElement;
    Object.defineProperty(video, "paused", { configurable: true, value: true });
    const errorHandler = hlsState.handlers.filter((h) => h.event === "hlsError").at(-1);
    errorHandler?.cb({}, { fatal: true, details: "manifestLoadError" });

    // No new resolve and no silent reload: the player shows a recoverable
    // failure card instead.
    await delay(30);
    expect(records.filter((r) => r.path === "/playback/sessions").length).toBe(before);
  });

  it("still recovers once while the viewer is actually watching", async () => {
    const records: { method: string; path: string; body?: unknown }[] = [];
    installApi(records);
    renderPlayer();

    await waitFor(() => {
      expect(hlsState.handlers.some((h) => h.event === "hlsError")).toBe(true);
    });
    const before = records.filter((r) => r.path === "/playback/sessions").length;

    const video = document.querySelector("video") as HTMLVideoElement;
    Object.defineProperty(video, "paused", { configurable: true, value: false });
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
    const errorHandler = hlsState.handlers.filter((h) => h.event === "hlsError").at(-1);
    errorHandler?.cb({}, { fatal: true, details: "fragLoadError" });

    await waitFor(() => {
      expect(records.filter((r) => r.path === "/playback/sessions").length).toBe(before + 1);
    });
  });
});
