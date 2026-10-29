// @vitest-environment happy-dom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Reader } from "./Reader";
import { orderChapters } from "./reader-model";
import type * as ApiModule from "./api";
import type { Manga, MangaChapter, MangaPage } from "./api";

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));

vi.mock("./api", async (importOriginal) => {
  const original = await importOriginal<typeof ApiModule>();
  return { ...original, api: apiMock };
});

function manga(): Manga {
  return { id: "mangadex:abc", title: "Frieren", availability: ["mangadex"] };
}

function chapter(overrides: Partial<MangaChapter> = {}): MangaChapter {
  return {
    id: "chapter-1",
    mangaId: "mangadex:abc",
    label: "Ch. 1",
    ordinal: 1,
    number: "1",
    language: "en",
    pageCount: 5,
    ...overrides,
  };
}

function pages(count: number): MangaPage[] {
  return Array.from({ length: count }, (_, index) => ({ index, url: `/pages/${index}.jpg` }));
}

function renderReader(chapters: MangaChapter[], current: MangaChapter) {
  const onChapter = vi.fn();
  const onClose = vi.fn();
  const props = {
    manga: manga(),
    chapters,
    chapter: current,
    onChapter,
    onClose,
  };
  const view = render(<Reader {...props} />);
  return {
    onChapter,
    onClose,
    view,
    rerender(next: MangaChapter) {
      view.rerender(<Reader {...props} chapter={next} />);
    },
  };
}

async function ready(total = 5) {
  await screen.findByText(`Page 1 of ${total}`);
}

beforeEach(() => {
  apiMock.mockReset();
});

afterEach(cleanup);

