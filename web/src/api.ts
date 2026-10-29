import { call, isMyGo } from "mygo-runtime";
import { copy } from "./copy";

export interface Anime {
  id: string;
  title: string;
  alternativeTitles?: string[];
  posterUrl?: string;
  synopsis?: string;
  type?: string;
  season?: string;
  year?: number;
  status?: string;
  genres?: string[];
  tags?: string[];
  availability: string[];
}
export interface Episode {
  id: string;
  animeId: string;
  label: string;
  title?: string;
  ordinal: number;
  availability: string[];
}
export interface Page<T> {
  items: T[];
  nextCursor: string | null;
  /** True when the server served last-known data during an upstream outage. */
  stale?: boolean;
}
export interface Manga {
  id: string; // "mangadex:<uuid>"
  title: string;
  alternativeTitles?: string[];
  posterUrl?: string;
  synopsis?: string;
  type?: string; // Manga / Manhwa / Manhua / ...
  year?: number;
  status?: string; // ongoing | completed | hiatus | cancelled | unknown
  tags?: string[];
  author?: string;
  chapterCount?: number;
  availability: string[]; // ["mangadex"]
}
export interface MangaChapter {
  id: string; // "mangadex:<uuid>"
  mangaId: string;
  label: string; // "Ch. 5" / "Ch. 5.5" / "Oneshot"
  title?: string; // chapter title if any
  ordinal: number;
  // Identity dimensions from the backend chapter key; they keep the reader's
  // ordering aligned with the server sort (numeric, volume, special, language).
  number?: string; // "5" / "5.5" / "" for specials
  volume?: string;
  special?: string;
  language: string;
  pageCount: number;
  publishedAt?: string;
  groupName?: string;
}
export interface MangaChapterPage {
  items: MangaChapter[];
  nextCursor?: string | null;
}
export interface MangaPage {
  index: number;
  url: string;
  width?: number;
  height?: number;
}
export const qualityOptions = ["auto", "1080p", "720p", "480p", "360p"] as const;
export interface Settings {
  subtitleLanguage: string;
  quality: (typeof qualityOptions)[number];
  autoplay: boolean;
  preferredProvider: string;
  revision: number;
}
export interface Progress {
  episodeId: string;
  positionSeconds: number;
  durationSeconds: number | null;
  mode: string;
  completed: boolean;
  revision: number;
  updatedAt: string;
}
export interface HistoryItem {
  anime: Anime;
  episode: Episode;
  progress: Progress;
  provider: string;
  mode: string;
  lastWatchedAt: string;
}
export interface ProviderCapabilities {
  kinds: string[];
  audioModes: string[];
  readerFormats: string[];
}
export interface Provider {
  id: string;
  enabled: boolean;
  modes: string[];
  status: string;
  capabilities?: ProviderCapabilities;
  health?: ProviderHealth;
}
export interface ProviderHealth {
  state: "unknown" | "reachable" | "degraded" | "unavailable";
  checkedAt?: string;
  reason?: string;
}

export interface DiagnosticsErrorCategory {
  category: string;
  count: number;
}

/**
 * Safe troubleshooting summary: app version, OS, run mode, provider
 * identifiers and recent redacted error categories. Never contains
 * cookies, tokens, signed media URLs or personal history.
 */
export interface DiagnosticsReport {
  version: string;
  os: string;
  mode: string;
  providers: string[];
  recentErrors: DiagnosticsErrorCategory[];
}

/** Fetches the diagnostics report. Nothing is shared until the user copies it. */
export async function fetchDiagnostics(): Promise<DiagnosticsReport> {
  return api<DiagnosticsReport>("/diagnostics");
}

/** Opens the host file manager at the log folder, best effort. */
export async function openLogFolder(): Promise<void> {
  await api<{ opened: boolean }>("/diagnostics/open-log-folder", { method: "POST" });
}
export interface Subtitle {
  id: string;
  label: string;
  language: string;
  url: string;
  default: boolean;
}
export interface Interval {
  start: number;
  end: number;
}
export interface PlaybackSession {
  id: string;
  masterUrl: string;
  actualProvider: string;
  actualMode: string;
  variants: { id: string; label: string; height?: number; url?: string }[];
  subtitles: Subtitle[];
  expiresAt: string;
  intro?: Interval;
  outro?: Interval;
  hardsub?: boolean;
  hardsubLanguage?: string;
}
export interface Bootstrap {
  apiBase: string;
  apiToken: string;
  csrfToken: string;
  desktop: boolean;
}
let bootstrap: Bootstrap = { apiBase: "", apiToken: "", csrfToken: "", desktop: false };
export class APIError extends Error {
  constructor(
    public code: string,
    message: string,
    public status: number,
    public retryable = false,
    /** Backend pipeline stage that failed, when the endpoint reports one. */
    public stage?: string,
  ) {
    super(message);
    this.name = "APIError";
  }
}
export function sessionBearer(): string {
  return bootstrap.apiToken;
}

