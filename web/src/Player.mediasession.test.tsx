// @vitest-environment happy-dom
//
// OS media keys / now-playing metadata (issue #13 [3.2]).
//
// happy-dom has no navigator.mediaSession and no MediaMetadata constructor,
// which mirrors WKWebView: the "absent" cases below run against the untouched
// host. The "supported" cases install a stubbed navigator. Real WKWebView
// behaviour cannot be exercised here — see the platform-limit note in the
// issue report.
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Player } from "./Player";
import { mediaURL } from "./api";
import type * as ApiModule from "./api";
import type { Anime, Episode, PlaybackSession, Provider, Settings } from "./api";

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));

vi.mock("./api", async (importOriginal) => {
  const original = await importOriginal<typeof ApiModule>();
  return { ...original, api: (...args: unknown[]) => apiMock(...args) };
});

vi.mock("hls.js", () => {
  class Hls {
    static isSupported() {
      return false;
    }
    on() {}
    loadSource() {}
    attachMedia() {}
    destroy() {}
  }
  return { default: Hls };
});

const providers: Provider[] = [
  { id: "hianime", enabled: true, modes: ["sub", "dub"], status: "unknown", capabilities: { kinds: ["anime"], audioModes: ["sub", "dub"], readerFormats: [] } },
];

const settings: Settings = { subtitleLanguage: "off", quality: "auto", autoplay: false, preferredProvider: "auto", revision: 0 };
const anime: Anime = { id: "anilist:1", title: "Test Anime", posterUrl: "/posters/1.jpg" } as Anime;
const episode: Episode = { id: "anilist:1:episode:MQ", animeId: "anilist:1", label: "1", ordinal: 1, availability: ["hianime"], title: "Episode 1 The Beginning" } as Episode;

function session(): PlaybackSession {
  return { id: "sess-1", masterUrl: "/media/t/1", actualProvider: "hianime", actualMode: "sub", variants: [], subtitles: [], expiresAt: "2026-01-01T00:00:00Z" } as unknown as PlaybackSession;
}

/* happy-dom cannot parse MIME types, so the native-HLS branch would refuse the
   source. Accepting it keeps the element usable for event-driven tests. */
const _canPlayType = HTMLMediaElement.prototype.canPlayType;
HTMLMediaElement.prototype.canPlayType = () => "maybe";

type ActionHandler = ((details?: { seekTime?: number; fastSeek?: boolean }) => void) | null;

class FakeMediaMetadata {
  readonly init: MediaMetadataInit;
  constructor(init: MediaMetadataInit) {
    this.init = init;
  }
}

function installMediaSession() {
  const handlers: Record<string, ActionHandler> = {};
  const actions: string[] = [];
  const stub = {
    metadata: null as FakeMediaMetadata | null,
    playbackState: "none" as string,
    setActionHandler(action: string, handler: ActionHandler) {
      actions.push(action);
      handlers[action] = handler;
    },
    setPositionState: vi.fn(),
  };
  vi.stubGlobal("MediaMetadata", FakeMediaMetadata);
  const base = Object.create(navigator);
  Object.defineProperty(base, "mediaSession", { value: stub, configurable: true });
  vi.stubGlobal("navigator", base);
  return { stub, handlers, actions };
}

async function renderPlayingPlayer() {
  apiMock.mockImplementation((path: string) => {
    if (path === `/progress/anilist%3A1%3Aepisode%3AMQ`) return Promise.resolve(null);
    if (path === "/playback/sessions") return Promise.resolve(session());
    if (path.startsWith("/progress/") && path.endsWith("/progress")) return Promise.resolve({});
    return Promise.resolve({});
  });
  render(
    <Player
      anime={anime}
      episode={episode}
      episodes={[episode]}
      settings={settings}
      provider="hianime"
      mode="sub"
      providers={providers}
      sessionSpeed={1}
      onSpeedChange={() => {}}
      onProviderChange={() => {}}
      onEpisode={() => {}}
      onClose={() => {}}
    />,
  );
  const video = document.querySelector("video");
  expect(video).toBeTruthy();
  await waitFor(() => {
    expect(apiMock.mock.calls.some(([p]) => p === "/playback/sessions")).toBe(true);
  });
  video?.dispatchEvent(new Event("loadedmetadata"));
  await waitFor(() => {
    expect(video?.src).toBeTruthy();
  });
  return video as HTMLVideoElement;
}

afterEach(() => {
  cleanup();
  apiMock.mockReset();
  vi.unstubAllGlobals();
});

describe("Player OS media session", () => {
  it("publishes now-playing metadata when playback starts", async () => {
    const { stub } = installMediaSession();
    const video = await renderPlayingPlayer();
    expect(stub.metadata).toBeNull();
    fireEvent.play(video);
    await waitFor(() => {
      expect(stub.metadata).not.toBeNull();
    });
    expect(stub.metadata?.init.title).toBe("The Beginning");
    expect(stub.metadata?.init.artist).toBe("Test Anime");
    expect(stub.metadata?.init.artwork).toEqual([{ src: mediaURL("/posters/1.jpg") }]);
    expect(stub.playbackState).toBe("playing");
  });

  it("clears metadata and handlers when the player exits", async () => {
    const { stub, handlers, actions } = installMediaSession();
    const video = await renderPlayingPlayer();
    fireEvent.play(video);
    await waitFor(() => {
      expect(stub.metadata).not.toBeNull();
    });
    fireEvent.pause(video);
    await waitFor(() => {
      expect(stub.playbackState).toBe("paused");
    });
    // Pause keeps the card; unmount (stop/exit) must drop it entirely.
    cleanup();
    expect(stub.metadata).toBeNull();
    expect(stub.playbackState).toBe("none");
    expect(handlers.play).toBeNull();
    expect(handlers.pause).toBeNull();
    expect(handlers.seekto).toBeNull();
    expect(actions.filter((action) => action === "play").length).toBeGreaterThanOrEqual(2);
  });

  it("routes OS handlers to the active player", async () => {
    const { handlers } = installMediaSession();
    const video = await renderPlayingPlayer();
    const play = vi.spyOn(video, "play");
    const pause = vi.spyOn(video, "pause");
    /* happy-dom events do not advance media state, so drive the element's
       paused flag directly: the handlers branch on the real media state. */
    const setPaused = (value: boolean) => {
      Object.defineProperty(video, "paused", { get: () => value, configurable: true });
    };
    setPaused(false);
    fireEvent.play(video);
    // Pause key: active element is playing.
    handlers.pause?.();
    expect(pause).toHaveBeenCalledTimes(1);
    // Play key after pausing (event state, not the spy, drives the branch).
    setPaused(true);
    fireEvent.pause(video);
    handlers.play?.();
    expect(play).toHaveBeenCalledTimes(1);
    // Seek key: clamped through the same seek path as in-app shortcuts.
    handlers.seekto?.({ seekTime: 30, fastSeek: false });
    await waitFor(() => {
      expect(video.currentTime).toBe(30);
    });
  });

  it("is a complete no-op when navigator.mediaSession is absent", async () => {
    // happy-dom ships no mediaSession, matching a WKWebView-style host.
    expect("mediaSession" in navigator).toBe(false);
    const video = await renderPlayingPlayer();
    expect(() => {
      fireEvent.play(video);
      fireEvent.pause(video);
      video.dispatchEvent(new Event("loadedmetadata"));
    }).not.toThrow();
    expect(video.paused).toBe(true);
  });
});
