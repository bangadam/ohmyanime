// @vitest-environment happy-dom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import Preferences from "./Preferences";
import type { PreferencesPageProps } from "../types";
import type { Provider, Settings } from "../api";
import type * as ApiModule from "../api";

vi.mock("../api", async (importOriginal) => {
  const actual = await importOriginal<typeof ApiModule>();
  return {
    ...actual,
    fetchDiagnostics: vi.fn(),
    openLogFolder: vi.fn(),
  };
});

import { APIError, fetchDiagnostics, openLogFolder } from "../api";

afterEach(cleanup);

const settings: Settings = {
  subtitleLanguage: "en",
  quality: "auto",
  autoplay: false,
  preferredProvider: "",
  revision: 0,
};

const providers: Provider[] = [
  {
    id: "hianime",
    enabled: true,
    modes: ["sub", "dub"],
    status: "unverified",
    capabilities: { kinds: ["anime"], audioModes: ["sub", "dub"], readerFormats: [] },
    health: { state: "unknown" },
  },
];

function props(): PreferencesPageProps {
  return {
    settings,
    providers,
    healthBusy: false,
    healthError: "",
    onRefreshProviderHealth: () => {},
    theme: "light",
    sessionSpeed: 1,
    busy: false,
    loading: false,
    saveError: "",
    onTheme: () => {},
    onSave: () => {},
    onReset: () => {},
    onReload: () => {},
    onSpeedChange: () => {},
    onClearHistory: () => {},
    onClearList: () => {},
    onClearCache: () => {},
  };
}

function stubClipboard(text: string[] = []) {
  const writeText = vi.fn(async (value: string) => {
    text.push(value);
  });
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText },
    configurable: true,
  });
  return writeText;
}

describe("Preferences diagnostics", () => {
  it("explains exactly what the report includes and excludes", () => {
    render(<Preferences {...props()} />);
    expect(
      screen.getAllByText(/never includes cookies, sign-in tokens, signed media links/i).length,
    ).toBeGreaterThan(0);
  });

  it("copies only after a deliberate click, never on render", async () => {
    const writeText = stubClipboard();
    vi.mocked(fetchDiagnostics).mockResolvedValue({
      version: "dev",
      os: "darwin arm64",
      mode: "desktop",
      providers: ["hianime"],
      recentErrors: [{ category: "request_failed", count: 2 }],
    });
    render(<Preferences {...props()} />);
    expect(fetchDiagnostics).not.toHaveBeenCalled();
    expect(writeText).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Copy diagnostics" }));
    await waitFor(() => {
      expect(screen.getByText("Diagnostics copied.")).toBeTruthy();
    });
    expect(writeText).toHaveBeenCalledTimes(1);
    const copied = vi.mocked(writeText).mock.calls[0][0];
    expect(copied).toContain('"version": "dev"');
    expect(copied).toContain("request_failed");
    expect(copied).not.toMatch(/cookie|bearer|authorization/i);
  });

  it("shows an honest error when the clipboard is unavailable", async () => {
    Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true });
    vi.mocked(fetchDiagnostics).mockResolvedValue({
      version: "dev",
      os: "darwin arm64",
      mode: "desktop",
      providers: [],
      recentErrors: [],
    });
    render(<Preferences {...props()} />);
    fireEvent.click(screen.getByRole("button", { name: "Copy diagnostics" }));
    await waitFor(() => {
      expect(screen.getByText(/couldn't be copied/i)).toBeTruthy();
    });
  });

  it("reports an unsupported log-folder action honestly", async () => {
    vi.mocked(openLogFolder).mockRejectedValue(
      new APIError("NOT_SUPPORTED", "Opening the log folder isn't supported on this system.", 501),
    );
    render(<Preferences {...props()} />);
    fireEvent.click(screen.getByRole("button", { name: "Open log folder" }));
    await waitFor(() => {
      expect(screen.getByText(/isn't supported on this system/i)).toBeTruthy();
    });
    expect(screen.queryByText(/opened/i)).toBeNull();
  });

  it("surfaces a failed open attempt without pretending success", async () => {
    vi.mocked(openLogFolder).mockRejectedValue(
      new APIError("LOG_FOLDER_FAILED", "The log folder couldn't be opened.", 502),
    );
    render(<Preferences {...props()} />);
    fireEvent.click(screen.getByRole("button", { name: "Open log folder" }));
    await waitFor(() => {
      expect(screen.getByText(/couldn't be opened/i)).toBeTruthy();
    });
  });

  it("opens the log folder on click", async () => {
    vi.mocked(openLogFolder).mockResolvedValue(undefined);
    render(<Preferences {...props()} />);
    fireEvent.click(screen.getByRole("button", { name: "Open log folder" }));
    await waitFor(() => {
      expect(openLogFolder).toHaveBeenCalledTimes(1);
    });
    expect(screen.queryByText(/couldn't be opened/i)).toBeNull();
  });
});