export interface Capabilities {
  csrfToken: string;
  desktop: boolean;
  providers: Provider[];
}

export function mediaURL(path: string) {
  return new URL(path, bootstrap.apiBase || location.origin).href;
}
export function isDesktop() {
  return bootstrap.desktop;
}
export async function initialize(): Promise<Capabilities> {
  if (isMyGo()) bootstrap = (await call("Bootstrap.Get")) as Bootstrap;
  const capabilities = await api<Capabilities>("/capabilities");
  bootstrap.csrfToken = capabilities.csrfToken;
  bootstrap.desktop = capabilities.desktop;
  return capabilities;
}

/**
 * Manual provider health refresh: one bounded homepage probe per provider.
 * Reachability is observed evidence only — it is not a playback guarantee.
 */
export async function refreshProvidersHealth(): Promise<Provider[]> {
  const data = await api<{ items: Provider[] }>("/providers/health", { method: "POST" });
  return data.items;
}


/**
 * Maps API codes to English copy. The server can return Indonesian fallback
 * messages, so raw server text is never rendered in the interface.
 */
export function errorMessage(reason: unknown, fallback: string = copy.states.requestFailed): string {
  if (!(reason instanceof APIError)) return fallback;
  switch (reason.code) {
    case "CATALOG_OFFLINE":
      return copy.states.offlineCatalogue;
    case "OFFLINE":
      return copy.states.offline;
    case "CSRF_DENIED":
    case "ORIGIN_DENIED":
      return "This request was blocked. Reload the page and try again.";
    case "CONFLICT":
    case "INVALID_REVISION":
      return copy.states.conflict;
    case "CONFIRM_REQUIRED":
      return copy.states.confirmRequired;
    case "PROVIDER_UNAVAILABLE":
    case "PROVIDER_AUTH":
    case "PROVIDER_FORBIDDEN":
    case "MALFORMED_SOURCE":
    case "UPSTREAM_UNAVAILABLE":
    case "MANIFEST_INVALID":
      return copy.states.playableError;
    case "PROVIDER_TIMEOUT":
      return copy.player.sourceTimeout;
    case "PROVIDER_RATE_LIMITED":
      return copy.states.rateLimited;
    case "TITLE_ABSENT":
      return copy.player.providerUnavailable;
    case "RATE_LIMITED":
      return copy.states.rateLimited;
    case "NOT_FOUND":
      return copy.states.notFound;
    case "INVALID_INPUT":
      return copy.states.invalidInput;
    case "INVALID_SUBTITLE":
      return copy.states.subtitleInvalid;
    case "INVALID_MEDIA":
      return copy.states.mediaError;
    case "UNSUPPORTED":
      return copy.states.unsupported;
    case "SESSION_LIMIT":
      return copy.states.sessionLimit;
    case "TLS_REQUIRED":
    case "INVALID_HOST":
      return copy.states.secureConnection;
    case "DENIED":
      return copy.states.denied;
    case "REQUEST_CANCELED":
      return fallback;
    default:
      return fallback;
  }
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  const method = init.method || "GET";
  const bearer = sessionBearer();
  if (bearer) headers.set("Authorization", "Bearer " + bearer);
  if (!["GET", "HEAD"].includes(method)) headers.set("X-CSRF-Token", bootstrap.csrfToken);
  if (init.body && !(init.body instanceof FormData)) headers.set("Content-Type", "application/json");
  let response: Response;
  try {
    response = await fetch(bootstrap.apiBase + "/api/v1" + path, {
      ...init,
      headers,
      credentials: "omit",
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") throw error;
    throw new APIError("OFFLINE", copy.states.offline, 0, true);
  }
  if (response.status === 204) return undefined as T;
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    const code = body?.error?.code || "REQUEST_FAILED";
    throw new APIError(
      code,
      errorMessage(new APIError(code, "", response.status, Boolean(body?.error?.retryable))),
      response.status,
      Boolean(body?.error?.retryable),
      body?.error?.stage,
    );
  }
  return body.data as T;
}
export const id = encodeURIComponent;
export const json = (value: unknown) => JSON.stringify(value);

/**
 * Chapter list for one manga. The server returns the canonical order
 * (numeric chapter, volume, special, language); callers must preserve it.
 */
export async function mangaChapters(
  mangaId: string,
  init: RequestInit = {},
): Promise<MangaChapter[]> {
  const page = await api<MangaChapterPage>(`/manga/${id(mangaId)}/chapters`, init);
  return page.items ?? [];
}

export interface ReadingProgress {
  mangaId: string;
  chapterId: string;
  page: number;
  completed: boolean;
  revision: number;
  updatedAt: string;
}

