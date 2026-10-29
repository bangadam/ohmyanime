import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { APIError, api, errorMessage, id, initialize, json, mediaURL, sessionBearer } from "./api";
import { copy } from "./copy";

/* Guest browse-to-play flow: the web app talks to the API without an account,
   so requests must carry no bearer token, stay CSRF-safe on writes, and map
   every error shape to English interface copy. */

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const fetchMock = vi.fn<typeof fetch>();

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("api guest requests", () => {
  it("sends no Authorization header and unwraps data for a guest GET", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { data: { hello: "world" } }));
    const result = await api<{ hello: string }>("/capabilities");
    expect(result).toEqual({ hello: "world" });
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const headers = new Headers(init.headers);
    expect(headers.get("Authorization")).toBeNull();
    expect(init.credentials).toBe("omit");
  });

  it("attaches the CSRF token and JSON content type to writes", async () => {
    // initialize() performs its own /capabilities request first.
    fetchMock.mockResolvedValueOnce(
      jsonResponse(200, { data: { csrfToken: "csrf-1", desktop: false, providers: [] } }),
    );
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { data: { ok: true } }));
    await initialize();
    await api("/progress", { method: "POST", body: json({ positionSeconds: 1 }) });
    const [, init] = fetchMock.mock.calls.at(-1) as [string, RequestInit];
    const headers = new Headers(init.headers);
    expect(headers.get("X-CSRF-Token")).toBe("csrf-1");
    expect(headers.get("Content-Type")).toBe("application/json");
  });

  it("returns undefined for 204 responses", async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
    await expect(api("/bookmarks/x", { method: "DELETE" })).resolves.toBeUndefined();
  });

  it("throws APIError with mapped copy for error statuses", async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(404, { error: { code: "NOT_FOUND", message: "tidak ditemukan" } }),
    );
    const failure = (await api("/anime/missing").catch((reason: unknown) => reason)) as APIError;
    expect(failure).toBeInstanceOf(APIError);
    expect(failure.code).toBe("NOT_FOUND");
    expect(failure.status).toBe(404);
    expect(failure.message).toBe(copy.states.notFound);
  });

  it("falls back to REQUEST_FAILED copy when the body carries no code", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(500, {}));
    const failure = (await api("/catalogue").catch((reason: unknown) => reason)) as APIError;
    expect(failure.code).toBe("REQUEST_FAILED");
    expect(failure.message).toBe(copy.states.requestFailed);
  });

  it("reports OFFLINE when the network fails", async () => {
    fetchMock.mockRejectedValueOnce(new TypeError("fetch failed"));
    const failure = (await api("/catalogue").catch((reason: unknown) => reason)) as APIError;
    expect(failure).toBeInstanceOf(APIError);
    expect(failure.code).toBe("OFFLINE");
    expect(failure.retryable).toBe(true);
  });

  it("rethrows AbortError untouched", async () => {
    const abort = new DOMException("aborted", "AbortError");
    fetchMock.mockRejectedValueOnce(abort);
    await expect(api("/catalogue", { signal: new AbortController().signal })).rejects.toBe(abort);
  });
});

describe("initialize", () => {
  it("captures capabilities for the guest session", async () => {
    const capabilities = {
      csrfToken: "token-1",
      desktop: false,
      providers: [{ id: "prov", enabled: true, modes: ["sub"], status: "ok" }],
    };
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { data: capabilities }));
    const result = await initialize();
    expect(result).toEqual(capabilities);
    // The captured CSRF token is reused on the next write.
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
    await api("/settings", { method: "PUT", body: "{}" });
    const [, init] = fetchMock.mock.calls.at(-1) as [string, RequestInit];
    expect(new Headers(init.headers).get("X-CSRF-Token")).toBe("token-1");
  });
});

describe("errorMessage", () => {
  it("maps known provider codes to playable-error copy", () => {
    for (const code of ["PROVIDER_UNAVAILABLE", "MALFORMED_SOURCE", "UPSTREAM_UNAVAILABLE", "MANIFEST_INVALID"]) {
      expect(errorMessage(new APIError(code, "raw", 502))).toBe(copy.states.playableError);
    }
  });

  it("keeps timeout and title absence distinct from generic failures", () => {
    expect(errorMessage(new APIError("PROVIDER_TIMEOUT", "raw", 504))).toBe(copy.player.sourceTimeout);
    expect(errorMessage(new APIError("PROVIDER_RATE_LIMITED", "raw", 429))).toBe(copy.states.rateLimited);
    expect(errorMessage(new APIError("TITLE_ABSENT", "raw", 404))).toBe(copy.player.providerUnavailable);
  });

  it("maps security codes to the blocked-request message", () => {
    for (const code of ["CSRF_DENIED", "ORIGIN_DENIED"]) {
      expect(errorMessage(new APIError(code, "raw", 403))).toBe(
        "This request was blocked. Reload the page and try again.",
      );
    }
  });

  it("passes the fallback through for unknown codes and non-API errors", () => {
    expect(errorMessage(new APIError("SOMETHING_ELSE", "raw", 400), "fallback")).toBe("fallback");
    expect(errorMessage(new Error("plain"), "fallback")).toBe("fallback");
    expect(errorMessage(new Error("plain"))).toBe(copy.states.requestFailed);
  });
});

describe("helpers", () => {
  it("encodes identifiers for paths", () => {
    expect(id("a/b c")).toBe("a%2Fb%20c");
  });

  it("builds media URLs against the API base", () => {
    // In tests the bootstrap apiBase is empty, so the URL resolves against
    // a stubbed origin exactly as it does in the browser.
    vi.stubGlobal("location", { origin: "http://localhost:4310" });
    expect(mediaURL("/media/x.jpg")).toBe("http://localhost:4310/media/x.jpg");
    vi.unstubAllGlobals();
  });

  it("exposes the session bearer", () => {
    expect(typeof sessionBearer()).toBe("string");
  });
});
