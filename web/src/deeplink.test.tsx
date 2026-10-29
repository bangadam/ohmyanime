// @vitest-environment happy-dom
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type * as ApiModule from "./api";

/* Deep links: the Go shell parses an allowlisted grammar and emits one typed
   event; the page must forward only well-formed payloads, dispatch both
   kinds, and route an unavailable title into the existing error recovery. */

const { emitLink, apiMock, initializeMock } = vi.hoisted(() => ({
  emitLink: vi.fn(),
  apiMock: vi.fn(),
  initializeMock: vi.fn(),
}));

vi.mock("mygo-runtime", () => ({
  isMyGo: () => true,
  event: () => ({
    on(listener: (payload: unknown) => void) {
      emitLink.mockImplementation((payload: unknown) => listener(payload));
      return () => {};
    },
  }),
  call: vi.fn(),
}));

vi.mock("./api", async (importOriginal) => {
  const original = await importOriginal<typeof ApiModule>();
  return {
    ...original,
    api: (...args: unknown[]) => apiMock(...args),
    mangaChapters: (mangaId: string, init?: RequestInit) =>
      apiMock(`/manga/${encodeURIComponent(mangaId)}/chapters`, init),
    refreshProvidersHealth: vi.fn(() => Promise.resolve([])),
    initialize: (...args: unknown[]) => initializeMock(...args),
  };
});

import App from "./App";
import { onTitleLink, isTitleLink } from "./deeplink";
import { APIError } from "./api";
import { copy } from "./copy";

beforeEach(() => {
  apiMock.mockReset();
  initializeMock.mockReset();
  initializeMock.mockResolvedValue({ providers: [], csrfToken: "csrf", desktop: true });
});

afterEach(() => {
  cleanup();
});

describe("deep-link event bridge", () => {
  it("subscribes and forwards only well-formed links", async () => {
    const seen: unknown[] = [];
    const stop = onTitleLink((link) => seen.push(link));
    expect(typeof stop).toBe("function");
    emitLink({ kind: "anime", titleId: "anilist:1" });
    emitLink({ kind: "manga" });
    emitLink(null);
    emitLink("myanime://anime/x");
    await vi.waitFor(() => expect(seen).toEqual([{ kind: "anime", titleId: "anilist:1" }]));
  });

  it("rejects malformed payloads without throwing", () => {
    expect(isTitleLink({ kind: "anime", titleId: "" })).toBe(false);
    expect(isTitleLink({ kind: 1, titleId: "x" })).toBe(false);
    expect(isTitleLink(undefined)).toBe(false);
  });
});

describe("App deep-link navigation", () => {
  it("navigates an anime link once startup completes and the detail request fails into recovery", async () => {
    apiMock.mockImplementation((path: string) => {
      if (path === "/settings") return Promise.resolve({ preferredProvider: "auto", revision: 0 });
      if (path === "/genres") return Promise.resolve({ items: [] });
      if (path === "/anime/anilist%3A404") {
        return Promise.reject(new APIError("NOT_FOUND", copy.states.notFound, 404, false));
      }
      return Promise.resolve({ items: [], episodes: [] });
    });
    render(<App />);
    await waitFor(() => expect(screen.getByRole("heading", { name: copy.nav.home })).toBeTruthy());
    emitLink({ kind: "anime", titleId: "anilist:404" });
    // Unavailable title: the detail overlay's error recovery UI.
    await waitFor(() => expect(screen.getByRole("alert")).toBeTruthy());
    expect(screen.getByRole("alert").textContent).toContain(copy.states.notFound);
    expect(apiMock.mock.calls.some(([p]) => p === "/anime/anilist%3A404")).toBe(true);
  });

  it("navigates a manga link to the manga detail flow", async () => {
    apiMock.mockImplementation((path: string) => {
      if (path === "/settings") return Promise.resolve({ preferredProvider: "auto", revision: 0 });
      if (path === "/genres") return Promise.resolve({ items: [] });
      if (path === "/manga/mangadex%3A1")
        return Promise.resolve({
          id: "mangadex:1",
          title: "Deep Linked Manga",
          availability: ["mangadex"],
        });
      if (path === "/manga/mangadex%3A1/chapters") return Promise.resolve([]);
      return Promise.resolve({ items: [] });
    });
    render(<App />);
    await waitFor(() => expect(screen.getByRole("heading", { name: copy.nav.home })).toBeTruthy());
    emitLink({ kind: "manga", titleId: "mangadex:1" });
    await waitFor(() => expect(apiMock.mock.calls.some(([p]) => p === "/manga/mangadex%3A1")).toBe(true));
    await waitFor(() => expect(screen.getByText("Deep Linked Manga")).toBeTruthy());
  });

  it("ignores unknown link kinds", async () => {
    apiMock.mockImplementation(() => Promise.resolve({ items: [] }));
    render(<App />);
    await waitFor(() => expect(screen.getByRole("heading", { name: copy.nav.home })).toBeTruthy());
    const callsBefore = apiMock.mock.calls.length;
    emitLink({ kind: "novel", titleId: "x" });
    const { promise, resolve } = Promise.withResolvers<void>();
    setTimeout(resolve, 20);
    await promise;
    expect(apiMock.mock.calls.length).toBe(callsBefore);
  });
});
