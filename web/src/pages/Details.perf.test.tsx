// @vitest-environment happy-dom
// Issue #22 measurement harness — documents why the episode list is not
// virtualized. Numbers below are from this machine (arm64, happy-dom); real
// Chromium compositing is faster. Mount cost is linear and paid once, so we
// keep the simple full list and memoize rows (see Details.tsx EpisodeRow).
//
// Measured 2026-10-29 (happy-dom, vitest 5.0.5):
//   1000 rows: render ~467ms, unmount ~65ms
//   1500 rows: render ~694ms, unmount ~82ms
//   3000 rows: render ~1245ms, unmount ~199ms
//
// Run manually with:
//   bunx vitest run src/pages/Details.perf.test.tsx --silent=false
import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { Anime, Episode } from "../api";
import type { DetailPageProps } from "../types";
import Details from "./Details";

function episode(ordinal: number): Episode {
  return {
    id: `e${ordinal}`,
    animeId: "a1",
    label: String(ordinal),
    title: `Story ${ordinal} — a fairly long episode title to stress the label column`,
    ordinal,
    availability: ["alpha"],
  };
}

const anime: Anime = {
  id: "a1",
  title: "Long Series",
  synopsis: "Syn",
  availability: ["alpha"],
};

afterEach(cleanup);

describe("episode list render measurement", () => {
  it.each([1000, 1500, 3000])("measures render of %i rows", (count) => {
    const episodes: Episode[] = Array.from({ length: count }, (_, i) => episode(i + 1));
    const props: DetailPageProps = {
      anime,
      episodes,
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
    const t0 = performance.now();
    const { unmount } = render(<Details {...props} />);
    const t1 = performance.now();
    console.log(`[perf] render ${count} rows: ${(t1 - t0).toFixed(1)}ms`);
    expect(document.querySelectorAll(".detail-episode").length).toBe(count);
    const t2 = performance.now();
    unmount();
    const t3 = performance.now();
    console.log(`[perf] unmount ${count} rows: ${(t3 - t2).toFixed(1)}ms`);
  });
});
