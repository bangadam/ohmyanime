// @vitest-environment happy-dom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
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

function episode(ordinal: number, overrides: Partial<Episode> = {}): Episode {
  return {
    id: `e${ordinal}`,
    animeId: "a1",
    label: String(ordinal),
    title: `Story ${ordinal}`,
    ordinal,
    availability: ["alpha"],
    ...overrides,
  };
}

const props = {
  error: "",
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

describe("Details movie/series presentation", () => {
  it("presents a movie with a single Play movie action and no episode list", () => {
    render(
      <Details
        {...props}
        anime={anime({ type: "movie" })}
        episodes={[episode(1)]}
        state="ready"
        bookmarked={false}
      />,
    );
    expect(screen.getByRole("heading", { name: "Spirited Away" })).toBeTruthy();
    const play = screen.getAllByRole("button").find((button) => button.textContent === "Play movie");
    expect(play).toBeTruthy();
    expect(screen.queryByRole("list")).toBeNull();
    // A movie is a single work, so no episode count is shown.
    expect(screen.queryByText("1 episode")).toBeNull();
  });

  it("presents a series with an ordered, playable episode list", () => {
    render(
      <Details
        {...props}
        anime={anime({ type: "tv" })}
        episodes={[episode(2), episode(1)]}
        state="ready"
        bookmarked={false}
      />,
    );
    expect(screen.getByText("Play episode 1")).toBeTruthy();
    expect(document.querySelector(".section-header__count")?.textContent).toBe("2");
    const rows = screen.getAllByRole("listitem");
    expect(rows.map((row) => row.textContent?.match(/\d/)?.[0])).toEqual(["1", "2"]);
  });

  it("disables unavailable episodes and the hero action when nothing is playable", () => {
    render(
      <Details
        {...props}
        anime={anime({ type: "tv" })}
        episodes={[episode(1, { availability: [] })]}
        state="ready"
        bookmarked={false}
      />,
    );
    const row = screen.getByRole("button", { name: "Play episode 1: Story 1, Unavailable" });
    expect((row as HTMLButtonElement).disabled).toBe(true);
    const hero = screen.getAllByRole("button").find((button) => button.textContent === "Play first episode");
    expect(hero).toBeTruthy();
    expect((hero as HTMLButtonElement).disabled).toBe(true);
  });

  it("shows the error state with a retry action", async () => {
    const onRetry = vi.fn();
    render(
      <Details
        {...props}
        onRetry={onRetry}
        anime={anime()}
        episodes={[]}
        state="error"
        error="boom"
        bookmarked={false}
      />,
    );
    const retry = screen.getByRole("button", { name: "Try again" });
    retry.click();
    await waitFor(() => expect(onRetry).toHaveBeenCalled());
  });

  it("strips source attribution from the synopsis and shows alternative titles", () => {
    render(
      <Details
        {...props}
        anime={anime({ alternativeTitles: ["  ", "Sen to Chihiro"] })}
        episodes={[]}
        state="ready"
        bookmarked={false}
      />,
    );
    const synopsis = screen.getByText("A tale about a girl.");
    expect(synopsis).toBeTruthy();
    expect(screen.getByText("Sen to Chihiro")).toBeTruthy();
  });

  it("keeps the episode list for a movie split into several parts", () => {
    render(
      <Details
        {...props}
        anime={anime({ type: "movie" })}
        episodes={[episode(1), episode(2)]}
        state="ready"
        bookmarked={false}
      />,
    );
    expect(screen.getByRole("list")).toBeTruthy();
    expect(screen.queryByText("2 episodes")).toBeNull();
  });
});

describe("Details watched flags", () => {
  it("renders a toggle per episode reflecting the watched map", () => {
    render(
      <Details
        {...props}
        anime={anime()}
        episodes={[episode(1), episode(2)]}
        state="ready"
        bookmarked={false}
        watched={{ e1: true }}
      />,
    );
    const on = screen.getByRole("button", { name: "1: Mark as unwatched" });
    const off = screen.getByRole("button", { name: "2: Mark as watched" });
    expect(on.getAttribute("aria-pressed")).toBe("true");
    expect(off.getAttribute("aria-pressed")).toBe("false");
  });

  it("invokes the toggle handler and disables while busy", () => {
    const onToggleWatched = vi.fn();
    render(
      <Details
        {...props}
        onToggleWatched={onToggleWatched}
        anime={anime()}
        episodes={[episode(1)]}
        state="ready"
        bookmarked={false}
        watchedBusy
      />,
    );
    const toggle = screen.getByRole("button", { name: "1: Mark as watched" });
    expect((toggle as HTMLButtonElement).disabled).toBe(true);
    toggle.click();
    expect(onToggleWatched).not.toHaveBeenCalled();
  });

  it("offers bulk actions that call the bulk handler", () => {
    const onBulkWatched = vi.fn();
    render(
      <Details
        {...props}
        onBulkWatched={onBulkWatched}
        anime={anime()}
        episodes={[episode(1), episode(2)]}
        state="ready"
        bookmarked={false}
      />,
    );
    screen.getByRole("button", { name: "Mark all watched" }).click();
    screen.getByRole("button", { name: "Mark all unwatched" }).click();
    expect(onBulkWatched).toHaveBeenCalledWith(true);
    expect(onBulkWatched).toHaveBeenCalledWith(false);
  });
});

describe("Details long episode lists", () => {
  function longEpisodes(count: number): Episode[] {
    return Array.from({ length: count }, (_, index) => {
      const ordinal = index + 1;
      if (ordinal === 12) {
        // Fractional entry: label does not match its ordinal.
        return episode(12.5, { id: "e12.5", label: "12.5", title: "Interlude half" });
      }
      if (ordinal === count) {
        return episode(ordinal, {
          title: "Grand finale with a very long title that must not be dropped from the row",
        });
      }
      return episode(ordinal);
    });
  }

  function renderLong(episodeCount: number, watched: Record<string, boolean> = {}) {
    return render(
      <Details
        {...props}
        anime={anime({ type: "tv" })}
        episodes={longEpisodes(episodeCount)}
        state="ready"
        bookmarked={false}
        watched={watched}
      />,
    );
  }

  it("renders a 1000-plus-entry fixture with search intact", () => {
    renderLong(1200);
    const rows = screen.getAllByRole("listitem");
    expect(rows).toHaveLength(1200);
    // The jump-to-episode search appears for long lists.
    expect(screen.getByLabelText("Find or jump to an episode")).toBeTruthy();
    // Fractional labels are not lost.
    expect(screen.getByRole("button", { name: "Play episode 12.5: Interlude half" })).toBeTruthy();
    // The final entry with a long title is present.
    expect(
      screen.getByRole("button", {
        name: "Play episode 1200: Grand finale with a very long title that must not be dropped from the row",
      }),
    ).toBeTruthy();
  });

  it("keeps first, middle and final entries keyboard-focusable in list order", () => {
    renderLong(1200);
    const list = document.querySelector(".detail-episode-list");
    expect(list).toBeTruthy();
    const items = Array.from(list?.querySelectorAll<HTMLButtonElement>(".detail-episode") ?? []);
    expect(items).toHaveLength(1200);
    // DOM order defines tab order; every row stays a plain focusable button.
    for (const [name, button] of [
      ["first", items[0]],
      ["middle", items[600]],
      ["final", items[1199]],
    ] as const) {
      expect(button.disabled).toBe(false);
      button.focus();
      expect(document.activeElement).toBe(button);
      expect(button.getAttribute("aria-label")).toContain(`Play episode ${name === "first" ? "1" : name === "middle" ? "601" : "1200"}`);
    }
  });

  it("keeps focus on a row across re-renders of the long list", () => {
    const view = renderLong(1200);
    const list = document.querySelector(".detail-episode-list");
    const items = Array.from(list?.querySelectorAll<HTMLButtonElement>(".detail-episode") ?? []);
    const middle = items[600];
    middle.focus();
    expect(document.activeElement).toBe(middle);
    // Re-render with a watched-flag change elsewhere in the list: stable keys
    // and memoized rows must keep the focused node mounted.
    view.rerender(
      <Details
        {...props}
        anime={anime({ type: "tv" })}
        episodes={longEpisodes(1200)}
        state="ready"
        bookmarked={false}
        watched={{ e1: true }}
      />,
    );
    expect(document.activeElement).toBe(middle);
    expect(document.querySelectorAll(".detail-episode")).toHaveLength(1200);
  });

  it("filters the long list by number label and by title", () => {
    renderLong(1200);
    const search = screen.getByLabelText("Find or jump to an episode") as HTMLInputElement;
    // "12.5" must not match every "12x" label: fractional entries survive.
    fireEvent.change(search, { target: { value: "12.5" } });
    expect(screen.getAllByRole("listitem")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Play episode 12.5: Interlude half" })).toBeTruthy();

    fireEvent.change(search, { target: { value: "grand finale with a very long" } });
    expect(screen.getAllByRole("listitem")).toHaveLength(1);

    fireEvent.change(search, { target: { value: "zzz-no-match" } });
    expect(screen.queryByRole("listitem")).toBeNull();
    expect(screen.getByText("No episodes match your search.")).toBeTruthy();

    fireEvent.change(search, { target: { value: "" } });
    expect(screen.getAllByRole("listitem")).toHaveLength(1200);
  });

  it("keeps small lists simple without the search field", () => {
    render(
      <Details
        {...props}
        anime={anime({ type: "tv" })}
        episodes={[episode(1), episode(2), episode(3)]}
        state="ready"
        bookmarked={false}
      />,
    );
    expect(screen.queryByLabelText("Find or jump to an episode")).toBeNull();
    expect(screen.getAllByRole("listitem")).toHaveLength(3);
  });
});
