// @vitest-environment happy-dom
import { describe, expect, it } from "vitest";
import {
  buildSpreads,
  clampPage,
  isChapterComplete,
  isTextEntryTarget,
  moveBySpread,
  pageTurnFor,
  preloadWindow,
  spreadPosition,
} from "./reader-model";

describe("buildSpreads", () => {
  it("keeps every page alone without two-page spread", () => {
    expect(buildSpreads(3, false)).toEqual([{ indices: [0] }, { indices: [1] }, { indices: [2] }]);
  });

  it("pairs pages and gives an odd final page its own spread", () => {
    expect(buildSpreads(5, true)).toEqual([
      { indices: [0, 1] },
      { indices: [2, 3] },
      { indices: [4] },
    ]);
    expect(buildSpreads(4, true)).toEqual([
      { indices: [0, 1] },
      { indices: [2, 3] },
    ]);
  });

  it("handles empty chapters", () => {
    expect(buildSpreads(0, true)).toEqual([]);
    expect(buildSpreads(0, false)).toEqual([]);
  });
});

describe("spreadPosition and moveBySpread", () => {
  const spreads = buildSpreads(5, true);

  it("finds the spread holding either page of a pair", () => {
    expect(spreadPosition(spreads, 0)).toBe(0);
    expect(spreadPosition(spreads, 1)).toBe(0);
    expect(spreadPosition(spreads, 4)).toBe(2);
  });

  it("clamps out-of-range pages into the chapter", () => {
    expect(clampPage(9, 5)).toBe(4);
    expect(clampPage(-3, 5)).toBe(0);
    expect(spreadPosition(spreads, 99)).toBe(2);
  });

  it("steps by spread and reports chapter boundaries", () => {
    expect(moveBySpread(spreads, 0, 1)).toBe(2);
    expect(moveBySpread(spreads, 3, 1)).toBe(4);
    expect(moveBySpread(spreads, 0, -1)).toBeNull();
    expect(moveBySpread(spreads, 4, 1)).toBeNull();
  });
});

describe("pageTurnFor", () => {
  it("advances with the right arrow in LTR", () => {
    expect(pageTurnFor("ArrowRight", "ltr")).toEqual({ kind: "step", delta: 1 });
    expect(pageTurnFor("ArrowLeft", "ltr")).toEqual({ kind: "step", delta: -1 });
  });

  it("flips horizontal arrows under RTL", () => {
    expect(pageTurnFor("ArrowRight", "rtl")).toEqual({ kind: "step", delta: -1 });
    expect(pageTurnFor("ArrowLeft", "rtl")).toEqual({ kind: "step", delta: 1 });
  });

  it("keeps space and page keys direction-independent", () => {
    expect(pageTurnFor(" ", "rtl")).toEqual({ kind: "step", delta: 1 });
    expect(pageTurnFor("PageDown", "rtl")).toEqual({ kind: "step", delta: 1 });
    expect(pageTurnFor("PageUp", "ltr")).toEqual({ kind: "step", delta: -1 });
    expect(pageTurnFor("Home", "ltr")).toEqual({ kind: "home" });
    expect(pageTurnFor("End", "ltr")).toEqual({ kind: "end" });
    expect(pageTurnFor("a", "ltr")).toBeNull();
  });
});

describe("isTextEntryTarget", () => {
  it("ignores keys aimed at text entry controls", () => {
    document.body.innerHTML = "<input /><textarea></textarea><select></select><div></div>";
    const [input, textarea, select, div] = Array.from(document.body.children);
    expect(isTextEntryTarget(input)).toBe(true);
    expect(isTextEntryTarget(textarea)).toBe(true);
    expect(isTextEntryTarget(select)).toBe(true);
    expect(isTextEntryTarget(div)).toBe(false);
    expect(isTextEntryTarget(null)).toBe(false);
    expect(isTextEntryTarget(window)).toBe(false);
  });
});

describe("preloadWindow", () => {
  it("looks ahead in reading direction with a bounded behind window", () => {
    expect(preloadWindow(2, 10, 3, 1, "ltr")).toEqual([1, 2, 3, 4, 5]);
    expect(preloadWindow(5, 10, 3, 1, "rtl")).toEqual([2, 3, 4, 5, 6]);
  });

  it("clamps to the chapter bounds", () => {
    expect(preloadWindow(0, 4, 3, 1, "ltr")).toEqual([0, 1, 2, 3]);
    expect(preloadWindow(3, 4, 3, 1, "ltr")).toEqual([2, 3]);
    expect(preloadWindow(2, 0, 3, 1, "ltr")).toEqual([]);
  });
});

describe("isChapterComplete", () => {
  it("completes only when the last logical page is reached", () => {
    expect(isChapterComplete(0, 5)).toBe(false);
    expect(isChapterComplete(1, 5)).toBe(false);
    expect(isChapterComplete(4, 5)).toBe(true);
  });

  it("never completes an empty chapter", () => {
    expect(isChapterComplete(0, 0)).toBe(false);
  });
});
