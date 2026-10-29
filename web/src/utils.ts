import type { Anime } from "./api";

export const SPEED_OPTIONS = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2] as const;

export function isAbortError(reason: unknown): boolean {
  return reason instanceof DOMException && reason.name === "AbortError";
}

export function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

/** h:mm:ss for hour-long media, m:ss otherwise. Unknown durations stay unknown. */
export function formatClock(seconds: number | null | undefined): string {
  if (seconds == null || !Number.isFinite(seconds) || seconds < 0) return "--:--";
  const total = Math.floor(seconds);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const secs = total % 60;
  if (hours > 0) return hours + ":" + String(minutes).padStart(2, "0") + ":" + String(secs).padStart(2, "0");
  return minutes + ":" + String(secs).padStart(2, "0");
}

export function percent(value: number, total: number | null | undefined): number {
  if (!total || !Number.isFinite(total) || total <= 0) return 0;
  return clamp((value / total) * 100, 0, 100);
}

/**
 * Removes explicit source-attribution annotations from catalogue synopsis text
 * while preserving story content and edition notes.
 */
export function cleanSynopsis(value?: string | null): string {
  if (!value) return "";
  return value
    .replace(/\s*\((?:Source|Sumber)\s*:[^)]*\)/gi, "")
    .replace(/^[\t ]*(?:Source|Sumber)\s*:[^\n]*(?:\n|$)/gim, "")
    .trim();
}

/**
 * Strips streaming-service attributions from track labels while keeping the
 * language/format label useful. Upstream labels can read like
 * "English (- (Crunchyroll))" and must not become visible provenance copy.
 */
export function cleanDisplayLabel(value: string): string {
  if (!value) return "";
  const services = /(?:Crunchyroll|Netflix|Hulu|Amazon|Prime Video|Disney\+?|Bilibili|VRV|HIDIVE|Funimation|AnimeLab|Wakanim|Muse Asia|Ani-One)/i;
  let cleaned = value;
  // Drop parentheticals naming a service, including nested wrappers such as
  // "English (- (Crunchyroll))".
  cleaned = cleaned.replace(/\(([^()]*)\)/g, (full, inner: string) => (services.test(inner) ? "" : full));
  cleaned = cleaned.replace(/\(\s*[-–—]?\s*\)/g, "");
  cleaned = cleaned.replace(/\s*[-–—]\s*(?:Crunchyroll|Netflix|Hulu|Amazon|Prime Video|Disney\+?|Bilibili|VRV|HIDIVE|Funimation|AnimeLab|Wakanim|Muse Asia|Ani-One)\s*$/gi, "");
  cleaned = cleaned
    .replace(/[\s(]+$/g, "")
    .replace(/^\s*[-–—]\s*/g, "")
    .replace(/\s*[-–—]\s*$/g, "")
    .trim();
  return cleaned || "Unknown language";
}

/** Disambiguates duplicate display labels without changing track identity. */
export function uniqueLabels(labels: string[]): string[] {
  const seen = new Map<string, number>();
  return labels.map((label) => {
    const count = (seen.get(label) ?? 0) + 1;
    seen.set(label, count);
    return count === 1 ? label : label + " (" + count + ")";
  });
}

export function metaLine(parts: Array<string | number | null | undefined>): string {
  return parts.filter((part) => part !== null && part !== undefined && String(part).trim() !== "").join(" · ");
}

const TYPE_LABELS: Record<string, string> = {
  tv: "Series",
  movie: "Movie",
  ova: "OVA",
  ona: "ONA",
  special: "Special",
  music: "Music",
};

const STATUS_LABELS: Record<string, string> = {
  finished: "Finished",
  releasing: "Airing",
  not_yet_released: "Upcoming",
  cancelled: "Cancelled",
  hiatus: "On hiatus",
};

function naturalCase(value: string): string {
  const lower = value.toLowerCase().replace(/[_-]+/g, " ").trim();
  return lower ? lower.charAt(0).toUpperCase() + lower.slice(1) : "";
}

export function animeTypeLabel(type?: string | null): string {
  if (!type) return "";
  return TYPE_LABELS[type.toLowerCase()] ?? naturalCase(type);
}

export function animeStatusLabel(status?: string | null): string {
  if (!status) return "";
  return STATUS_LABELS[status.toLowerCase()] ?? naturalCase(status);
}

export function seasonLabel(season?: string | null): string {
  return season ? naturalCase(season) : "";
}

export function episodeCountLabel(count: number): string {
  return count === 1 ? "1 episode" : count + " episodes";
}

/**
 * Provider titles often repeat the episode number ("1 To You Two Thousand Years
 * Later") while the UI already prints the badge, so strip a leading number that
 * matches this episode. Genuine numeric titles without a separator stay intact.
 */
export function cleanEpisodeTitle(title: string | undefined, label: string, ordinal: number): string {
  if (!title) return "";
  const candidates = [String(ordinal), label].filter((value, index, all) => Boolean(value) && all.indexOf(value) === index);
  for (const candidate of candidates) {
    const escaped = candidate.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const pattern = new RegExp("^\\s*(?:Episode\\s+)?" + escaped + "\\s*[.\\-–—:]?\\s+", "i");
    if (pattern.test(title)) return title.replace(pattern, "").trim() || title.trim();
  }
  return title.trim();
}

const WEEKDAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"] as const;
const WEEKDAYS_SHORT = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"] as const;

/** Local weekday index (0 = Sunday) for a unix-seconds airing time. */
export function localDay(airingAt: number): number {
  return new Date(airingAt * 1000).getDay();
}

export function weekdayName(index: number, short = false): string {
  return (short ? WEEKDAYS_SHORT : WEEKDAYS)[((index % 7) + 7) % 7];
}

export function formatLocalTime(airingAt: number): string {
  return new Date(airingAt * 1000).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}

export function formatLocalDate(airingAt: number): string {
  return new Date(airingAt * 1000).toLocaleDateString(undefined, { weekday: "long", month: "long", day: "numeric" });
}

export function formatHistoryDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

export function isSeries(anime: Anime): boolean {
  const type = (anime.type || "").toLowerCase();
  return type !== "movie" && type !== "film";
}

export function isMovie(anime: Anime): boolean {
  const type = (anime.type || "").toLowerCase();
  return type === "movie" || type === "film";
}

export function sortByTitle(items: Anime[], direction: "asc" | "desc"): Anime[] {
  const sorted = [...items].sort((a, b) => a.title.localeCompare(b.title, undefined, { sensitivity: "base" }));
  return direction === "desc" ? sorted.reverse() : sorted;
}

export function localTimeZoneLabel(): string {
  try {
    const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
    if (!zone) return "";
    return zone.replace(/_/g, " ");
  } catch {
    return "";
  }
}

export function focusElement(selector: string, options: FocusOptions = { preventScroll: true }) {
  window.requestAnimationFrame(() => {
    const element = document.querySelector<HTMLElement>(selector);
    element?.focus(options);
  });
}
