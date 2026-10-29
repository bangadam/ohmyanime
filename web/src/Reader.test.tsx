// @vitest-environment happy-dom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Reader } from "./Reader";
import type * as ApiModule from "./api";
import { APIError, type Manga, type MangaChapter, type MangaPage } from "./api";

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));

vi.mock("./api", async (importOriginal) => {
  const original = await importOriginal<typeof ApiModule>();
  return {
    ...original,
    api: (...args: unknown[]) => apiMock(...args),
  };
});

function manga(): Manga {
  return { id: "mangadex:abc", title: "Frieren", availability: ["mangadex"] };
}

function chapter(id = "chapter-1", ordinal = 1): MangaChapter {
  return { id, mangaId: "mangadex:abc", label: `Ch. ${ordinal}`, ordinal, language: "en", pageCount: 5 };
}

function pages(count: number): MangaPage[] {
  return Array.from({ length: count }, (_, index) => ({ index, url: `/pages/${index}.jpg` }));
}

function renderReader(chapters: MangaChapter[] = [chapter()], pageCount = 5) {
  apiMock.mockResolvedValue({
    items: pages(pageCount),
    chapter: chapters[0],
    mangaId: "mangadex:abc",
  });
  const onChapter = vi.fn();
  const onClose = vi.fn();
  const view = render(
    <Reader manga={manga()} chapter={chapters[0]} chapters={chapters} onChapter={onChapter} onClose={onClose} />,
  );
  return { onChapter, onClose, ...view };
}

async function ready(total = 5) {
  await screen.findByText(`Page 1 of ${total}`);
}

async function enterPagedMode() {
  fireEvent.click(screen.getByTitle("Paged mode"));
  await screen.findByText("Page 1 of 5");
}

beforeEach(() => {
  apiMock.mockReset();
});

afterEach(cleanup);

