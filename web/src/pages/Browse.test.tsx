// @vitest-environment happy-dom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Anime } from "../api";
import { copy, format } from "../copy";
import { defaultCatalogFilters } from "../filters-url";
import type { BrowsePageProps, CatalogFilters } from "../types";
import Browse from "./Browse";

function filters(overrides: Partial<CatalogFilters> = {}): CatalogFilters {
  return { ...defaultCatalogFilters(), ...overrides };
}

function anime(overrides: Partial<Anime> = {}): Anime {
  return { id: "a1", title: "Frieren", availability: [], ...overrides };
}

function browseProps(overrides: Partial<BrowsePageProps> = {}): BrowsePageProps {
  return {
    filters: filters(),
    applied: filters(),
    onFilters: vi.fn(),
    onApply: vi.fn(),
    onClear: vi.fn(),
    seasons: ["winter", "spring", "summer", "fall"],
    genres: [
      { id: "Action", name: "Action" },
      { id: "Drama", name: "Drama" },
    ],
    genresError: "",
    tags: [
      { id: "Isekai", name: "Isekai", category: "Theme" },
      { id: "Time Skip", name: "Time Skip", category: "Setting" },
    ],
    tagsError: "",
    onRetryFacets: vi.fn(),
    items: [anime()],
    loading: false,
    loadingMore: false,
    error: "",
    stale: false,
    hasMore: false,
    onOpen: vi.fn(),
    onLoadMore: vi.fn(),
    onRetry: vi.fn(),
    ...overrides,
  };
}

afterEach(cleanup);

describe("Browse filter panel", () => {
  it("applies the draft filters from the submit button", () => {
    const onApply = vi.fn();
    render(<Browse {...browseProps({ onApply })} />);

    fireEvent.click(screen.getByRole("button", { name: copy.browse.apply }));

    expect(onApply).toHaveBeenCalledOnce();
  });

  it("applies the draft filters when the title field submits", () => {
    // Enter in a text input submits its form; the panel must not reload the page.
    const onApply = vi.fn();
    render(<Browse {...browseProps({ onApply })} />);
    const input = screen.getByLabelText(copy.browse.titleLabel) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "Frieren" } });

    const form = input.closest("form");
    if (!form) throw new Error("the title field must live inside the filter form");
    fireEvent.submit(form);

    expect(onApply).toHaveBeenCalledOnce();
  });

  it("appends and removes a genre chip without mutating the filter arrays", () => {
    const onFilters = vi.fn();
    const { rerender } = render(<Browse {...browseProps({ onFilters })} />);

    fireEvent.click(screen.getByRole("button", { name: "Action" }));
    expect(onFilters).toHaveBeenLastCalledWith(expect.objectContaining({ genres: ["Action"] }));

    rerender(
      <Browse {...browseProps({ onFilters, filters: filters({ genres: ["Action", "Drama"] }) })} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Action" }));
    expect(onFilters).toHaveBeenLastCalledWith(expect.objectContaining({ genres: ["Drama"] }));
  });

  it("removes an active filter chip and applies the reduced set immediately", () => {
    const onFilters = vi.fn();
    const onApply = vi.fn();
    const active = filters({ status: "releasing", genres: ["Action"] });
    render(<Browse {...browseProps({ onFilters, onApply, filters: active, applied: active })} />);

    fireEvent.click(
      screen.getByRole("button", {
        name: format(copy.browse.removeFilter, { filter: copy.browse.statusOptions.releasing }),
      }),
    );

    expect(onFilters).toHaveBeenCalledWith(expect.objectContaining({ status: "all", genres: ["Action"] }));
    expect(onApply).toHaveBeenCalledOnce();
  });

  it("clears every filter through the quiet action", () => {
    const onClear = vi.fn();
    render(<Browse {...browseProps({ onClear })} />);

    fireEvent.click(screen.getByRole("button", { name: copy.browse.clearAll }));

    expect(onClear).toHaveBeenCalledOnce();
  });
});

describe("Browse results", () => {
  it("keeps cached results visible under the stale notice", () => {
    render(<Browse {...browseProps({ stale: true })} />);

    expect(screen.getByText(copy.states.staleCatalogue)).toBeTruthy();
    expect(screen.getByText("Frieren")).toBeTruthy();
  });

  it("offers a retry when the catalogue fails with nothing cached", () => {
    const onRetry = vi.fn();
    render(<Browse {...browseProps({ onRetry, items: [], error: copy.states.requestFailed })} />);

    fireEvent.click(screen.getByRole("button", { name: copy.states.retry }));

    expect(onRetry).toHaveBeenCalledOnce();
  });

  it("shows the empty state when the applied filters match nothing", () => {
    render(
      <Browse {...browseProps({ items: [], applied: filters({ genres: ["Action"] }) })} />,
    );

    expect(screen.getByText(copy.browse.emptyTitle)).toBeTruthy();
  });
});
