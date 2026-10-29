import { useEffect, useState } from "react";

export type ThemeMode = "system" | "light" | "dark";
export type ResolvedTheme = "light" | "dark";

const STORAGE_KEY = "ohmyanime.theme";
const LIGHT_QUERY = "(prefers-color-scheme: light)";

export function systemTheme(): ResolvedTheme {
  if (typeof window === "undefined" || !window.matchMedia) return "dark";
  return window.matchMedia(LIGHT_QUERY).matches ? "light" : "dark";
}

export function resolveTheme(mode: ThemeMode): ResolvedTheme {
  return mode === "system" ? systemTheme() : mode;
}

export function readStoredTheme(): ThemeMode {
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY);
    if (stored === "light" || stored === "dark" || stored === "system") return stored;
  } catch {
    /* storage can be unavailable in private contexts */
  }
  return "dark";
}

export function applyTheme(mode: ThemeMode): ResolvedTheme {
  const resolved = resolveTheme(mode);
  if (typeof document === "undefined") return resolved;
  const root = document.documentElement;
  root.dataset.theme = resolved;
  root.dataset.themeMode = mode;
  root.style.colorScheme = resolved;
  const meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute("content", resolved === "light" ? "#f3f5fa" : "#090d16");
  return resolved;
}

export function storeTheme(mode: ThemeMode) {
  try {
    window.localStorage.setItem(STORAGE_KEY, mode);
  } catch {
    /* an explicit choice still applies for this session */
  }
}

/** Applies the current mode and keeps System in sync with live OS changes. */
export function useThemeController(): [ThemeMode, (mode: ThemeMode) => void] {
  const [mode, setMode] = useState<ThemeMode>(() => readStoredTheme());

  useEffect(() => {
    applyTheme(mode);
    const media = window.matchMedia(LIGHT_QUERY);
    const onChange = () => {
      if (readStoredTheme() === "system" || mode === "system") applyTheme("system");
    };
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, [mode]);

  function update(next: ThemeMode) {
    setMode(next);
    storeTheme(next);
    applyTheme(next);
  }

  return [mode, update];
}
