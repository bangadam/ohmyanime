// @vitest-environment happy-dom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Anime, HistoryItem } from "../api";
import { copy } from "../copy";
import Home from "./Home";
import Search from "./Search";
import Browse from "./Browse";
import { defaultCatalogFilters } from "../filters-url";

function anime(overrides: Partial<Anime> = {}): Anime {
  return {
    id: "a1",
    title: "Cached Title",
    // Playback availability is never sourced from the catalogue cache.
    availability: [],
    ...overrides,
  };
}

function homeProps(overrides: Partial<Parameters<typeof Home>[0]> = {}) {
  return {
    feature: anime(),
    catalogue: [anime()],
    history: [] as HistoryItem[],
    onRemoveFromContinue: () => {},
    loading: false,
    loadingMore: false,
    error: "",
    stale: false,
    hasMore: false,
    onOpen: () => {},
    onViewSeason: () => {},
    onViewHistory: () => {},
    onLoadMore: () => {},
    onRetry: () => {},
    ...overrides,
  };
}

afterEach(cleanup);

describe("browse pages stale/offline catalogue states", () => {
  it("Home shows a stale banner with retry when serving cached catalogue data", () => {
    const onRetry = vi.fn();
    render(<Home {...homeProps({ stale: true, onRetry })} />);

    expect(screen.getByText(copy.states.staleCatalogue)).toBeTruthy();
    expect(screen.getAllByText("Cached Title").length).toBeGreaterThan(0);

    fireEvent.click(screen.getAllByRole("button", { name: copy.states.retry })[0]);
    expect(onRetry).toHaveBeenCalledOnce();

    expect(screen.getAllByText("Cached Title").length).toBeGreaterThan(0);
  });

  it("Home hides the stale banner for fresh data", () => {
    render(<Home {...homeProps({ stale: false })} />);
    expect(screen.queryByText(copy.states.staleCatalogue)).toBeNull();
  });

  it("Home shows an honest offline error when the cache is empty", () => {
    const onRetry = vi.fn();
    render(
      <Home
        {...homeProps({
          feature: null,
          catalogue: [],
          history: [],
          error: copy.states.offlineCatalogue,
          stale: false,
          onRetry,
        })}
      />,
    );

    expect(screen.queryByText("Cached Title")).toBeNull();
    expect(screen.getByText(copy.states.offlineCatalogue)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: copy.states.retry }));
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it("Search shows the stale banner over cached results", () => {
    const onRetry = vi.fn();
    render(
      <Search
        input="cached"
        query="cached"
        onInput={() => {}}
        onSubmit={() => {}}
        onClear={() => {}}
        results={[anime()]}
        mangaResults={[]}
        kind="all"
        onKind={() => {}}
        mangaProvider="all"
        onMangaProvider={() => {}}
        mangaProviderOptions={[{ value: "all", label: "All sources" }]}

        loading={false}
        loadingMore={false}
        error=""
        stale
        searched

        hasMore={false}
        onOpen={() => {}}
        onLoadMore={() => {}}
        onRetry={onRetry}
      />,
    );
    expect(screen.getByText(copy.states.staleCatalogue)).toBeTruthy();
    expect(screen.getAllByText("Cached Title").length).toBeGreaterThan(0);

    fireEvent.click(screen.getAllByRole("button", { name: copy.states.retry })[0]);
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it("Browse shows the stale banner over cached results", () => {
    render(
      <Browse
        filters={defaultCatalogFilters()}
        applied={defaultCatalogFilters()}
        onFilters={() => {}}
        onApply={() => {}}
        onClear={() => {}}
        seasons={["winter"]}
        genres={[]}
        genresError=""
        tags={[]}
        tagsError=""
        onRetryFacets={() => {}}
        items={[anime()]}
        loading={false}
        loadingMore={false}
        error=""
        stale
        hasMore={false}
        onOpen={() => {}}
        onLoadMore={() => {}}
        onRetry={() => {}}
      />,
    );

    expect(screen.getByText(copy.states.staleCatalogue)).toBeTruthy();
    expect(screen.getByText("Cached Title")).toBeTruthy();
  });
});
