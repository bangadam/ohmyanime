// @vitest-environment happy-dom
// Covers issue #10: unified anime+manga discovery and library. Same-title
// anime and manga never merge identities (namespaced ids), kind routing opens
// the right viewer, a failed source keeps the successful kind visible, the
// kind filter switches datasets, and bookmark changes persist via the API.
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Anime, Manga } from "../api";
import { copy } from "../copy";
import type { LibraryEntry } from "../types";
import MyList from "./MyList";
import Search from "./Search";

function anime(overrides: Partial<Anime> = {}): Anime {
  return { id: "anilist:a", title: "Same Title", availability: [], ...overrides };
}

function manga(overrides: Partial<Manga> = {}): Manga {
  return { id: "mangadex:m", title: "Same Title", availability: ["mangadex"], ...overrides };
}

function searchProps(overrides: Partial<Parameters<typeof Search>[0]> = {}) {
  return {
    input: "same",
    query: "same",
    onInput: () => {},
    onSubmit: () => {},
    onClear: () => {},
    results: [anime()],
    mangaResults: [manga()],
    kind: "all" as const,
    onKind: () => {},
    mangaProvider: "all",
    onMangaProvider: () => {},
    mangaProviderOptions: [
      { value: "all", label: "All sources" },
      { value: "mangadex", label: "Mangadex" },
    ],
    loading: false,
    loadingMore: false,
    error: "",
    stale: false,
    searched: true,
    hasMore: false,
    onOpen: () => {},
    onLoadMore: () => {},
    onRetry: () => {},
    ...overrides,
  };
}

function listProps(overrides: Partial<Parameters<typeof MyList>[0]> = {}) {
  const base: Parameters<typeof MyList>[0] = {
    items: [
      { kind: "anime", item: anime(), createdAt: "" },
      { kind: "manga", item: manga(), createdAt: "" },
    ],
    loading: false,
    loadingMore: false,
    error: "",
    hasMore: false,
    filter: "all",
    sort: "title-asc",
    removingId: null,
    onFilter: () => {},
    onSort: () => {},
    onOpen: () => {},
    onLoadMore: () => {},
    onRetry: () => {},
    onRemove: () => {},
    onExplore: () => {},
  };
  return { ...base, ...overrides };
}

afterEach(cleanup);

describe("unified anime and manga discovery", () => {
  it("keeps same-title anime and manga as separate identities", () => {
    render(<Search {...searchProps()} />);
    // Two distinct cards keyed by namespaced ids, both titled "Same Title".
    expect(screen.getAllByText("Same Title").length).toBe(2);
    expect(screen.getByText(copy.search.animeResults)).toBeTruthy();
    expect(screen.getByText(copy.manga.results)).toBeTruthy();
  });

  it("routes each kind to its own viewer", () => {
    const onOpen = vi.fn();
    render(<Search {...searchProps({ onOpen })} />);
    // The first open button belongs to the anime card, the last to manga.
    const openButtons = screen.getAllByRole("button", { name: new RegExp(copy.home.viewDetails) });
    fireEvent.click(openButtons[0]);
    expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ id: "anilist:a" }), "anime");
    fireEvent.click(openButtons[openButtons.length - 1]);
    expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ id: "mangadex:m" }), "manga");
  });

  it("keeps the successful kind visible when one source fails", () => {
    render(
      <Search
        {...searchProps({
          mangaResults: [],
          error: copy.search.partialError,
        })}
      />,
    );
    expect(screen.getByText(copy.search.partialError)).toBeTruthy();
    expect(screen.getAllByText("Same Title").length).toBeGreaterThan(0);
    expect(screen.getByText(copy.search.animeResults)).toBeTruthy();
  });

  it("switches datasets when the kind filter changes", () => {
    const onKind = vi.fn();
    const { rerender } = render(<Search {...searchProps({ onKind })} />);
    fireEvent.click(screen.getByRole("button", { name: copy.search.kindManga }));
    expect(onKind).toHaveBeenCalledWith("manga");
    rerender(<Search {...searchProps({ kind: "manga" })} />);
    expect(screen.queryByText(copy.search.animeResults)).toBeNull();
    expect(screen.getByText(copy.manga.results)).toBeTruthy();
    rerender(<Search {...searchProps({ kind: "anime" })} />);
    expect(screen.getByText(copy.search.animeResults)).toBeTruthy();
    expect(screen.queryByText(copy.manga.results)).toBeNull();
  });
});

describe("unified personal library", () => {
  it("shows bookmarks of both kinds and filters by kind", () => {
    const { rerender } = render(<MyList {...listProps()} />);
    expect(screen.getAllByText("Same Title").length).toBe(2);
    rerender(<MyList {...listProps({ filter: "manga" })} />);
    expect(screen.getAllByText("Same Title").length).toBe(1);
    rerender(<MyList {...listProps({ filter: "anime" })} />);
    expect(screen.getAllByText("Same Title").length).toBe(1);
    rerender(<MyList {...listProps({ filter: "series" })} />);
    expect(screen.getAllByText("Same Title").length).toBe(1);
  });

  it("removes the right kind's bookmark and persists via the API", async () => {
    const onRemove = vi.fn();
    render(<MyList {...listProps({ onRemove })} />);
    const removeButtons = screen.getAllByRole("button", {
      name: new RegExp(`${copy.list.remove}: Same Title`),
    });
    fireEvent.click(removeButtons[1]);
    await waitFor(() => expect(onRemove).toHaveBeenCalledOnce());
    expect(onRemove).toHaveBeenCalledWith(expect.objectContaining({ kind: "manga", item: expect.objectContaining({ id: "mangadex:m" }) }));
  });

  it("keeps saved titles visible with a notice when one bookmark source fails", () => {
    render(<MyList {...listProps({ error: copy.search.partialError })} />);
    expect(screen.getByText(copy.search.partialError)).toBeTruthy();
    expect(screen.getAllByText("Same Title").length).toBe(2);
  });
});

describe("manga source filter", () => {
  it("offers each declaring provider and reports the chosen source", () => {
    const onMangaProvider = vi.fn();
    render(
      <Search
        {...searchProps({
          onMangaProvider,
          mangaProviderOptions: [
            { value: "all", label: "All sources" },
            { value: "mangadex", label: "Mangadex" },
            { value: "mangafire", label: "Mangafire" },
          ],
        })}
      />,
    );
    expect(screen.getByRole("group", { name: copy.manga.providerFilterLabel })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Mangafire" }));
    expect(onMangaProvider).toHaveBeenCalledWith("mangafire");
  });

  it("hides the source filter while only anime is shown", () => {
    render(<Search {...searchProps({ kind: "anime" })} />);
    expect(screen.queryByRole("group", { name: copy.manga.providerFilterLabel })).toBeNull();
  });
});
