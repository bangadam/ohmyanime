import { describe, expect, it } from "vitest";
import type { Anime, Provider } from "./api";
import { filterAvailableProviders, isMovie, isSeries, providerAudioModes, sortByTitle } from "./utils";

const providers: Provider[] = [
  { id: "alpha", enabled: true, modes: ["sub"], status: "ok" },
  { id: "beta", enabled: true, modes: ["sub", "dub"], status: "ok" },
  { id: "gamma", enabled: false, modes: ["sub"], status: "disabled" },
];

describe("filterAvailableProviders", () => {
  it("keeps only enabled providers that list the episode", () => {
    const available = filterAvailableProviders(providers, ["alpha", "gamma", "delta"]);
    expect(available.map((item) => item.id)).toEqual(["alpha"]);
  });

  it("keeps every enabled provider when availability is unknown", () => {
    expect(filterAvailableProviders(providers, undefined).map((item) => item.id)).toEqual(["alpha", "beta"]);
    expect(filterAvailableProviders(providers, []).map((item) => item.id)).toEqual(["alpha", "beta"]);
  });

  it("returns nothing when no enabled provider carries the episode", () => {
    expect(filterAvailableProviders(providers, ["gamma"])).toEqual([]);
  });

  it("does not mutate the input list", () => {
    const input = [providers[0]];
    filterAvailableProviders(input, ["beta"]);
    expect(input).toHaveLength(1);
  });
});

describe("providerAudioModes", () => {
  it("prefers declared provider-level capabilities", () => {
    const provider: Provider = {
      id: "alpha",
      enabled: true,
      modes: ["sub", "dub"],
      status: "ok",
      capabilities: { kinds: ["anime"], audioModes: ["sub"], readerFormats: [] },
    };
    expect(providerAudioModes(provider)).toEqual(["sub"]);
  });

  it("falls back to the legacy modes list without capabilities", () => {
    expect(providerAudioModes(providers[1])).toEqual(["sub", "dub"]);
  });

  it("never infers an undeclared dub capability", () => {
    const provider: Provider = {
      id: "alpha",
      enabled: true,
      modes: ["sub"],
      status: "ok",
      capabilities: { kinds: ["anime"], audioModes: ["sub"], readerFormats: [] },
    };
    expect(providerAudioModes(provider)).not.toContain("dub");
  });
});

const anime = (type: string): Anime => ({ id: "a", title: "T", type, availability: [] });

describe("movie/series presentation", () => {
  it("classifies movies and films as movies", () => {
    expect(isMovie(anime("movie"))).toBe(true);
    expect(isMovie(anime("Film"))).toBe(true);
    expect(isMovie(anime("tv"))).toBe(false);
  });

  it("treats everything not a movie as a series", () => {
    expect(isSeries(anime("tv"))).toBe(true);
    expect(isSeries(anime("ova"))).toBe(true);
    expect(isSeries(anime("MOVIE"))).toBe(false);
  });

  it("sorts titles in both directions without mutating the input", () => {
    const items = [{ ...anime("tv"), title: "Zeta" }, { ...anime("tv"), title: "alpha" }, { ...anime("tv"), title: "Beta" }];
    const asc = sortByTitle(items, "asc").map((item) => item.title);
    expect(asc).toEqual(["alpha", "Beta", "Zeta"]);
    expect(sortByTitle(items, "desc").map((item) => item.title)).toEqual(["Zeta", "Beta", "alpha"]);
    expect(items[0].title).toBe("Zeta");
  });
});
