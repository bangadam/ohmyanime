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

/*
  Cue background and edge style are painted by the same engine cue renderer,
  so they follow the same browser-local persistence as the scale. The mapping
  to CSS is a pure function so preferences, live preview and the player render
  the exact same values. These only affect soft (switchable) subtitle tracks.
*/

const BACKGROUND_OPACITY_KEY = "ohmyanime.subtitleBackgroundOpacity";
const EDGE_STYLE_KEY = "ohmyanime.subtitleEdgeStyle";

export const SUBTITLE_BACKGROUND_OPACITY_MIN = 0;
export const SUBTITLE_BACKGROUND_OPACITY_MAX = 1;
export const SUBTITLE_BACKGROUND_OPACITY_DEFAULT = 0.72;

export type SubtitleEdgeStyle = "none" | "outline" | "shadow";
export const SUBTITLE_EDGE_STYLES: SubtitleEdgeStyle[] = ["none", "outline", "shadow"];
export const SUBTITLE_EDGE_STYLE_DEFAULT: SubtitleEdgeStyle = "shadow";

export interface SubtitleCueStyles {
  backgroundColor: string;
  textShadow: string;
}

export function clampSubtitleBackgroundOpacity(value: number): number {
  if (!Number.isFinite(value)) return SUBTITLE_BACKGROUND_OPACITY_DEFAULT;
  return Math.min(SUBTITLE_BACKGROUND_OPACITY_MAX, Math.max(SUBTITLE_BACKGROUND_OPACITY_MIN, value));
}

export function normalizeSubtitleEdgeStyle(value: unknown): SubtitleEdgeStyle {
  return SUBTITLE_EDGE_STYLES.includes(value as SubtitleEdgeStyle)
    ? (value as SubtitleEdgeStyle)
    : SUBTITLE_EDGE_STYLE_DEFAULT;
}

/*
  Pure preference -> CSS mapping shared by the player's ::cue properties and
  the Preferences live preview. An outline is drawn with multi-directional
  text shadows because ::cue does not accept text-stroke.
*/
export function subtitleCueStyles(
  opacity: number,
  edgeStyle: SubtitleEdgeStyle,
): SubtitleCueStyles {
  const background = clampSubtitleBackgroundOpacity(opacity);
  const backgroundColor = `rgba(2, 5, 11, ${background})`;
  let textShadow: string;
  switch (normalizeSubtitleEdgeStyle(edgeStyle)) {
    case "none":
      textShadow = "none";
      break;
    case "outline":
      textShadow = [
        "0 -1px 0 rgba(2, 5, 11, 0.9)",
        "0 1px 0 rgba(2, 5, 11, 0.9)",
        "-1px 0 0 rgba(2, 5, 11, 0.9)",
        "1px 0 0 rgba(2, 5, 11, 0.9)",
      ].join(", ");
      break;
    default:
      textShadow = "0 1px 2px rgba(2, 5, 11, 0.9)";
  }
  return { backgroundColor, textShadow };
}

export function readSubtitleBackgroundOpacity(): number {
  try {
    const stored = Number(window.localStorage.getItem(BACKGROUND_OPACITY_KEY));
    if (Number.isFinite(stored) && stored >= 0) return clampSubtitleBackgroundOpacity(stored);
  } catch {
    /* storage can be unavailable in private contexts */
  }
  return SUBTITLE_BACKGROUND_OPACITY_DEFAULT;
}

export function storeSubtitleBackgroundOpacity(value: number) {
  try {
    window.localStorage.setItem(BACKGROUND_OPACITY_KEY, String(clampSubtitleBackgroundOpacity(value)));
  } catch {
    /* the choice still applies for this session */
  }
}

export function readSubtitleEdgeStyle(): SubtitleEdgeStyle {
  try {
    return normalizeSubtitleEdgeStyle(window.localStorage.getItem(EDGE_STYLE_KEY));
  } catch {
    /* storage can be unavailable in private contexts */
  }
  return SUBTITLE_EDGE_STYLE_DEFAULT;
}

export function storeSubtitleEdgeStyle(value: SubtitleEdgeStyle) {
  try {
    window.localStorage.setItem(EDGE_STYLE_KEY, normalizeSubtitleEdgeStyle(value));
  } catch {
    /* the choice still applies for this session */
  }
}

export function useSubtitleCuePreferences(): {
  backgroundOpacity: number;
  edgeStyle: SubtitleEdgeStyle;
  setBackgroundOpacity: (value: number) => void;
  setEdgeStyle: (value: SubtitleEdgeStyle) => void;
} {
  const [backgroundOpacity, setOpacity] = useState<number>(() => readSubtitleBackgroundOpacity());
  const [edgeStyle, setEdge] = useState<SubtitleEdgeStyle>(() => readSubtitleEdgeStyle());

  useEffect(() => {
    storeSubtitleBackgroundOpacity(backgroundOpacity);
  }, [backgroundOpacity]);

  useEffect(() => {
    storeSubtitleEdgeStyle(edgeStyle);
  }, [edgeStyle]);

  return {
    backgroundOpacity,
    edgeStyle,
    setBackgroundOpacity: (value: number) => setOpacity(clampSubtitleBackgroundOpacity(value)),
    setEdgeStyle: (value: SubtitleEdgeStyle) => setEdge(normalizeSubtitleEdgeStyle(value)),
  };
}
