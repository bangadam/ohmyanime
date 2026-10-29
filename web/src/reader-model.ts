/*
  Pure presentation logic for the manga reader. Logical page indices are
  always 0-based and independent of mode/direction/spread; these helpers
  map between the logical index and whatever a presentation mode shows.
*/

export type ReaderMode = "strip" | "paged";
export type ReaderDirection = "ltr" | "rtl";
export type ReaderFit = "width" | "height";

/** A spread is one screen of the paged mode: one or two logical pages. */
export interface Spread {
  indices: number[];
}

/** Group logical pages into spreads. With twoPage off every page is alone;
    with it on pages pair up and a lone final page becomes its own spread. */
export function buildSpreads(pageCount: number, twoPage: boolean): Spread[] {
  const total = Math.max(0, Math.floor(pageCount));
  if (total === 0) return [];
  if (!twoPage) {
    return Array.from({ length: total }, (_, index) => ({ indices: [index] }));
  }
  const spreads: Spread[] = [];
  for (let index = 0; index < total; index += 2) {
    spreads.push({ indices: total - index === 1 ? [index] : [index, index + 1] });
  }
  return spreads;
}

export function clampPage(page: number, pageCount: number): number {
  if (pageCount <= 0) return 0;
  return Math.min(pageCount - 1, Math.max(0, Math.floor(page)));
}

/** Position of the spread containing the (clamped) logical page. */
export function spreadPosition(spreads: Spread[], page: number): number {
  if (spreads.length === 0) return 0;
  let position = spreads.length - 1;
  for (let index = 0; index < spreads.length; index += 1) {
    if (spreads[index].indices.includes(clampPage(page, Number.MAX_SAFE_INTEGER))) {
      position = index;
      break;
    }
  }
  return position;
}

export type PageTurn =
  | { kind: "step"; delta: -1 | 1 }
  | { kind: "home" }
  | { kind: "end" }
  | null;

/** Map a key to a page turn. Horizontal keys flip meaning under RTL so the
    physical "next" arrow always follows the reading direction. */
export function pageTurnFor(key: string, direction: ReaderDirection): PageTurn {
  const forward = direction === "rtl" ? -1 : 1;
  switch (key) {
    case "ArrowRight":
      return { kind: "step", delta: forward };
    case "ArrowLeft":
      return { kind: "step", delta: forward === 1 ? -1 : 1 };
    case " ":
    case "Spacebar":
    case "PageDown":
      return { kind: "step", delta: 1 };
    case "PageUp":
      return { kind: "step", delta: -1 };
    case "Home":
      return { kind: "home" };
    case "End":
      return { kind: "end" };
    default:
      return null;
  }
}

/** Step to the adjacent spread and land on its first logical page.
    Returns null when the step would leave the chapter. */
export function moveBySpread(spreads: Spread[], page: number, delta: -1 | 1): number | null {
  if (spreads.length === 0) return null;
  const position = spreadPosition(spreads, page);
  const next = position + delta;
  if (next < 0 || next >= spreads.length) return null;
  return spreads[next].indices[0];
}

/** True when key handling must not touch the event: focus sits in a text
    entry control that owns the keyboard. */
export function isTextEntryTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName.toLowerCase();
  return tag === "input" || tag === "textarea" || tag === "select" || target.isContentEditable;
}

/** Bounded ahead-of-cursor preload window. "Ahead" follows the reading
    direction; behind is the smaller look-back for fast flips. */
export function preloadWindow(
  page: number,
  pageCount: number,
  ahead: number,
  behind: number,
  direction: ReaderDirection,
): number[] {
  if (pageCount <= 0) return [];
  const current = clampPage(page, pageCount);
  const forward = direction === "rtl" ? -1 : 1;
  const low = Math.max(0, Math.min(current, current + forward * ahead, current - forward * behind));
  const high = Math.min(
    pageCount - 1,
    Math.max(current, current + forward * ahead, current - forward * behind),
  );
  const indices: number[] = [];
  for (let index = low; index <= high; index += 1) indices.push(index);
  return indices;
}

export const READER_PRELOAD_AHEAD = 3;
export const READER_PRELOAD_BEHIND = 1;

/* Chapter navigation: ordering and availability are derived purely from the
   backend chapter shape, never from lexical label ordering. */

export interface ChapterLike {
  id: string;
  label: string;
  ordinal: number;
  number?: string;
  pageCount: number;
}

/** A chapter is openable only when the backend reports pages for it; the
    provider drops zero-page chapters, so a stale cached row is not. */
export function isChapterAvailable(chapter: ChapterLike): boolean {
  return chapter.pageCount > 0;
}

function chapterNumber(value: string | undefined): number | null {
  if (!value) return null;
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : null;
}

/** Backend chapter ordering: numbered chapters first in numeric order (so
    chapter 10 always follows 9 regardless of label quirks), then specials
    by ordinal; ties keep the incoming order (stable insertion sort). */
export function compareChapters(a: ChapterLike, b: ChapterLike): number {
  const na = chapterNumber(a.number);
  const nb = chapterNumber(b.number);
  if (na !== null && nb !== null) {
    if (na !== nb) return na < nb ? -1 : 1;
    if (a.number === b.number) return 0;
    return (a.number ?? "") < (b.number ?? "") ? -1 : 1;
  }
  if (na !== null) return -1;
  if (nb !== null) return 1;
  if (a.ordinal !== b.ordinal) return a.ordinal < b.ordinal ? -1 : 1;
  return 0;
}

/** Deterministic chapter order mirroring the backend sort, without mutating
    the incoming array. */
export function orderChapters<T extends ChapterLike>(chapters: readonly T[]): T[] {
  const ordered = [...chapters];
  for (let i = 1; i < ordered.length; i++) {
    for (let j = i; j > 0 && compareChapters(ordered[j - 1], ordered[j]) > 0; j--) {
      const swap = ordered[j - 1];
      ordered[j - 1] = ordered[j];
      ordered[j] = swap;
    }
  }
  return ordered;
}

/** Nearest openable chapter in the given direction from index. Unavailable
    chapters are skipped because they cannot be opened. */
export function neighborChapter<T extends ChapterLike>(
  chapters: readonly T[],
  index: number,
  delta: -1 | 1,
): T | null {
  for (let i = index + delta; i >= 0 && i < chapters.length; i += delta) {
    if (isChapterAvailable(chapters[i])) return chapters[i];
  }
  return null;
}

/**
 * Completion policy (issue #9): a chapter is complete only when the reader
 * actually reached its last logical page. Merely scrolling past one page or
 * leaving mid-chapter never marks it complete — no explicit "mark read"
 * action exists, so reaching the final page is the sole trigger.
 */
export function isChapterComplete(page: number, pageCount: number): boolean {
  return pageCount > 0 && page >= pageCount - 1;
}
