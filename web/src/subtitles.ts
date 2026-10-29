import { useEffect, useState } from "react";

/*
  Subtitle text is drawn by the engine's cue renderer, so its size is a CSS
  custom property on the player rather than React markup. The choice is a
  display preference like the theme: it belongs to this browser, not to the
  account, and it must survive a reload.
*/

const STORAGE_KEY = "ohmyanime.subtitleScale";

export const SUBTITLE_SCALE_MIN = 0.85;
export const SUBTITLE_SCALE_MAX = 2;
export const SUBTITLE_SCALE_DEFAULT = 1;

export function clampSubtitleScale(value: number): number {
  if (!Number.isFinite(value)) return SUBTITLE_SCALE_DEFAULT;
  return Math.min(SUBTITLE_SCALE_MAX, Math.max(SUBTITLE_SCALE_MIN, value));
}

export function readSubtitleScale(): number {
  try {
    const stored = Number(window.localStorage.getItem(STORAGE_KEY));
    if (Number.isFinite(stored) && stored > 0) return clampSubtitleScale(stored);
  } catch {
    /* storage can be unavailable in private contexts */
  }
  return SUBTITLE_SCALE_DEFAULT;
}

export function storeSubtitleScale(value: number) {
  try {
    window.localStorage.setItem(STORAGE_KEY, String(clampSubtitleScale(value)));
  } catch {
    /* the choice still applies for this session */
  }
}

export function useSubtitleScale(): [number, (value: number) => void] {
  const [scale, setScale] = useState<number>(() => readSubtitleScale());

  useEffect(() => {
    storeSubtitleScale(scale);
  }, [scale]);

  return [scale, (value: number) => setScale(clampSubtitleScale(value))];
}
