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
export interface Provider {
  id: string;
  enabled: boolean;
  modes: string[];
  status: string;
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
  ) {
    super(message);
    this.name = "APIError";
  }
}
export function setCSRF(token: string) {
  bootstrap.csrfToken = token;
}

/**
 * The desktop webview runs on mygo:// and cannot share the web session cookie,
 * so after Google sign-in the shell keeps the session token here and sends it
 * as a bearer credential. The bootstrap token is only a capability: it starts
 * and claims the sign-in flow and is never accepted as a session.
 */
let sessionToken = "";
export function setSessionToken(token: string) {
  sessionToken = token;
}
export function sessionBearer(): string {
  return sessionToken;
}

export interface SessionInfo {
  authenticated: boolean;
  csrfToken: string;
  desktop: boolean;
  email?: string;
  capability?: boolean;
  google: { configured: boolean };
}

export interface DesktopSignIn {
  url: string;
  handoff: string;
  pollToken: string;
}

export interface ClaimResult {
  authenticated?: boolean;
  pending?: boolean;
  apiToken?: string;
  csrfToken?: string;
  email?: string;
}

/**
 * Desktop sign-in starts with the bootstrap capability, which only authorizes
 * the flow endpoints. These calls bypass `api()` so the capability never leaks
 * into ordinary requests.
 */
async function flow<T>(path: string, body?: unknown): Promise<T> {
  const headers = new Headers({ "X-CSRF-Token": bootstrap.csrfToken });
  if (bootstrap.apiToken) headers.set("Authorization", "Bearer " + bootstrap.apiToken);
  if (body !== undefined) headers.set("Content-Type", "application/json");
  const response = await fetch(bootstrap.apiBase + "/api/v1" + path, {
    method: body === undefined ? "GET" : "POST",
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: "omit",
  });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    const code = payload?.error?.code || "REQUEST_FAILED";
    throw new APIError(code, errorMessage(new APIError(code, "", response.status)), response.status);
  }
  return payload.data as T;
}

export function desktopSignIn(): Promise<DesktopSignIn> {
  return flow<DesktopSignIn>("/auth/google/start");
}

export function claimDesktopSignIn(handoff: string, pollToken: string): Promise<ClaimResult> {
  return flow<ClaimResult>("/auth/google/claim", { handoff, pollToken });
}

export function googleSignInURL(returnTo: string): string {
  const base = bootstrap.apiBase || location.origin;
  return base + "/api/v1/auth/google/start?return=" + encodeURIComponent(returnTo);
}

export function mediaURL(path: string) {
  return new URL(path, bootstrap.apiBase || location.origin).href;
}
export function isDesktop() {
  return bootstrap.desktop;
}
export async function initialize() {
  if (isMyGo()) bootstrap = (await call("Bootstrap.Get")) as Bootstrap;
  return bootstrap;
}

/**
 * Maps API codes to English copy. The server can return Indonesian fallback
 * messages, so raw server text is never rendered in the interface.
 */
export function errorMessage(reason: unknown, fallback: string = copy.states.requestFailed): string {
  if (!(reason instanceof APIError)) return fallback;
  switch (reason.code) {
    case "OFFLINE":
      return copy.states.offline;
    case "AUTH_NOT_CONFIGURED":
      return copy.auth.notConfigured;
    case "DENIED":
      return copy.auth.notAllowed;
    case "UNAUTHORIZED":
    case "SESSION_EXPIRED":
      return copy.auth.sessionEnded;
    case "CSRF_DENIED":
    case "ORIGIN_DENIED":
      return "This request was blocked. Reload the page and try again.";
    case "CONFLICT":
    case "INVALID_REVISION":
      return copy.states.conflict;
    case "CONFIRM_REQUIRED":
      return copy.states.confirmRequired;
    case "PROVIDER_UNAVAILABLE":
    case "PROVIDER_TIMEOUT":
    case "PROVIDER_RATE_LIMITED":
    case "PROVIDER_AUTH":
    case "PROVIDER_FORBIDDEN":
    case "MALFORMED_SOURCE":
      return copy.states.playableError;
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
      credentials: bootstrap.desktop ? "omit" : "same-origin",
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") throw error;
    throw new APIError("OFFLINE", copy.states.offline, 0, true);
  }
  if (response.status === 204) return undefined as T;
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    if (response.status === 401) window.dispatchEvent(new Event("myanime:unauthorized"));
    const code = body?.error?.code || "REQUEST_FAILED";
    throw new APIError(code, errorMessage(new APIError(code, "", response.status, Boolean(body?.error?.retryable))), response.status, Boolean(body?.error?.retryable));
  }
  return body.data as T;
}
export const id = encodeURIComponent;
export const json = (value: unknown) => JSON.stringify(value);
