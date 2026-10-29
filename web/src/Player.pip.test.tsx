// @vitest-environment happy-dom
//
// Capability-detected picture-in-picture playback (issue #19 [4.1]).
//
// happy-dom ships neither the standard document picture-in-picture API nor the
// webkit presentation-mode one — exactly the WKWebView shape — so the
// unsupported case runs against the untouched host. Supported cases install
// stubs on document/video. Live WKWebView and Chromium behaviour cannot be
// exercised here — that is the platform limit noted in the issue.
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
import type * as ApiModule from "./api";
import { Player } from "./Player";
import type { Anime, Episode, PlaybackSession, Provider, Settings } from "./api";

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));

vi.mock("./api", async (importOriginal) => {
  const original = await importOriginal<typeof ApiModule>();
  return { ...original, api: (...args: unknown[]) => apiMock(...(args as [string])) };
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
HTMLMediaElement.prototype.canPlayType = () => "maybe";

function pipButton() {
  return document.querySelector<HTMLButtonElement>(".player-pip");
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
  video!.dispatchEvent(new Event("loadedmetadata"));
  await waitFor(() => {
    expect(video!.src).toBeTruthy();
  });
  return video as HTMLVideoElement;
}

/* Standard-API stub split in two: the document-level capability flag must
   exist before render (detection runs on mount), the video-level request
   function after (the element only exists then). */
function enableDocumentPip() {
  const state = { pipElement: null as Element | null };
  Object.defineProperty(document, "pictureInPictureEnabled", { value: true, configurable: true });
  Object.defineProperty(document, "pictureInPictureElement", {
    get: () => state.pipElement,
    configurable: true,
  });
  return state;
}

function attachVideoPip(state: { pipElement: Element | null }, video: HTMLVideoElement) {
  const request = vi.fn(async () => {
    state.pipElement = video;
    video.dispatchEvent(new Event("enterpictureinpicture"));
  });
  Object.defineProperty(video, "requestPictureInPicture", { value: request, configurable: true });
  const exit = vi.fn(async () => {
    state.pipElement = null;
    video.dispatchEvent(new Event("leavepictureinpicture"));
  });
  (document as Document & { exitPictureInPicture: () => Promise<void> }).exitPictureInPicture = exit;
  return { request, exit };
}

afterEach(() => {
  cleanup();
  apiMock.mockReset();
  vi.unstubAllGlobals();
  // document property stubs were installed with configurable: true.
  delete (document as unknown as Record<string, unknown>).pictureInPictureEnabled;
  delete (document as unknown as Record<string, unknown>).pictureInPictureElement;
  delete (document as unknown as Record<string, unknown>).exitPictureInPicture;
  delete (HTMLVideoElement.prototype as unknown as Record<string, unknown>).webkitSupportsPresentationMode;
  delete (HTMLVideoElement.prototype as unknown as Record<string, unknown>).webkitSetPresentationMode;
});

describe("Player picture-in-picture", () => {
  it("shows no PiP control when neither API is supported (WKWebView shape)", async () => {
    expect("pictureInPictureEnabled" in document).toBe(false);
    const video = await renderPlayingPlayer();
    expect(typeof video.requestPictureInPicture).toBe("undefined");
    await waitFor(() => {
      expect(pipButton()).toBeNull();
    });
  });

  it("hides the control when the standard API is present but disabled", async () => {
    Object.defineProperty(document, "pictureInPictureEnabled", { value: false, configurable: true });
    await renderPlayingPlayer();
    expect(pipButton()).toBeNull();
  });

  it("toggles PiP through the standard API and reflects presentation state", async () => {
    const state = enableDocumentPip();
    const video = await renderPlayingPlayer();
    const { request, exit } = attachVideoPip(state, video);
    const button = await waitFor(() => {
      const node = pipButton();
      expect(node).toBeTruthy();
      return node!;
    });
    expect(button.getAttribute("aria-pressed")).toBe("false");
    expect(button.getAttribute("aria-label")).toBe("Enter picture-in-picture");
    button.click();
    await waitFor(() => {
      expect(button.getAttribute("aria-pressed")).toBe("true");
    });
    expect(button.getAttribute("aria-label")).toBe("Exit picture-in-picture");
    expect(request).toHaveBeenCalledTimes(1);
    expect(state.pipElement).toBe(video);
    button.click();
    await waitFor(() => {
      expect(button.getAttribute("aria-pressed")).toBe("false");
    });
    expect(exit).toHaveBeenCalledTimes(1);
    expect(state.pipElement).toBeNull();
  });

  it("reflects externally driven enter/leave events", async () => {
    const state = enableDocumentPip();
    const video = await renderPlayingPlayer();
    attachVideoPip(state, video);
    const button = await waitFor(() => pipButton()!);
    video.dispatchEvent(new Event("enterpictureinpicture"));
    await waitFor(() => {
      expect(button.getAttribute("aria-pressed")).toBe("true");
    });
    // External exit, e.g. the pip window close button.
    video.dispatchEvent(new Event("leavepictureinpicture"));
    await waitFor(() => {
      expect(button.getAttribute("aria-pressed")).toBe("false");
    });
  });

  it("falls back to the webkit presentation-mode API", async () => {
    /* Capability must exist before mount for detection: put the webkit
       surface on the prototype, then shadow it with a spy per element. */
    Object.defineProperty(HTMLVideoElement.prototype, "webkitSupportsPresentationMode", {
      value: true,
      configurable: true,
    });
    Object.defineProperty(HTMLVideoElement.prototype, "webkitSetPresentationMode", {
      value: () => {},
      configurable: true,
    });
    const video = await renderPlayingPlayer();
    Object.defineProperty(video, "webkitPresentationMode", { value: "inline", configurable: true });
    const setMode = vi.fn(async (mode: string) => {
      Object.defineProperty(video, "webkitPresentationMode", { value: mode, configurable: true });
      video.dispatchEvent(new Event("webkitpresentationmodechanged"));
    });
    Object.defineProperty(video, "webkitSetPresentationMode", { value: setMode, configurable: true });
    const button = await waitFor(() => pipButton()!);
    button.click();
    await waitFor(() => {
      expect(setMode).toHaveBeenCalledWith("picture-in-picture");
    });
    await waitFor(() => {
      expect(button.getAttribute("aria-pressed")).toBe("true");
    });
  });

  it("exits PiP when the player unmounts", async () => {
    const state = enableDocumentPip();
    const video = await renderPlayingPlayer();
    const { exit } = attachVideoPip(state, video);
    const button = await waitFor(() => pipButton()!);
    button.click();
    await waitFor(() => {
      expect(state.pipElement).toBe(video);
    });
    cleanup();
    expect(state.pipElement).toBeNull();
    expect(exit).toHaveBeenCalledTimes(1);
  });
});
