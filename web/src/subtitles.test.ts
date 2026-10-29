// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  SUBTITLE_BACKGROUND_OPACITY_DEFAULT,
  SUBTITLE_EDGE_STYLES,
  SUBTITLE_EDGE_STYLE_DEFAULT,
  SUBTITLE_SCALE_DEFAULT,
  SUBTITLE_SCALE_MAX,
  SUBTITLE_SCALE_MIN,
  clampSubtitleScale,
  normalizeSubtitleEdgeStyle,
  readSubtitleBackgroundOpacity,
  readSubtitleEdgeStyle,
  readSubtitleScale,
  storeSubtitleBackgroundOpacity,
  storeSubtitleEdgeStyle,
  storeSubtitleScale,
  subtitleCueStyles,
  type SubtitleEdgeStyle,
} from "./subtitles";

const KEY = "ohmyanime.subtitleScale";

function setStored(value: string | null) {
  if (value === null) window.localStorage.removeItem(KEY);
  else window.localStorage.setItem(KEY, value);
}

afterEach(() => {
  window.localStorage.clear();
});

describe("clampSubtitleScale", () => {
  it("clamps to the supported range", () => {
    expect(clampSubtitleScale(0.5)).toBe(SUBTITLE_SCALE_MIN);
    expect(clampSubtitleScale(5)).toBe(SUBTITLE_SCALE_MAX);
    expect(clampSubtitleScale(1.25)).toBe(1.25);
  });

  it("restores the default for non-finite values", () => {
    expect(clampSubtitleScale(Number.NaN)).toBe(SUBTITLE_SCALE_DEFAULT);
    expect(clampSubtitleScale(Number.POSITIVE_INFINITY)).toBe(SUBTITLE_SCALE_DEFAULT);
  });
});

describe("subtitle scale persistence", () => {
  it("round-trips a clamped value through storage", () => {
    storeSubtitleScale(3);
    setStored("1.4");
    expect(readSubtitleScale()).toBe(1.4);
    setStored("9");
    expect(readSubtitleScale()).toBe(SUBTITLE_SCALE_MAX);
    setStored("garbage");
    expect(readSubtitleScale()).toBe(SUBTITLE_SCALE_DEFAULT);
    setStored(null);
    expect(readSubtitleScale()).toBe(SUBTITLE_SCALE_DEFAULT);
  });

  it("ignores stored values that are not positive", () => {
    setStored("0");
    expect(readSubtitleScale()).toBe(SUBTITLE_SCALE_DEFAULT);
  });

  it("tolerates unavailable storage", () => {
    const failing = {
      getItem: () => {
        throw new Error("denied");
      },
      setItem: () => {
        throw new Error("denied");
      },
      removeItem: () => {},
    };
    vi.stubGlobal("localStorage", failing);
    expect(readSubtitleScale()).toBe(SUBTITLE_SCALE_DEFAULT);
    expect(() => storeSubtitleScale(1.2)).not.toThrow();
    vi.unstubAllGlobals();
  });
});

describe("subtitleCueStyles", () => {
  it("maps background opacity to the same rgba the player paints", () => {
    expect(subtitleCueStyles(0, "none").backgroundColor).toBe("rgba(2, 5, 11, 0)");
    expect(subtitleCueStyles(0.72, "shadow").backgroundColor).toBe(
      `rgba(2, 5, 11, ${SUBTITLE_BACKGROUND_OPACITY_DEFAULT})`,
    );
    expect(subtitleCueStyles(9, "none").backgroundColor).toBe("rgba(2, 5, 11, 1)");
  });

  it("maps every edge style to a distinct text shadow", () => {
    const shadows = new Map(
      SUBTITLE_EDGE_STYLES.map((style) => [
        style,
        subtitleCueStyles(SUBTITLE_BACKGROUND_OPACITY_DEFAULT, style).textShadow,
      ]),
    );
    expect([...shadows.values()].length).toBe(new Set(shadows.values()).size);
    expect(shadows.get("none")).toBe("none");
    expect(shadows.get("outline")).toContain("rgba(2, 5, 11, 0.9)");
  });

  it("falls back to the default edge style for unknown values", () => {
    expect(subtitleCueStyles(0.5, "glitch" as SubtitleEdgeStyle).textShadow).toBe(
      subtitleCueStyles(0.5, SUBTITLE_EDGE_STYLE_DEFAULT).textShadow,
    );
    expect(normalizeSubtitleEdgeStyle("outline")).toBe("outline");
    expect(normalizeSubtitleEdgeStyle(undefined)).toBe(SUBTITLE_EDGE_STYLE_DEFAULT);
  });
});

describe("subtitle appearance persistence", () => {
  it("round-trips background opacity and edge style through storage", () => {
    storeSubtitleBackgroundOpacity(0.35);
    expect(readSubtitleBackgroundOpacity()).toBe(0.35);
    storeSubtitleBackgroundOpacity(5);
    expect(readSubtitleBackgroundOpacity()).toBe(1);

    storeSubtitleEdgeStyle("outline");
    expect(readSubtitleEdgeStyle()).toBe("outline");
    storeSubtitleEdgeStyle("shadow");
    expect(readSubtitleEdgeStyle()).toBe("shadow");
  });
});
