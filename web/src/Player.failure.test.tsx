// @vitest-environment happy-dom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Player } from "./Player";
import { APIError } from "./api";
import { classifyPlaybackFailure, hlsFailureStage } from "./playback-failures";
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
  { id: "otakudesu", enabled: true, modes: ["sub"], status: "unknown", capabilities: { kinds: ["anime"], audioModes: ["sub"], readerFormats: [] } },
];

const settings: Settings = { subtitleLanguage: "off", quality: "auto", autoplay: false, preferredProvider: "auto", revision: 0 };
const anime: Anime = { id: "anilist:1", title: "Test Anime" } as Anime;
const episode: Episode = { id: "anilist:1:episode:MQ", animeId: "anilist:1", label: "1", ordinal: 1, availability: ["hianime"] } as Episode;

function session(): PlaybackSession {
  return { id: "sess-1", masterUrl: "/media/t/1", actualProvider: "hianime", actualMode: "sub", variants: [], subtitles: [], expiresAt: "2026-01-01T00:00:00Z" } as unknown as PlaybackSession;
}

function renderPlayer(provider = "hianime") {
  const onProviderChange = vi.fn();
  const onClose = vi.fn();
  render(
    <Player
      anime={anime}
      episode={episode}
      episodes={[episode]}
      settings={settings}
      provider={provider}
      mode="sub"
      providers={providers}
      sessionSpeed={1}
      onSpeedChange={() => {}}
      onProviderChange={onProviderChange}
      onEpisode={() => {}}
      onClose={onClose}
    />,
  );
  return { onProviderChange, onClose };
}

afterEach(() => {
  cleanup();
  apiMock.mockReset();
});

describe("classifyPlaybackFailure", () => {
  it("keeps timeout distinct from title absence", () => {
    const timeout = classifyPlaybackFailure(new APIError("PROVIDER_TIMEOUT", "", 504, true, "resolve"), "hianime");
    const absent = classifyPlaybackFailure(new APIError("TITLE_ABSENT", "", 404, false, "resolve"), "hianime");
    expect(timeout).toEqual({ kind: "timeout", stage: "resolve", retryable: true });
    expect(absent).toEqual({ kind: "provider", stage: "resolve", retryable: false });
    expect(timeout.kind).not.toBe(absent.kind);
  });

  it("marks auto-provider absence as exhausted", () => {
    expect(classifyPlaybackFailure(new APIError("TITLE_ABSENT", "", 404, false, "resolve"), "auto").kind).toBe("exhausted");
  });

  it("routes proxy stages to manifest and segment retries", () => {
    expect(classifyPlaybackFailure(new APIError("MANIFEST_INVALID", "", 502, true, "manifest"), "hianime").stage).toBe("manifest");
    expect(classifyPlaybackFailure(new APIError("UPSTREAM_UNAVAILABLE", "", 502, true, "segment"), "hianime").stage).toBe("segment");
    expect(classifyPlaybackFailure(new APIError("SESSION_EXPIRED", "", 410, false, "session"), "hianime").stage).toBe("session");
    expect(hlsFailureStage("manifestLoadError")).toBe("manifest");
    expect(hlsFailureStage("fragLoadError")).toBe("segment");
  });
});

describe("Player failure card", () => {
  it("offers retry for a timeout and retries the resolve stage only", async () => {
    apiMock.mockImplementation((path: string) => {
      if (path === "/progress/anilist%3A1%3Aepisode%3AMQ") return Promise.resolve(null);
      return Promise.reject(new APIError("PROVIDER_TIMEOUT", "", 504, true, "resolve"));
    });
    const { onClose } = renderPlayer();
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByText("This source timed out")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Choose provider" })).toBeTruthy();
    // Back action exists alongside retry and provider choice.
    expect(screen.getAllByRole("button", { name: "Back to episodes" }).length).toBeGreaterThan(0);
    // Retry resolves again: exactly one resolve call after the first failure.
    apiMock.mockImplementation((path: string) => {
      if (path === "/progress/anilist%3A1%3Aepisode%3AMQ") return Promise.resolve(null);
      return Promise.resolve(session());
    });
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => {
      const calls = apiMock.mock.calls.filter(([p]) => p === "/playback/sessions");
      expect(calls.length).toBe(2);
    });
    // No orphan session: the failed attempt never created one to revoke.
    const deletes = apiMock.mock.calls.filter(([p, init]) => String(p).startsWith("/playback/sessions/") && (init as RequestInit | undefined)?.method === "DELETE");
    expect(deletes.length).toBe(0);
    expect(onClose).not.toHaveBeenCalled();
  });

  it("hides retry for title absence and keeps provider and back actions", async () => {
    apiMock.mockImplementation((path: string) => {
      if (path === "/progress/anilist%3A1%3Aepisode%3AMQ") return Promise.resolve(null);
      return Promise.reject(new APIError("TITLE_ABSENT", "", 404, false, "resolve"));
    });
    const { onClose } = renderPlayer();
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByText("This episode isn't available here")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
    expect(screen.getByRole("button", { name: "Choose provider" })).toBeTruthy();
    fireEvent.click(screen.getAllByRole("button", { name: "Back to episodes" }).at(-1) as HTMLElement);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("retries a segment failure against the live session without a new resolve", async () => {
    apiMock.mockImplementation((path: string) => {
      if (path === "/progress/anilist%3A1%3Aepisode%3AMQ") return Promise.resolve(null);
      return Promise.resolve(session());
    });
    const { onClose } = renderPlayer();
    await waitFor(() => {
      expect(apiMock.mock.calls.filter(([p]) => p === "/playback/sessions").length).toBe(1);
    });
    const video = document.querySelector("video");
    expect(video).toBeTruthy();
    // Fatal media error after the one automatic recovery: element error path.
    fireEvent.error(video as HTMLVideoElement);
    expect(await screen.findByRole("alert")).toBeTruthy();
    const resolvesBefore = apiMock.mock.calls.filter(([p]) => p === "/playback/sessions").length;
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => {
      expect(screen.queryByRole("alert")).toBeNull();
    });
    const resolvesAfter = apiMock.mock.calls.filter(([p]) => p === "/playback/sessions").length;
    expect(resolvesAfter).toBe(resolvesBefore);
    expect(onClose).not.toHaveBeenCalled();
  });
});
