// @vitest-environment happy-dom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Preferences from "./Preferences";
import type { PreferencesPageProps } from "../types";
import type { Provider, Settings } from "../api";
import type * as UpdatesModule from "../updates";

vi.mock("../updates", async (importOriginal) => {
  const actual = await importOriginal<typeof UpdatesModule>();
  return {
    ...actual,
    updateChecksSupported: vi.fn(() => true),
    checkForDesktopUpdates: vi.fn(),
    installDesktopUpdate: vi.fn(),
    restartDesktopApp: vi.fn(),
  };
});

import {
  checkForDesktopUpdates,
  installDesktopUpdate,
  restartDesktopApp,
  updateChecksSupported,
} from "../updates";

afterEach(cleanup);

const settings: Settings = {
  subtitleLanguage: "en",
  quality: "auto",
  autoplay: false,
  preferredProvider: "",
  revision: 0,
};

const providers: Provider[] = [];

function props(overrides: Partial<PreferencesPageProps> = {}): PreferencesPageProps {
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
    ...overrides,
  };
}


beforeEach(() => {
  vi.mocked(updateChecksSupported).mockReturnValue(true);
});
describe("Preferences update checks", () => {
  it("offers no update UI outside the desktop app", () => {
    vi.mocked(updateChecksSupported).mockReturnValue(false);
    render(<Preferences {...props()} />);
    expect(
      screen.getByText("Update checks need the desktop app, and a build with updates configured."),
    ).toBeDefined();
    expect(screen.queryByRole("button", { name: "Check for updates" })).toBeNull();
  });

  it("reports up to date after a manual check", async () => {
    vi.mocked(checkForDesktopUpdates).mockResolvedValue({
      state: "up-to-date",
      current: "1.0.0",
    });
    render(<Preferences {...props()} />);
    fireEvent.click(screen.getByRole("button", { name: "Check for updates" }));
    await waitFor(() => {
      expect(screen.getByText("You're up to date.")).toBeDefined();
    });
  });

  it("checks, downloads with progress and asks for restart consent", async () => {
    vi.mocked(checkForDesktopUpdates).mockResolvedValue({
      state: "available",
      version: "1.1.0",
      notes: "Fixes",
      current: "1.0.0",
    });
    vi.mocked(installDesktopUpdate).mockImplementation(async (onProgress) => {
      onProgress({ downloaded: 50, total: 100 });
      onProgress({ downloaded: 100, total: 100 });
    });
    vi.mocked(restartDesktopApp).mockResolvedValue(undefined);
    render(<Preferences {...props()} />);
    fireEvent.click(screen.getByRole("button", { name: "Check for updates" }));
    await waitFor(() => {
      expect(screen.getByText("Version 1.1.0 is available.")).toBeDefined();
    });
    fireEvent.click(screen.getByRole("button", { name: "Download and install" }));
    await waitFor(() => {
      expect(screen.getByText("Version 1.1.0 is installed. Restart the app to finish the update."))
        .toBeDefined();
    });
    // Restart requires explicit consent; the button exists only after install.
    fireEvent.click(screen.getByRole("button", { name: "Restart now" }));
    await waitFor(() => {
      expect(restartDesktopApp).toHaveBeenCalledTimes(1);
    });
  });

  it("shows a retryable error state when the update backend is unreachable", async () => {
    vi.mocked(checkForDesktopUpdates).mockRejectedValueOnce(new Error("network down"));
    render(<Preferences {...props()} />);
    fireEvent.click(screen.getByRole("button", { name: "Check for updates" }));
    await waitFor(() => {
      expect(screen.getByText("The request couldn't be completed. Try again.")).toBeDefined();
    });
    // The check button remains enabled: the failure is retryable.
    expect(screen.getByRole("button", { name: "Check for updates" })).toHaveProperty("disabled", false);
  });

  it("keeps the app running when the download fails and offers the update again", async () => {
    vi.mocked(checkForDesktopUpdates).mockResolvedValue({
      state: "available",
      version: "1.1.0",
      current: "1.0.0",
    });
    vi.mocked(installDesktopUpdate).mockRejectedValueOnce(new Error("interrupted"));
    render(<Preferences {...props()} />);
    fireEvent.click(screen.getByRole("button", { name: "Check for updates" }));
    await waitFor(() => {
      expect(screen.getByText("Version 1.1.0 is available.")).toBeDefined();
    });
    fireEvent.click(screen.getByRole("button", { name: "Download and install" }));
    await waitFor(() => {
      expect(
        screen.getByText("The update couldn't be downloaded. It will be offered again on the next check."),
      ).toBeDefined();
    });
    // The install can be retried; no restart is offered.
    expect(screen.queryByRole("button", { name: "Restart now" })).toBeNull();
  });

  it("warns about stopping playback before a consented restart", async () => {
    vi.mocked(checkForDesktopUpdates).mockResolvedValue({
      state: "available",
      version: "1.1.0",
      current: "1.0.0",
    });
    vi.mocked(installDesktopUpdate).mockResolvedValue(undefined);
    render(<Preferences {...props({ playing: true })} />);
    fireEvent.click(screen.getByRole("button", { name: "Check for updates" }));
    await waitFor(() => {
      expect(screen.getByText("Version 1.1.0 is available.")).toBeDefined();
    });
    fireEvent.click(screen.getByRole("button", { name: "Download and install" }));
    await waitFor(() => {
      expect(
        screen.getByText("Something is playing right now. Restarting stops it; playback positions stay saved."),
      ).toBeDefined();
    });
  });
});
