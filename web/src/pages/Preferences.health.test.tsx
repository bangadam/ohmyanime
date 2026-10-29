// @vitest-environment happy-dom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import Preferences from "./Preferences";
import type { PreferencesPageProps } from "../types";
import type { Provider, Settings } from "../api";

afterEach(cleanup);
const settings: Settings = {
  subtitleLanguage: "en",
  quality: "auto",
  autoplay: false,
  preferredProvider: "",
  revision: 0,
};

function props(providers: Provider[], onRefreshProviderHealth = () => {}): PreferencesPageProps {
  return {
    settings,
    providers,
    healthBusy: false,
    healthError: "",
    onRefreshProviderHealth,
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

const providers: Provider[] = [
  {
    id: "hianime",
    enabled: true,
    modes: ["sub", "dub"],
    status: "unverified",
    capabilities: { kinds: ["anime"], audioModes: ["sub", "dub"], readerFormats: [] },
    health: { state: "reachable", checkedAt: "2026-10-29T10:00:00Z" },
  },
  {
    id: "anichan",
    enabled: true,
    modes: ["sub"],
    status: "unverified",
    capabilities: { kinds: ["anime"], audioModes: ["sub"], readerFormats: [] },
    health: { state: "unknown" },
  },
];

describe("Preferences provider health", () => {
  it("renders each provider's health state and last-check timestamp", () => {
    render(<Preferences {...props(providers)} />);
    expect(screen.getAllByText("Reachable").length).toBe(1);
    expect(screen.getAllByText("Not checked").length).toBe(1);
    expect(screen.getByText(/Checked /)).toBeTruthy();
  });

  it("keeps reachability copy distinct from playback guarantees", () => {
    render(<Preferences {...props(providers)} />);
    expect(screen.getByText(/doesn't guarantee every episode or chapter plays/i)).toBeTruthy();
  });

  it("triggers one manual refresh, never on render", () => {
    const onRefreshProviderHealth = vi.fn();
    render(<Preferences {...props(providers, onRefreshProviderHealth)} />);
    expect(onRefreshProviderHealth).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Check providers now" }));
    expect(onRefreshProviderHealth).toHaveBeenCalledTimes(1);
  });

  it("surfaces a failed refresh without probing again", () => {
    render(
      <Preferences
        {...props(providers, () => {})}
        healthError="The health check couldn't run."
      />,
    );
    expect(screen.getByText("The health check couldn't run.")).toBeTruthy();
  });
});