describe("Reader reading modes", () => {
  it("flips the page-turn direction under RTL", async () => {
    renderReader();
    await ready();
    await enterPagedMode();

    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(screen.getByText("Page 2 of 5")).toBeTruthy();

    fireEvent.click(screen.getByTitle("Read right to left"));
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(screen.getByText("Page 1 of 5")).toBeTruthy();

    // The flipped left arrow now advances.
    fireEvent.keyDown(window, { key: "ArrowLeft" });
    expect(screen.getByText("Page 2 of 5")).toBeTruthy();
  });

  it("retains the visible logical page across resize and mode changes", async () => {
    const scrollIntoView = vi
      .spyOn(Element.prototype, "scrollIntoView")
      .mockImplementation(() => {});
    renderReader();
    await ready();
    await enterPagedMode();

    fireEvent.keyDown(window, { key: "ArrowRight" });
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(screen.getByText("Page 3 of 5")).toBeTruthy();

    fireEvent.resize(window);
    expect(screen.getByText("Page 3 of 5")).toBeTruthy();

    // Back to the vertical strip: the same logical page stays visible.
    fireEvent.click(screen.getByTitle("Vertical scroll mode"));
    expect(screen.getByText("Page 3 of 5")).toBeTruthy();
    const target = document.querySelector('[data-page-index="2"]');
    expect(scrollIntoView.mock.instances.at(-1)).toBe(target);
    scrollIntoView.mockRestore();
  });

  it("gives an odd final page its own single-page spread", async () => {
    renderReader();
    await ready();
    await enterPagedMode();
    fireEvent.click(screen.getByTitle("Two-page spread off"));

    fireEvent.keyDown(window, { key: "Home" });
    expect(screen.getByText("Page 1 of 5")).toBeTruthy();
    expect(screen.getAllByRole("img")).toHaveLength(2);

    fireEvent.keyDown(window, { key: "End" });
    expect(screen.getByText("Page 5 of 5")).toBeTruthy();
    expect(screen.getAllByRole("img")).toHaveLength(1);
    expect((screen.getByRole("img") as HTMLImageElement).alt).toBe("Page 5");
  });

  it("ignores reader keys while focus is in a text input", async () => {
    renderReader();
    await ready();
    await enterPagedMode();

    const input = document.createElement("input");
    document.querySelector(".reader")?.appendChild(input);
    input.focus();
    expect(document.activeElement).toBe(input);

    fireEvent.keyDown(input, { key: "ArrowRight" });
    expect(screen.getByText("Page 1 of 5")).toBeTruthy();

    // The same key on the reader itself still pages.
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(screen.getByText("Page 2 of 5")).toBeTruthy();
  });

  it("restores a failed page after retry", async () => {
    renderReader();
    await ready();

    const image = await screen.findByAltText("Page 1");
    fireEvent.error(image);
    expect(screen.getByText("Page 1 couldn't be loaded.")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Retry page" }));
    const retried = await screen.findByAltText("Page 1");
    expect(retried).not.toBe(image);
    fireEvent.load(retried);

    await waitFor(() => {
      expect(screen.queryByText("Page 1 couldn't be loaded.")).toBeNull();
    });
    expect(screen.getByAltText("Page 1").getAttribute("src")).toBe("/pages/0.jpg");
  });
});

describe("Reader reading progress", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  type Saved = { chapterId: string; page: number; completed: boolean; revision: number } | null;

  /* An in-memory reading-progress server: GET returns the saved position
     (or null), PUT applies it and bumps the revision, mirroring the API.
     Everything else falls through to the chapter pages response. */
  function progressServer(saved: { current: Saved }, rejectPut = false) {
    return vi.fn(async (path: string, init: RequestInit = {}) => {
      if (!path.includes("/reading-progress")) {
        return { items: pages(5), chapter: chapter(), mangaId: "mangadex:abc" };
      }
      if (init.method === "PUT") {
        if (rejectPut) throw new APIError("CONFLICT", "conflict", 409);
        const body = JSON.parse(String(init.body));
        saved.current = {
          chapterId: body.chapterId,
          page: body.page,
          completed: body.completed,
          revision: (saved.current?.revision ?? 0) + 1,
        };
        return { mangaId: "mangadex:abc", ...saved.current, updatedAt: "2026-01-01T00:00:00Z" };
      }
      return saved.current
        ? { mangaId: "mangadex:abc", ...saved.current, updatedAt: "2026-01-01T00:00:00Z" }
        : null;
    });
  }

  async function flush(rounds = 3) {
    for (let i = 0; i < rounds; i++) {
      await act(async () => {
        await Promise.resolve();
      });
    }
  }

  async function settle(ms: number) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ms);
    });
  }

  function putCalls() {
    return apiMock.mock.calls.filter((call) => call[1]?.method === "PUT");
  }

  function lastPutBody() {
    const call = putCalls().at(-1);
    return JSON.parse(String(call?.[1]?.body));
  }

  it("resumes the exact stored logical page after a restart", async () => {
    const saved: { current: Saved } = { current: null };
    apiMock.mockImplementation(progressServer(saved));
    const first = render(
      <Reader manga={manga()} chapter={chapter()} chapters={[chapter()]} onChapter={vi.fn()} onClose={vi.fn()} />,
    );
    await flush();
    fireEvent.click(screen.getByTitle("Paged mode"));
    fireEvent.keyDown(window, { key: "ArrowRight" });
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(screen.getByText("Page 3 of 5")).toBeTruthy();
    await settle(1000);
    expect(putCalls()).toHaveLength(1);
    expect(lastPutBody()).toEqual({ chapterId: "chapter-1", page: 2, completed: false, expectedRevision: 0 });
    first.unmount();
    await flush();

    // Restart: the saved position comes back from the server.
    const scrollIntoView = vi.spyOn(Element.prototype, "scrollIntoView").mockImplementation(() => {});
    render(
      <Reader manga={manga()} chapter={chapter()} chapters={[chapter()]} onChapter={vi.fn()} onClose={vi.fn()} />,
    );
    await flush();
    expect(screen.getByText("Page 3 of 5")).toBeTruthy();
    // Strip mode scrolls the resumed page into view instead of the top.
    const target = document.querySelector('[data-page-index="2"]');
    expect(scrollIntoView.mock.instances.at(-1)).toBe(target);
    scrollIntoView.mockRestore();
  });

  it("does not mark the chapter complete after moving past one page", async () => {
    const saved: { current: Saved } = { current: null };
    apiMock.mockImplementation(progressServer(saved));
    render(
      <Reader manga={manga()} chapter={chapter()} chapters={[chapter()]} onChapter={vi.fn()} onClose={vi.fn()} />,
    );
    await flush();
    fireEvent.click(screen.getByTitle("Paged mode"));
    fireEvent.keyDown(window, { key: "ArrowRight" });
    await settle(1000);
    expect(lastPutBody().completed).toBe(false);

    // Reaching the last page is the completion trigger (documented policy).
    fireEvent.keyDown(window, { key: "End" });
    await settle(1000);
    expect(lastPutBody()).toMatchObject({ page: 4, completed: true });
  });

  it("clamps the stored page when the inventory shrank", async () => {
    const saved: { current: Saved } = { current: { chapterId: "chapter-1", page: 9, completed: false, revision: 3 } };
    apiMock.mockImplementation(progressServer(saved));
    render(
      <Reader manga={manga()} chapter={chapter()} chapters={[chapter()]} onChapter={vi.fn()} onClose={vi.fn()} />,
    );
    await flush();
    expect(screen.getByText("Page 5 of 5")).toBeTruthy();
    await settle(1000);
    expect(lastPutBody().page).toBe(4);
  });

  it("rejects a stale window: a 409 stops further writes", async () => {
    apiMock.mockImplementation(progressServer({ current: null }, true));
    render(
      <Reader manga={manga()} chapter={chapter()} chapters={[chapter()]} onChapter={vi.fn()} onClose={vi.fn()} />,
    );
    await flush();
    fireEvent.click(screen.getByTitle("Paged mode"));
    fireEvent.keyDown(window, { key: "ArrowRight" });
    await settle(1000);
    expect(putCalls()).toHaveLength(1);

    // Later changes in this window never reach the server again.
    fireEvent.keyDown(window, { key: "ArrowRight" });
    await settle(1000);
    expect(putCalls()).toHaveLength(1);
  });

  it("debounces rapid page turns into one write and saves on unmount", async () => {
    const saved: { current: Saved } = { current: null };
    apiMock.mockImplementation(progressServer(saved));
    const view = render(
      <Reader manga={manga()} chapter={chapter()} chapters={[chapter()]} onChapter={vi.fn()} onClose={vi.fn()} />,
    );
    await flush();
    fireEvent.click(screen.getByTitle("Paged mode"));
    fireEvent.keyDown(window, { key: "ArrowRight" });
    fireEvent.keyDown(window, { key: "ArrowRight" });
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(putCalls()).toHaveLength(0);

    await settle(1000);
    expect(putCalls()).toHaveLength(1);
    expect(lastPutBody()).toMatchObject({ page: 3, completed: false });

    // A change right before closing still flushes on unmount: the debounce
    // timer is cancelled and the final position is written immediately.
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(putCalls()).toHaveLength(1);
    view.unmount();
    await flush();
    expect(putCalls()).toHaveLength(2);
    expect(lastPutBody()).toMatchObject({ page: 4, completed: true });
  });
});
