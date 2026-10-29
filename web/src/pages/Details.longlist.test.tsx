// @vitest-environment happy-dom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import Details from "./Details";
import type { DetailPageProps } from "../types";

afterEach(cleanup);
function episode(index: number, label?: string) {
  const whole = Math.floor(index);
  const fractional = index - whole;
  return {
    id: `ep-${index}`,
    animeId: "a1",
    label: label ?? (fractional > 0 ? `${whole}.5` : `${whole}`),
    title: `Episode ${whole}${fractional > 0 ? ".5" : ""} of a long show`,
    ordinal: index,
    availability: ["hianime"],
  };
}

const baseProps: DetailPageProps = {
  anime: {
    id: "a1",
    title: "Long show",
    type: "TV",
    synopsis: "A synopsis.",
  } as DetailPageProps["anime"],
  episodes: Array.from({ length: 1000 }, (_, index) => episode(index + 1)),
  state: "ready",
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

describe("long episode list", () => {
  it("renders a 1000-entry inventory with first, middle and final rows reachable", () => {
    render(<Details {...baseProps} />);
    const first = screen.getByRole("button", { name: /play episode 1:/i });
    const middle = screen.getByRole("button", { name: /play episode 500:/i });
    const last = screen.getByRole("button", { name: /play episode 1000:/i });
    expect(first).toBeTruthy();
    expect(middle).toBeTruthy();
    expect(last).toBeTruthy();
    // Keyboard reachability: each play control is a focusable button.
    for (const control of [first, middle, last]) {
      expect(control.getAttribute("disabled")).toBeNull();
    }
  });

  it("preserves fractional episodes and long labels in the rendered rows", () => {
    const episodes = [
      ...Array.from({ length: 8 }, (_, index) => episode(index + 1)),
      episode(9, "12.5"),
      { ...episode(10, "OVA"), title: "A very long episode title that keeps going and going past the row width" },
    ];
    render(<Details {...baseProps} episodes={episodes} />);
    expect(screen.getByRole("button", { name: /play episode 12\.5/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /a very long episode title that keeps going/i })).toBeTruthy();
  });

  it("jump-to-episode search narrows the list and clears back to full", () => {
    render(<Details {...baseProps} />);
    const search = screen.getByRole("searchbox") as HTMLInputElement;
    expect(search).toBeTruthy();

    fireEvent.change(search, { target: { value: "999" } });
    let rows = screen.getAllByRole("listitem");
    expect(rows.length).toBe(1);

    fireEvent.change(search, { target: { value: "" } });
    rows = screen.getAllByRole("listitem");
    expect(rows.length).toBe(1000);
  });

  it("shows an honest empty state when nothing matches the search", () => {
    render(<Details {...baseProps} />);
    const search = screen.getByRole("searchbox") as HTMLInputElement;
    fireEvent.change(search, { target: { value: "zzz-no-match" } });
    expect(screen.getByRole("status").textContent).toContain("No episodes match");
  });

  it("keeps small lists simple without a search field", () => {
    const episodes = Array.from({ length: 5 }, (_, index) => episode(index + 1));
    render(<Details {...baseProps} episodes={episodes} />);
    expect(screen.queryByRole("searchbox")).toBeNull();
    expect(screen.getAllByRole("listitem").length).toBe(5);
  });
});
