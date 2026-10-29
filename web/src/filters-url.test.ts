import { describe, expect, it } from "vitest";
import type { CatalogFilters } from "./types";
import {
  activeFilterCount,
  catalogFiltersKey,
  defaultCatalogFilters,
  parseCatalogHash,
  serializeCatalogHash,
} from "./filters-url";

function filters(overrides: Partial<CatalogFilters> = {}): CatalogFilters {
  return { ...defaultCatalogFilters(), ...overrides };
}

describe("defaultCatalogFilters", () => {
  it("returns the unfiltered catalogue", () => {
    expect(defaultCatalogFilters()).toEqual({
      query: "",
      status: "all",
      type: "all",
      sort: "popular",
      genres: [],
      tags: [],
      season: "",
      year: 0,
    });
  });

  it("returns fresh arrays so callers cannot share state", () => {
    const first = defaultCatalogFilters();
    first.genres.push("Action");
    expect(defaultCatalogFilters().genres).toEqual([]);
  });
});

describe("serializeCatalogHash", () => {
  it("returns an empty string when only defaults are active", () => {
    expect(serializeCatalogHash(defaultCatalogFilters())).toBe("");
  });

  it("writes every non-default key in a fixed order", () => {
    const hash = serializeCatalogHash({
      query: "a",
      status: "releasing",
      type: "tv",
      sort: "score",
      genres: ["Action"],
      tags: ["Isekai"],
      season: "winter",
      year: 2024,
    });
    expect(hash).toBe("#browse?query=a&status=releasing&type=tv&sort=score&season=winter&year=2024&genre=Action&tag=Isekai");
  });

  it("keeps the same hash whatever order genres and tags were selected in", () => {
    const one = serializeCatalogHash(filters({ genres: ["Shounen", "Action"], tags: ["Isekai", "Drama"] }));
    const two = serializeCatalogHash(filters({ genres: ["Action", "Shounen"], tags: ["Drama", "Isekai"] }));
    expect(one).toBe(two);
    expect(one).toContain("genre=Action%2CShounen");
    expect(one).toContain("tag=Drama%2CIsekai");
  });

  it("skips empty facets and a zero year", () => {
    expect(serializeCatalogHash(filters({ genres: [], tags: [], year: 0, season: "" }))).toBe("");
  });

  it("does not mutate the input arrays", () => {
    const input = filters({ genres: ["Shounen", "Action"] });
    serializeCatalogHash(input);
    expect(input.genres).toEqual(["Shounen", "Action"]);
  });

  it("trims blanks so a whitespace-only query is not a filter", () => {
    expect(serializeCatalogHash(filters({ query: "   " }))).toBe("");
  });
});

describe("parseCatalogHash", () => {
  it("returns null for an empty hash", () => {
    expect(parseCatalogHash("")).toBeNull();
    expect(parseCatalogHash("   ")).toBeNull();
  });

  it("returns null for another route", () => {
    expect(parseCatalogHash("#search?query=naruto")).toBeNull();
    expect(parseCatalogHash("#browse-extra?query=naruto")).toBeNull();
    expect(parseCatalogHash("browse?query=naruto")).toBeNull();
  });

  it("accepts the bare browse hash as defaults", () => {
    expect(parseCatalogHash("#browse")).toEqual(defaultCatalogFilters());
  });

  it("round-trips a fully populated filter set", () => {
    const original = filters({
      query: "demon slayer",
      status: "finished",
      type: "tv_short",
      sort: "title_desc",
      genres: ["Action", "Comedy"],
      tags: ["Isekai"],
      season: "fall",
      year: 2019,
    });
    expect(parseCatalogHash(serializeCatalogHash(original))).toEqual(original);
  });

  it("falls back per field on unknown enum values", () => {
    const parsed = parseCatalogHash("#browse?status=airing&type=series&sort=random&query=keep");
    expect(parsed).toEqual(filters({ query: "keep" }));
  });

  it("reads comma-separated and repeated facets", () => {
    expect(parseCatalogHash("#browse?genre=Action%2CComedy&tag=Isekai")?.genres).toEqual(["Action", "Comedy"]);
    expect(parseCatalogHash("#browse?genre=Action&genre=Comedy")?.genres).toEqual(["Action", "Comedy"]);
    expect(parseCatalogHash("#browse?genre=Action%2C%20Action&tag=")?.genres).toEqual(["Action"]);
    expect(parseCatalogHash("#browse?tag=")?.tags).toEqual([]);
  });

  it("decodes percent-encoded queries", () => {
    expect(parseCatalogHash("#browse?query=demon%20slayer")?.query).toBe("demon slayer");
  });

  it("parses years safely", () => {
    const cases: Array<[string, number]> = [
      ["#browse?year=2024", 2024],
      ["#browse?year=0", 0],
      ["#browse?year=abcd", 0],
      ["#browse?year=-5", 0],
      ["#browse?year=20245", 0],
      ["#browse?year=", 0],
      ["#browse", 0],
    ];
    for (const [hash, year] of cases) {
      expect(parseCatalogHash(hash)?.year, hash).toBe(year);
    }
  });

  it("never throws on malformed input", () => {
    expect(() => parseCatalogHash("?")).not.toThrow();
    expect(parseCatalogHash("#browse?%")).toEqual(defaultCatalogFilters());
  });
});

describe("catalogFiltersKey", () => {
  it("is stable for the same filters", () => {
    expect(catalogFiltersKey(filters({ query: "a", genres: ["Action"] }))).toBe(
      catalogFiltersKey(filters({ query: "a", genres: ["Action"] })),
    );
  });

  it("ignores facet order and duplicates", () => {
    expect(catalogFiltersKey(filters({ genres: ["Shounen", "Action"], tags: ["Isekai"] }))).toBe(
      catalogFiltersKey(filters({ genres: ["Action", "Shounen", "Action"], tags: ["Isekai"] })),
    );
  });

  it("changes when any field changes", () => {
    const base = catalogFiltersKey(defaultCatalogFilters());
    expect(catalogFiltersKey(filters({ sort: "newest" }))).not.toBe(base);
    expect(catalogFiltersKey(filters({ year: 2024 }))).not.toBe(base);
    expect(catalogFiltersKey(filters({ type: "ova" }))).not.toBe(base);
    expect(catalogFiltersKey(filters({ season: "winter" }))).not.toBe(base);
  });
});

describe("activeFilterCount", () => {
  const cases: Array<[string, CatalogFilters, number]> = [
    ["defaults", defaultCatalogFilters(), 0],
    ["sort only", filters({ sort: "score" }), 0],
    ["query", filters({ query: "naruto" }), 1],
    ["blank query", filters({ query: "  " }), 0],
    ["status", filters({ status: "releasing" }), 1],
    ["type", filters({ type: "movie" }), 1],
    ["season", filters({ season: "winter" }), 1],
    ["year", filters({ year: 2024 }), 1],
    ["genres", filters({ genres: ["Action", "Comedy"] }), 2],
    ["tags", filters({ tags: ["Isekai"] }), 1],
    ["duplicate facets", filters({ genres: ["Action", "Action"] }), 1],
    [
      "everything except sort",
      filters({
        query: "a",
        status: "hiatus",
        type: "ona",
        sort: "updated",
        season: "summer",
        year: 2020,
        genres: ["Action", "Drama"],
        tags: ["Isekai"],
      }),
      8,
    ],
  ];

  for (const [name, value, expected] of cases) {
    it(`counts ${name}`, () => {
      expect(activeFilterCount(value)).toBe(expected);
    });
  }
});
