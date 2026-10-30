import type { CatalogFilters, CatalogSort, CatalogStatus, CatalogType } from "./types";

/**
 * Catalogue filter state <-> `#browse?…` URL hash. Keeping this module free of
 * React and side effects lets the router and the forms share one definition of
 * what a filter URL means.
 */

const HASH_PREFIX = "#browse";

const STATUS_VALUES: readonly CatalogStatus[] = [
  "all",
  "releasing",
  "finished",
  "not_yet_released",
  "cancelled",
  "hiatus",
];
const TYPE_VALUES: readonly CatalogType[] = ["all", "tv", "tv_short", "movie", "special", "ova", "ona", "music"];
const SORT_VALUES: readonly CatalogSort[] = [
  "popular",
  "trending",
  "score",
  "newest",
  "updated",
  "title_asc",
  "title_desc",
];
const SEASON_VALUES: readonly string[] = ["winter", "spring", "summer", "fall"];

export function defaultCatalogFilters(): CatalogFilters {
  return {
    query: "",
    status: "all",
    type: "all",
    sort: "popular",
    genres: [],
    tags: [],
    season: "",
    year: 0,
  };
}

function pickEnum<T extends string>(value: string | null, allowed: readonly T[], fallback: T): T {
  if (value === null) return fallback;
  const candidate = value.trim() as T;
  return allowed.includes(candidate) ? candidate : fallback;
}

/** Trims, drops blanks and removes duplicates while preserving the given order. */
function normalizeList(values: readonly string[]): string[] {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const value of values) {
    const normalized = value.trim();
    if (normalized === "" || seen.has(normalized)) continue;
    seen.add(normalized);
    result.push(normalized);
  }
  return result;
}

/** Facet params accept both repeats (`genre=a&genre=b`) and comma lists (`genre=a,b`). */
function readFacets(params: URLSearchParams, key: string): string[] {
  const values: string[] = [];
  for (const raw of params.getAll(key)) {
    for (const part of raw.split(",")) values.push(part);
  }
  return normalizeList(values);
}

/** A year outside four digits is a typo, not a filter; treat it as "any". */
function parseYear(raw: string | null): number {
  if (raw === null) return 0;
  const value = raw.trim();
  if (!/^\d{1,4}$/.test(value)) return 0;
  return Math.max(Number(value), 0);
}

/**
 * Stable serialization of every filter field, including sort. Used as an
 * effect dependency, so equal filters must always produce the same string
 * regardless of the order genres and tags were selected in.
 */
export function catalogFiltersKey(filters: CatalogFilters): string {
  return JSON.stringify([
    filters.query.trim(),
    filters.status,
    filters.type,
    filters.sort,
    filters.season.trim(),
    filters.year,
    normalizeList(filters.genres).sort(),
    normalizeList(filters.tags).sort(),
  ]);
}

/**
 * Parses a `#browse?…` hash. Foreign hashes (other routes, empty) yield null;
 * malformed values inside a browse hash fall back per field so a hand-edited
 * URL can never throw or blank the page.
 */
export function parseCatalogHash(hash: string): CatalogFilters | null {
  const trimmed = hash.trim();
  if (trimmed !== HASH_PREFIX && !trimmed.startsWith(`${HASH_PREFIX}?`)) return null;
  const queryIndex = trimmed.indexOf("?");
  const search = queryIndex === -1 ? "" : trimmed.slice(queryIndex + 1);
  let params: URLSearchParams;
  try {
    params = new URLSearchParams(search);
  } catch {
    return defaultCatalogFilters();
  }
  return {
    query: (params.get("query") ?? "").trim(),
    status: pickEnum(params.get("status"), STATUS_VALUES, "all"),
    type: pickEnum(params.get("type"), TYPE_VALUES, "all"),
    sort: pickEnum(params.get("sort"), SORT_VALUES, "popular"),
    genres: readFacets(params, "genre"),
    tags: readFacets(params, "tag"),
    // An unknown season is a hand-edited URL, not a filter the API accepts;
    // drop it here so a reload restores a working view instead of a 400.
    season: pickEnum((params.get("season") ?? "").trim() || null, SEASON_VALUES, ""),
    year: parseYear(params.get("year")),
  };
}

/**
 * Serializes filters into `#browse?…`, or "" when only defaults are active so
 * the address bar stays clean on the unfiltered catalogue. Keys are written in
 * a fixed order; sorted facet lists keep the URL identical for identical
 * selections.
 */
export function serializeCatalogHash(filters: CatalogFilters): string {
  const params = new URLSearchParams();
  const query = filters.query.trim();
  if (query !== "") params.set("query", query);
  if (filters.status !== "all") params.set("status", filters.status);
  if (filters.type !== "all") params.set("type", filters.type);
  if (filters.sort !== "popular") params.set("sort", filters.sort);
  const season = filters.season.trim();
  if (season !== "") params.set("season", season);
  if (filters.year > 0) params.set("year", String(filters.year));
  const genres = normalizeList(filters.genres).sort();
  if (genres.length > 0) params.set("genre", genres.join(","));
  const tags = normalizeList(filters.tags).sort();
  if (tags.length > 0) params.set("tag", tags.join(","));
  const search = params.toString();
  return search === "" ? "" : `${HASH_PREFIX}?${search}`;
}

/**
 * Badge count for "active filters". Sort is excluded: it changes the order,
 * not which titles match, and counting it would show a filter nobody can clear.
 */
export function activeFilterCount(filters: CatalogFilters): number {
  let count = 0;
  if (filters.query.trim() !== "") count += 1;
  if (filters.status !== "all") count += 1;
  if (filters.type !== "all") count += 1;
  if (filters.season.trim() !== "") count += 1;
  if (filters.year > 0) count += 1;
  count += normalizeList(filters.genres).length;
  count += normalizeList(filters.tags).length;
  return count;
}
