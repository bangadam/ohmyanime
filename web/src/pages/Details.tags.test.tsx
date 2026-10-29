// @vitest-environment happy-dom
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Anime, Episode } from "../api";
import Details from "./Details";

function anime(overrides: Partial<Anime> = {}): Anime {
  return {
    id: "a1",
    title: "Spirited Away",
    synopsis: "A tale (Source: AniList) about a girl.",
    availability: ["alpha"],
    ...overrides,
  };
}

const episode: Episode = {
  id: "e1",
  animeId: "a1",
  label: "1",
  title: "Story 1",
  ordinal: 1,
  availability: ["alpha"],
};

const props = {
  episodes: [episode],
  state: "ready" as const,
  error: "",
  bookmarked: false,
  busy: false,
  watched: {},
  watchedBusy: false,
  onBack: () => {},
  onPlay: () => {},
  onToggleBookmark: () => {},
  onToggleWatched: () => {},
  onBulkWatched: () => {},
  onFilterByGenre: () => {},
  onFilterByTag: () => {},
  onRetry: () => {},
};

afterEach(cleanup);

describe("Details taxonomy", () => {
  it("renders genres and tags as buttons named for the filter they apply", () => {
    render(
      <Details
        {...props}
        anime={anime({ genres: ["Action", "Fantasy"], tags: ["Time Travel", "Isekai"] })}
      />,
    );

    const genres = screen.getByRole("group", { name: "Genres" });
    expect(within(genres).getByRole("button", { name: "Filter by Action" })).toBeTruthy();
    expect(within(genres).getByRole("button", { name: "Filter by Fantasy" })).toBeTruthy();

    const tags = screen.getByRole("group", { name: "Tags" });
    expect(within(tags).getByRole("button", { name: "Filter by Time Travel" })).toBeTruthy();
    expect(within(tags).getByRole("button", { name: "Filter by Isekai" })).toBeTruthy();
  });

  it("calls onFilterByGenre with the clicked genre", () => {
    const onFilterByGenre = vi.fn();
    render(
      <Details {...props} anime={anime({ genres: ["Action", "Fantasy"] })} onFilterByGenre={onFilterByGenre} />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Filter by Fantasy" }));

    expect(onFilterByGenre).toHaveBeenCalledTimes(1);
    expect(onFilterByGenre).toHaveBeenCalledWith("Fantasy");
  });

  it("calls onFilterByTag with the clicked tag", () => {
    const onFilterByTag = vi.fn();
    render(<Details {...props} anime={anime({ tags: ["Isekai"] })} onFilterByTag={onFilterByTag} />);

    fireEvent.click(screen.getByRole("button", { name: "Filter by Isekai" }));

    expect(onFilterByTag).toHaveBeenCalledTimes(1);
    expect(onFilterByTag).toHaveBeenCalledWith("Isekai");
  });

  it("renders no taxonomy group when the title carries neither genres nor tags", () => {
    render(<Details {...props} anime={anime()} />);

    expect(screen.queryByRole("group", { name: "Genres" })).toBeNull();
    expect(screen.queryByRole("group", { name: "Tags" })).toBeNull();
    expect(screen.getByText("A tale about a girl.")).toBeTruthy();
  });
});