describe("Reader chapter navigation", () => {
  it("disables previous on the first chapter and next on the last", async () => {
    const chapters = [
      chapter({ id: "c1", label: "Ch. 1", number: "1", ordinal: 1 }),
      chapter({ id: "c2", label: "Ch. 2", number: "2", ordinal: 2 }),
      chapter({ id: "c3", label: "Ch. 3", number: "3", ordinal: 3 }),
    ];
    apiMock.mockResolvedValue({ items: pages(5) });

    const first = renderReader(chapters, chapters[0]);
    await ready();
    expect((screen.getByTitle("Previous chapter") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTitle("Next chapter") as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(screen.getByTitle("Next chapter"));
    expect(first.onChapter).toHaveBeenCalledWith(chapters[1]);
    first.view.unmount();

    renderReader(chapters, chapters[chapters.length - 1]);
    await ready();
    expect((screen.getByTitle("Previous chapter") as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByTitle("Next chapter") as HTMLButtonElement).disabled).toBe(true);
  });

  it("cannot open an unavailable chapter from the picker", async () => {
    const chapters = [
      chapter({ id: "c1", label: "Ch. 1", number: "1", ordinal: 1 }),
      chapter({ id: "c2", label: "Ch. 2", number: "2", ordinal: 2, pageCount: 0 }),
      chapter({ id: "c3", label: "Ch. 3", number: "3", ordinal: 3 }),
    ];
    apiMock.mockResolvedValue({ items: pages(5) });

    const { onChapter } = renderReader(chapters, chapters[0]);
    await ready();

    fireEvent.click(screen.getByTitle("Jump to chapter"));
    const unavailable = screen.getByRole("button", { name: /Ch\. 2 unavailable/ });
    expect((unavailable as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(unavailable as HTMLButtonElement);
    expect(onChapter).not.toHaveBeenCalled();

    // Header prev/next skip the unavailable middle chapter entirely.
    fireEvent.click(screen.getByTitle("Next chapter"));
    expect(onChapter).toHaveBeenCalledWith(chapters[2]);
  });

  it("orders chapters by backend identity, so 10 follows 9", () => {
    // Delivered out of order and with labels that sort lexically wrong:
    // "10" < "9" as strings, and specials sort by ordinal.
    const chapters = [
      chapter({ id: "c9", label: "Ch. 9", number: "9", ordinal: 9 }),
      chapter({ id: "c2", label: "Ch. 2", number: "2", ordinal: 2 }),
      chapter({ id: "c10", label: "Ch. 10", number: "10", ordinal: 10 }),
      chapter({ id: "c12", label: "Ch. 12", number: "12", ordinal: 12 }),
      chapter({ id: "extra", label: "Extra", number: "", ordinal: 1e9 }),
    ];
    const ordered = orderChapters(chapters);
    expect(ordered.map((item) => item.label)).toEqual([
      "Ch. 2",
      "Ch. 9",
      "Ch. 10",
      "Ch. 12",
      "Extra",
    ]);
    // Chapter 10 follows 9 regardless of lexical label ordering.
    const atNine = ordered.findIndex((item) => item.id === "c9");
    expect(ordered[atNine + 1].id).toBe("c10");
  });

  it("navigating from 9 lands on chapter 10 in the reader", async () => {
    const chapters = [
      chapter({ id: "c9", label: "Ch. 9", number: "9", ordinal: 9 }),
      chapter({ id: "c10", label: "Ch. 10", number: "10", ordinal: 10 }),
    ];
    apiMock.mockResolvedValue({ items: pages(5) });

    const { onChapter } = renderReader(chapters, chapters[0]);
    await ready();
    fireEvent.click(screen.getByTitle("Next chapter"));
    expect(onChapter).toHaveBeenCalledWith(chapters[1]);
    expect(onChapter.mock.calls[0][0].label).toBe("Ch. 10");
  });

  it("cancels the in-flight page load when the chapter changes", async () => {
    const chapters = [
      chapter({ id: "c1", label: "Ch. 1", number: "1", ordinal: 1 }),
      chapter({ id: "c2", label: "Ch. 2", number: "2", ordinal: 2 }),
    ];
    const signals: AbortSignal[] = [];
    let pagesCalls = 0;
    apiMock.mockImplementation((path: string, init?: RequestInit) => {
      // Only the page-load call must hang; progress fetches resolve at once.
      if (!path.endsWith("/pages")) return Promise.resolve(null);
      pagesCalls += 1;
      signals.push(init?.signal as AbortSignal);
      // Never resolves: the load must stay in flight until aborted.
      return new Promise<MangaPage[]>(() => {});
    });

    const { rerender } = renderReader(chapters, chapters[0]);
    await waitFor(() => expect(signals.length).toBe(1));
    expect(signals[0].aborted).toBe(false);

    rerender(chapters[1]);
    expect(signals[0].aborted).toBe(true);
    expect(pagesCalls).toBe(2);
  });


  it("moves focus to the first page of the new chapter", async () => {
    const chapters = [
      chapter({ id: "c1", label: "Ch. 1", number: "1", ordinal: 1 }),
      chapter({ id: "c2", label: "Ch. 2", number: "2", ordinal: 2 }),
    ];
    apiMock.mockResolvedValue({ items: pages(5) });

    const { rerender } = renderReader(chapters, chapters[0]);
    await ready();
    // Initial mount focuses the reader region, not a page.
    expect(document.activeElement).toBe(document.querySelector(".reader"));

    rerender(chapters[1]);
    await screen.findByText("Page 1 of 5");
    await waitFor(() => {
      expect(document.activeElement).toBe(document.querySelector('[data-page-index="0"]'));
    });
  });

  it("offers an explicit end-of-chapter action", async () => {
    const chapters = [
      chapter({ id: "c1", label: "Ch. 1", number: "1", ordinal: 1 }),
      chapter({ id: "c2", label: "Ch. 2", number: "2", ordinal: 2 }),
    ];
    apiMock.mockResolvedValue({ items: pages(5) });

    const { onChapter } = renderReader(chapters, chapters[0]);
    await ready();
    // Paged mode: the End key reaches the final spread and the action.
    fireEvent.click(screen.getByTitle("Paged mode"));
    fireEvent.keyDown(window, { key: "End" });
    const action = await screen.findByText("End of chapter");
    expect(action).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Next chapter · Ch. 2" }));
    expect(onChapter).toHaveBeenCalledWith(chapters[1]);
  });

  it("states the latest chapter at the end of the final chapter", async () => {
    apiMock.mockResolvedValue({ items: pages(2) });
    renderReader([chapter()], chapter());
    await ready(2);
    // Strip mode: the intersection-driven page counter reaches the last page
    // only through observation; drive it via the resize handler instead.
    fireEvent.click(screen.getByTitle("Paged mode"));
    fireEvent.keyDown(window, { key: "End" });
    expect(await screen.findByText("You've reached the latest chapter.")).toBeTruthy();
    expect(screen.queryByText("End of chapter")).toBeNull();
  });
});
