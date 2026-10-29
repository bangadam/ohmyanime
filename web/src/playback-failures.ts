import { APIError } from "./api";

export type PlaybackFailureKind = "media" | "provider" | "exhausted" | "timeout";

/**
 * The pipeline step a retry must target: "resolve" retries the resolve
 * request (no session exists yet, so nothing leaks), "manifest" reloads the
 * master playlist, "segment" resumes segment loading, and "session" rebuilds
 * playback. `null` means retrying cannot help.
 */
export type RetryStage = "resolve" | "manifest" | "segment" | "session";

export interface PlaybackFailure {
  kind: PlaybackFailureKind;
  stage: RetryStage | null;
  retryable: boolean;
}

/** Codes meaning the selected (or every) source lacks this episode. */
const ABSENT_CODES: Record<string, true> = {
  TITLE_ABSENT: true,
  INVALID_INPUT: true,
  NOT_FOUND: true,
  UNSUPPORTED: true,
};

/** Codes meaning the playback session itself is unusable. */
const SESSION_CODES: Record<string, true> = { SESSION_EXPIRED: true, RATE_LIMITED: true };

/** Codes meaning the source answered but the stream itself is broken. */
const MANIFEST_CODES: Record<string, true> = { MANIFEST_INVALID: true, INVALID_MEDIA: true };

/**
 * Maps a playback error onto the stable failure categories the backend
 * emits. A provider timeout and title absence are deliberately distinct:
 * the first is retryable against the same source, the second never is and
 * points the viewer at another provider.
 */
export function classifyPlaybackFailure(reason: unknown, effectiveProvider: string): PlaybackFailure {
  if (!(reason instanceof APIError)) {
    return { kind: "media", stage: "session", retryable: true };
  }
  const code = reason.code;
  if (code === "PROVIDER_TIMEOUT") {
    return { kind: "timeout", stage: "resolve", retryable: true };
  }
  if (ABSENT_CODES[code] === true) {
    return {
      kind: effectiveProvider === "auto" ? "exhausted" : "provider",
      stage: "resolve",
      retryable: false,
    };
  }
  if (SESSION_CODES[code] === true || reason.stage === "session") {
    return { kind: "media", stage: "session", retryable: true };
  }
  if (reason.stage === "manifest" || MANIFEST_CODES[code] === true) {
    return { kind: "media", stage: "manifest", retryable: true };
  }
  if (reason.stage === "segment" || code === "UPSTREAM_UNAVAILABLE") {
    return { kind: "media", stage: "segment", retryable: true };
  }
  if (code === "PROVIDER_RATE_LIMITED") {
    return { kind: "provider", stage: "resolve", retryable: true };
  }
  if (code === "PROVIDER_AUTH" || code === "PROVIDER_FORBIDDEN" || code === "MALFORMED_SOURCE") {
    return { kind: "provider", stage: null, retryable: false };
  }
  if (code === "PROVIDER_UNAVAILABLE") {
    return { kind: "provider", stage: "resolve", retryable: true };
  }
  return { kind: "media", stage: "session", retryable: true };
}

/**
 * Maps HLS fatal error details onto the retryable stages. Manifest load or
 * parse failures reload the playlist; anything else resumes segment loading.
 */
export function hlsFailureStage(details: string): RetryStage {
  return details.startsWith("manifest") || details.includes("manifestParsing") ? "manifest" : "segment";
}
