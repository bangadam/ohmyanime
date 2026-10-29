import { useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import {
  ArrowLeft,
  ArrowLeftRight,
  BookOpen,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Columns2,
  Maximize2,
  MoveVertical,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import {
  APIError,
  api,
  errorMessage,
  id,
  json,
  type MangaChapter,
  type MangaPage,
  type ReadingProgress,
} from "./api";
import { copy, format } from "./copy";
import { isAbortError } from "./utils";
import { Button, Spinner } from "./components";
import type { ReaderProps } from "./types";
import {
  buildSpreads,
  clampPage,
  isChapterAvailable,
  isChapterComplete,
  isTextEntryTarget,
  moveBySpread,
  neighborChapter,
  orderChapters,
  pageTurnFor,
  preloadWindow,
  READER_PRELOAD_AHEAD,
  READER_PRELOAD_BEHIND,
  spreadPosition,
  type ReaderDirection,
  type ReaderFit,
  type ReaderMode,
} from "./reader-model";
import "./styles/reader.css";

type ReaderStatus = "loading" | "ready" | "error";
type PageStatus = "waiting" | "loading" | "loaded" | "failed";

const ZOOM_MIN = 0.5;
const ZOOM_MAX = 2;
const ZOOM_STEP = 0.25;

// Debounce window for reading-progress writes: one PUT per quiet second.
const READER_SAVE_DEBOUNCE_MS = 1_000;

interface MangaPagesResponse {
  items: MangaPage[];
  chapter: MangaChapter;
  mangaId: string;
}

/* Keeps background tab order intact across reader remounts: focus only
   returns to the opener once the final reader instance unmounts. */
let activeReaderCount = 0;
let returnFocusTarget: HTMLElement | null = null;

/* One logical page image with its own bounded retry. The img remounts on
   retry so the browser re-requests without touching the signed URL. */
function ReaderPageImage({
  url,
  alt,
  failedMessage,
}: {
  url: string | null;
  alt: string;
  failedMessage: string;
}) {
  const [attempt, setAttempt] = useState(0);
  const [status, setStatus] = useState<PageStatus>(url ? "loading" : "waiting");

  useEffect(() => {
    setStatus(url ? "loading" : "waiting");
  }, [url]);

  if (!url) {
    return <div className="reader-page reader-page--placeholder" aria-hidden="true" />;
  }
  if (status === "failed") {
    return (
      <div className="reader-page-failed">
        <p role="alert">{failedMessage}</p>
        <Button
          variant="primary"
          onClick={() => {
            setAttempt((value) => value + 1);
            setStatus("loading");
          }}
        >
          {copy.manga.reader.retryPage}
        </Button>
      </div>
    );
  }
  return (
    <img
      key={attempt}
      className="reader-page"
      src={url}
      alt={alt}
      onLoad={() => setStatus("loaded")}
      onError={() => setStatus("failed")}
      decoding="async"
    />
  );
}

export function Reader({ manga, chapter, chapters, onChapter, onClose }: ReaderProps) {
  const [attempt, setAttempt] = useState(0);
  const [pages, setPages] = useState<MangaPage[]>([]);
  const [status, setStatus] = useState<ReaderStatus>("loading");
  const [error, setError] = useState("");
  // Logical page index (0-based); independent of mode/direction/spread.
  const [page, setPage] = useState(0);
  const [mode, setMode] = useState<ReaderMode>("strip");
  const [direction, setDirection] = useState<ReaderDirection>("ltr");
  const [fit, setFit] = useState<ReaderFit>("width");
  const [twoPage, setTwoPage] = useState(false);
  const [zoom, setZoom] = useState(1);
  const [preloaded, setPreloaded] = useState<ReadonlySet<number>>(new Set());
  const [pickerOpen, setPickerOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const pagesRef = useRef<Map<number, HTMLElement>>(new Map());
  const pageRef = useRef(page);

  useEffect(() => {
    pageRef.current = page;
  }, [page]);

  // Persisted reading position (issue #9). progressRef mirrors the last
  // known server state: its revision guards writes so a stale window never
  // overwrites newer progress, and its page resumes the exact logical page.
  const [progressReady, setProgressReady] = useState(false);
  const progressRef = useRef<ReadingProgress | null>(null);
  const appliedChapterRef = useRef<string | null>(null);
  const saveTimerRef = useRef<number | null>(null);
  const saveStateRef = useRef<{ chapterId: string; page: number; pageCount: number } | null>(null);
  const pendingRef = useRef<{ chapterId: string; page: number; completed: boolean } | null>(null);
  const savingRef = useRef(false);
  const conflictRef = useRef(false);
  const resumePageRef = useRef<number | null>(null);
  const lastSavedRef = useRef<{ chapterId: string; page: number; completed: boolean } | null>(null);

  // Chapter order follows the backend ordering, not lexical labels:
  // numbered chapters in numeric order, then specials. Unavailable
  // (zero-page) chapters remain visible in the picker but cannot be opened.
  const ordered = useMemo(() => orderChapters(chapters), [chapters]);
  const index = ordered.findIndex((item) => item.id === chapter.id);
  const previous = neighborChapter(ordered, index, -1);
  const next = neighborChapter(ordered, index, 1);
  const atFirst = previous === null;
  const atLast = next === null;

  // Focus follows the player convention: remember the opener, focus the
  // reader, restore focus when the last reader instance closes.
  useEffect(() => {
    activeReaderCount += 1;
    const active = document.activeElement;
    if (active instanceof HTMLElement && active !== document.body && !active.closest(".reader")) {
      returnFocusTarget = active;
    }
    rootRef.current?.focus({ preventScroll: true });
    return () => {
      activeReaderCount = Math.max(0, activeReaderCount - 1);
      if (activeReaderCount !== 0) return;
      const target = returnFocusTarget;
      window.requestAnimationFrame(() => {
        if (activeReaderCount === 0 && target?.isConnected) {
          target.focus({ preventScroll: true });
          returnFocusTarget = null;
        }
      });
    };
  }, []);

  // Load the chapter pages whenever the chapter changes.
  useEffect(() => {
    const controller = new AbortController();
    const signal = controller.signal;
    setStatus("loading");
    setError("");
    setPages([]);
    setPage(0);
    setPreloaded(new Set());
    scrollRef.current?.scrollTo({ top: 0 });
    api<MangaPagesResponse>(`/manga/chapters/${id(chapter.id)}/pages`, { signal })
      .then((result) => {
        if (signal.aborted) return;
        setPages(result.items ?? []);
        setStatus("ready");
      })
      .catch((reason: unknown) => {
        if (signal.aborted || isAbortError(reason)) return;
        setError(errorMessage(reason, copy.manga.reader.error));
        setStatus("error");
      });
    return () => controller.abort();
  }, [chapter.id, attempt]);

  // Fetch the stored reading position once per manga so the reader can
  // resume the exact logical page. A failed lookup degrades to a fresh
  // start (revision 0 inserts on the first save).
  useEffect(() => {
    const controller = new AbortController();
    api<ReadingProgress | null>(`/manga/${id(manga.id)}/reading-progress`, {
      signal: controller.signal,
    })
      .then((stored) => {
        if (controller.signal.aborted || !stored || !Number.isFinite(stored.page)) return;
        if (Number.isFinite(stored.revision)) progressRef.current = stored;
      })
      .catch(() => {})
      .finally(() => {
        if (!controller.signal.aborted) setProgressReady(true);
      });
    return () => controller.abort();
  }, [manga.id]);

  // Apply the stored resume position once per chapter load: once both the
  // pages and the stored progress are available, the exact logical page is
  // restored, clamped to the current inventory (the provider may have
  // removed pages since the position was saved).
  useEffect(() => {
    if (status !== "ready" || !progressReady) return;
    if (appliedChapterRef.current === chapter.id) return;
    appliedChapterRef.current = chapter.id;
    const stored = progressRef.current;
    if (stored && stored.chapterId === chapter.id && pages.length > 0) {
      const target = clampPage(stored.page, pages.length);
      resumePageRef.current = target;
      setPage(target);
    } else {
      setPage(0);
    }
  }, [status, progressReady, chapter.id, pages.length]);

  // Persist the reading position (issue #9): page turns debounce into one
  // write per quiet second; closing, switching chapters or unmounting
  // flushes the final position immediately. Completion follows the policy
  // in isChapterComplete: only reaching the last page completes a chapter,
  // scrolling past one page never does.
  const save = async (): Promise<void> => {
    if (conflictRef.current) return;
    const state = saveStateRef.current;
    if (!state || state.pageCount === 0) return;
    const candidate = {
      chapterId: state.chapterId,
      page: clampPage(state.page, state.pageCount),
      completed: isChapterComplete(state.page, state.pageCount),
    };
    // A no-op flush (nothing changed since the last write) skips the PUT so
    // closing the reader does not churn the revision.
    const last = lastSavedRef.current;
    if (
      last &&
      last.chapterId === candidate.chapterId &&
      last.page === candidate.page &&
      last.completed === candidate.completed
    ) {
      return;
    }
    pendingRef.current = candidate;
    if (savingRef.current) return;
    savingRef.current = true;
    let snapshot: { chapterId: string; page: number; completed: boolean } | null = pendingRef.current;
    try {
      while (snapshot !== null) {
        pendingRef.current = null;
        progressRef.current = await api<ReadingProgress>(
          `/manga/${id(manga.id)}/reading-progress`,
          {
            method: "PUT",
            keepalive: true,
            body: json({
              chapterId: snapshot.chapterId,
              page: snapshot.page,
              completed: snapshot.completed,
              expectedRevision: progressRef.current?.revision ?? 0,
            }),
          },
        );
        lastSavedRef.current = snapshot;
        snapshot = pendingRef.current;
      }
    } catch (reason: unknown) {
      if (reason instanceof APIError && reason.status === 409) {
        // Another window wrote newer progress; this window stops writing so
        // a stale position can never overwrite it.
        conflictRef.current = true;
        pendingRef.current = null;
      } else if (snapshot) {
        // A transient failure keeps the position pending for the next
        // change or the close-time flush to retry.
        pendingRef.current = snapshot;
      }
    } finally {
      savingRef.current = false;
    }
  };
  const saveRef = useRef(save);
  saveRef.current = save;

  useEffect(() => {
    if (status !== "ready" || pages.length === 0) return;
    saveStateRef.current = { chapterId: chapter.id, page, pageCount: pages.length };
    if (saveTimerRef.current !== null) window.clearTimeout(saveTimerRef.current);
    saveTimerRef.current = window.setTimeout(() => {
      saveTimerRef.current = null;
      void saveRef.current();
    }, READER_SAVE_DEBOUNCE_MS);
  }, [status, page, chapter.id, pages.length]);

  // Final save when the reader closes, unmounts, or the chapter switches:
  // the cleanup runs while the refs still hold the outgoing position.
  useEffect(() => {
    return () => {
      if (saveTimerRef.current !== null) {
        window.clearTimeout(saveTimerRef.current);
        saveTimerRef.current = null;
      }
      void saveRef.current();
    };
  }, [chapter.id]);

  // A chapter change resets the page, so focus must land predictably on the
  // new chapter's first page: mark the intent here, then the ready effect
  // below moves focus to the first page container once pages exist.
  const firstChapterRef = useRef(true);
  const focusFirstPageRef = useRef(false);
  useEffect(() => {
    if (firstChapterRef.current) {
      firstChapterRef.current = false;
      return;
    }
    focusFirstPageRef.current = true;
    rootRef.current?.focus({ preventScroll: true });
    setPickerOpen(false);
  }, [chapter.id]);

  // Focus lands on the first page of a newly opened chapter once its pages
  // arrive; strip mode focuses the page slot, paged mode the spread.
  const spreadRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (status !== "ready" || pages.length === 0 || !focusFirstPageRef.current) return;
    focusFirstPageRef.current = false;
    const target = mode === "strip" ? pagesRef.current.get(0) : spreadRef.current;
    target?.focus({ preventScroll: true });
  }, [status, pages.length, mode]);

  const spreads = useMemo(
    () => buildSpreads(pages.length, twoPage && mode === "paged"),
    [pages.length, twoPage, mode],
  );

  // Escape closes the reader; in paged mode the reader owns arrow/space
  // paging and never intercepts keys aimed at a text entry control.
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.preventDefault();
        // With the chapter picker open, Escape dismisses the picker; only a
        // second Escape leaves the reader.
        setPickerOpen((open) => {
          if (open) return false;
          onClose();
          return false;
        });
        return;
      }
      if (mode !== "paged" || isTextEntryTarget(event.target)) return;
      const turn = pageTurnFor(event.key, direction);
      if (!turn) return;
      event.preventDefault();
      setPage((current) => {
        if (spreads.length === 0) return current;
        if (turn.kind === "home") return spreads[0].indices[0];
        if (turn.kind === "end") return spreads[spreads.length - 1].indices[0];
        const moved = moveBySpread(spreads, current, turn.delta);
        return moved === null ? current : moved;
      });
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [mode, direction, spreads, onClose]);

  // Bounded ahead-of-cursor preloading; once preloaded a page stays cached.
  useEffect(() => {
    if (status !== "ready" || pages.length === 0) return;
    setPreloaded((previous) => {
      const next = new Set(previous);
      let changed = false;
      for (const candidate of preloadWindow(
        page,
        pages.length,
        READER_PRELOAD_AHEAD,
        READER_PRELOAD_BEHIND,
        direction,
      )) {
        if (!next.has(candidate)) {
          next.add(candidate);
          changed = true;
        }
      }
      return changed ? next : previous;
    });
  }, [status, page, pages.length, direction]);

  // In strip mode the counter follows the topmost page crossing the viewport.
  useEffect(() => {
    if (mode !== "strip" || status !== "ready" || pages.length === 0) return;
    if (typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(
      (entries) => {
        const visible = entries
          .filter((entry) => entry.isIntersecting)
          .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)[0];
        const element = visible?.target as HTMLElement | undefined;
        const observed = element ? Number(element.dataset.pageIndex) : NaN;
        if (!Number.isNaN(observed)) setPage(observed);
      },
      { root: scrollRef.current, threshold: 0 },
    );
    for (const element of pagesRef.current.values()) observer.observe(element);
    return () => observer.disconnect();
  }, [mode, status, pages.length]);

  // Entering strip mode (or finishing a load in it) keeps the visible
  // logical page: scroll it into view instead of snapping to the top.
  useEffect(() => {
    if (mode !== "strip" || status !== "ready" || pages.length === 0) return;
    // A freshly resumed position wins: the stored page scrolls into view
    // (clamped to the current inventory) instead of snapping to the top.
    const resume = resumePageRef.current;
    resumePageRef.current = null;
    pagesRef.current.get(resume ?? pageRef.current)?.scrollIntoView({ block: "start" });
  }, [mode, status, pages.length, progressReady]);

  // Resize must not lose the visible logical page in strip mode either.
  useEffect(() => {
    if (mode !== "strip") return;
    function onResize() {
      pagesRef.current.get(pageRef.current)?.scrollIntoView({ block: "start" });
    }
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, [mode]);

  const visiblePages =
    status === "ready" && spreads.length > 0
      ? spreads[spreadPosition(spreads, page)].indices
      : [];

  const heading = chapter.title ? `${chapter.label} · ${chapter.title}` : chapter.label;
  const pageAlt = (number: number) => format(copy.manga.reader.pageAlt, { number: number + 1 });

  const modeLabel = mode === "strip" ? copy.manga.reader.modePaged : copy.manga.reader.modeStrip;
  const modeIcon = mode === "strip" ? <Columns2 aria-hidden="true" /> : <MoveVertical aria-hidden="true" />;
  const directionLabel =
    direction === "ltr" ? copy.manga.reader.directionRtl : copy.manga.reader.directionLtr;
  const fitLabel = fit === "width" ? copy.manga.reader.fitHeight : copy.manga.reader.fitWidth;

  // Explicit end-of-chapter action: shown once the last page is reached, in
  // either mode. It moves to the next chapter when one is openable and
  // states plainly that this is the latest chapter otherwise.
  const endReached =
    status === "ready" &&
    pages.length > 0 &&
    (mode === "paged"
      ? spreads.length > 0 && page === spreads[spreads.length - 1].indices[0]
      : page >= pages.length - 1);
  const endOfChapter =
    endReached && next ? (
      <div className="reader-end">
        <p>{copy.manga.reader.endOfChapter}</p>
        <Button variant="primary" onClick={() => onChapter(next)}>
          {format(copy.manga.reader.endNext, { label: next.label })}
        </Button>
      </div>
    ) : endReached ? (
      <div className="reader-end">
        <p>{copy.manga.reader.endLatest}</p>
      </div>
    ) : null;

  return (
    <div className="reader" ref={rootRef} tabIndex={-1} role="region" aria-label={copy.manga.reader.region}>
      <header className="reader-bar">
        <button
          type="button"
          className="reader-bar__back"
          onClick={onClose}
          aria-label={copy.manga.reader.back}
          title={copy.manga.reader.back}
        >
          <ArrowLeft aria-hidden="true" />
        </button>
        <div className="reader-bar__heading">
          <p className="reader-bar__manga">{manga.title}</p>
          <p className="reader-bar__label">{heading}</p>
        </div>
        <p className="reader-bar__count tabular">
          {status === "ready"
            ? format(copy.manga.reader.page, { current: page + 1, total: pages.length })
            : ""}
        </p>
        <div className="reader-bar__modes" role="group" aria-label="Reading mode">
          <button
            type="button"
            className="reader-bar__toggle"
            onClick={() => setMode((value) => (value === "strip" ? "paged" : "strip"))}
            aria-label={modeLabel}
            title={modeLabel}
          >
            {modeIcon}
          </button>
          <button
            type="button"
            className="reader-bar__toggle"
            aria-pressed={direction === "rtl"}
            aria-label={directionLabel}
            title={directionLabel}
            onClick={() => setDirection((value) => (value === "ltr" ? "rtl" : "ltr"))}
          >
            <ArrowLeftRight aria-hidden="true" />
          </button>
          <button
            type="button"
            className="reader-bar__toggle"
            aria-pressed={fit === "height"}
            aria-label={fitLabel}
            title={fitLabel}
            onClick={() => setFit((value) => (value === "width" ? "height" : "width"))}
          >
            <Maximize2 aria-hidden="true" />
          </button>
          <button
            type="button"
            className="reader-bar__toggle"
            aria-pressed={twoPage}
            disabled={mode !== "paged"}
            aria-label={twoPage ? copy.manga.reader.spreadOn : copy.manga.reader.spreadOff}
            title={twoPage ? copy.manga.reader.spreadOn : copy.manga.reader.spreadOff}
            onClick={() => setTwoPage((value) => !value)}
          >
            <BookOpen aria-hidden="true" />
          </button>
          <button
            type="button"
            className="reader-bar__toggle"
            aria-label={copy.manga.reader.zoomOut}
            title={copy.manga.reader.zoomOut}
            disabled={zoom <= ZOOM_MIN}
            onClick={() => setZoom((value) => Math.max(ZOOM_MIN, value - ZOOM_STEP))}
          >
            <ZoomOut aria-hidden="true" />
          </button>
          <button
            type="button"
            className="reader-bar__toggle tabular"
            aria-label={copy.manga.reader.zoomReset}
            title={copy.manga.reader.zoomReset}
            onClick={() => setZoom(1)}
          >
            {Math.round(zoom * 100)}%
          </button>
          <button
            type="button"
            className="reader-bar__toggle"
            aria-label={copy.manga.reader.zoomIn}
            title={copy.manga.reader.zoomIn}
            disabled={zoom >= ZOOM_MAX}
            onClick={() => setZoom((value) => Math.min(ZOOM_MAX, value + ZOOM_STEP))}
          >
            <ZoomIn aria-hidden="true" />
          </button>
        </div>
        <div className="reader-bar__nav">
          {ordered.length > 1 ? (
            <div className="reader-bar__pick">
              <button
                type="button"
                className="reader-bar__toggle"
                aria-haspopup="true"
                aria-expanded={pickerOpen}
                aria-label={copy.manga.reader.chapterList}
                title={copy.manga.reader.chapterList}
                onClick={() => setPickerOpen((value) => !value)}
              >
                <ChevronDown aria-hidden="true" />
              </button>
              {pickerOpen ? (
                <div className="reader-bar__chapters">
                  {ordered.map((item) => {
                    const available = isChapterAvailable(item);
                    return (
                      <button
                        key={item.id}
                        type="button"
                        className="reader-bar__chapter-option"
                        disabled={!available}
                        aria-current={item.id === chapter.id ? "true" : undefined}
                        onClick={() => {
                          setPickerOpen(false);
                          if (item.id !== chapter.id) onChapter(item);
                        }}
                      >
                        <span className="reader-bar__chapter-label tabular">{item.label}</span>
                        {item.title ? (
                          <span className="reader-bar__chapter-title">{item.title}</span>
                        ) : null}
                        {!available ? (
                          <span className="reader-bar__chapter-flag">
                            {copy.manga.reader.unavailable}
                          </span>
                        ) : null}
                      </button>
                    );
                  })}
                </div>
              ) : null}
            </div>
          ) : null}
          <button
            type="button"
            className="reader-bar__chapter"
            onClick={() => previous && onChapter(previous)}
            disabled={atFirst}
            aria-label={copy.manga.reader.previous}
            title={copy.manga.reader.previous}
          >
            <ChevronLeft aria-hidden="true" />
          </button>
          <button
            type="button"
            className="reader-bar__chapter"
            onClick={() => next && onChapter(next)}
            disabled={atLast}
            aria-label={copy.manga.reader.next}
            title={copy.manga.reader.next}
          >
            <ChevronRight aria-hidden="true" />
          </button>
        </div>
      </header>

      {mode === "strip" ? (
        <div className="reader-scroll" ref={scrollRef}>
          {status === "loading" ? (
            <div className="reader-status">
              <Spinner label={copy.manga.reader.loading} />
            </div>
          ) : status === "error" ? (
            <div className="reader-status">
              <p className="reader-error" role="alert">
                {error}
              </p>
              <div className="reader-status__actions">
                <Button variant="primary" onClick={() => setAttempt((value) => value + 1)}>
                  {copy.manga.reader.retry}
                </Button>
                <Button variant="quiet" onClick={onClose}>
                  {copy.manga.reader.back}
                </Button>
              </div>
            </div>
          ) : (
            <div className={`reader-pages reader-pages--fit-${fit}`}>
              {pages.map((item) => (
                <div
                  key={item.index}
                  ref={(element) => {
                    if (element) pagesRef.current.set(item.index, element);
                    else pagesRef.current.delete(item.index);
                  }}
                  tabIndex={item.index === 0 ? -1 : undefined}
                  data-page-index={item.index}
                  className="reader-pages__slot"
                  style={{ "--reader-zoom": zoom } as CSSProperties}
                >
                  <ReaderPageImage
                    url={preloaded.has(item.index) ? item.url : null}
                    alt={pageAlt(item.index)}
                    failedMessage={format(copy.manga.reader.pageFailed, {
                      number: item.index + 1,
                    })}
                  />
                </div>
              ))}
              {endOfChapter}
            </div>
          )}
        </div>
      ) : (
        <div
          className={`reader-stage reader-stage--fit-${fit}${direction === "rtl" ? " reader-stage--rtl" : ""}`}
        >
          {status === "loading" ? (
            <div className="reader-status">
              <Spinner label={copy.manga.reader.loading} />
            </div>
          ) : status === "error" ? (
            <div className="reader-status">
              <p className="reader-error" role="alert">
                {error}
              </p>
              <div className="reader-status__actions">
                <Button variant="primary" onClick={() => setAttempt((value) => value + 1)}>
                  {copy.manga.reader.retry}
                </Button>
                <Button variant="quiet" onClick={onClose}>
                  {copy.manga.reader.back}
                </Button>
              </div>
            </div>
          ) : visiblePages.length === 0 ? (
            <div className="reader-status">
              <p className="reader-error">{copy.manga.reader.error}</p>
            </div>
          ) : (
            <>
              <div className="reader-stage__spread" tabIndex={-1} ref={spreadRef} style={{ "--reader-zoom": zoom } as CSSProperties}>
                {visiblePages.map((pageIndex) => {
                  const item = pages[pageIndex];
                  return (
                    <ReaderPageImage
                      key={item.index}
                      url={preloaded.has(item.index) ? item.url : null}
                      alt={pageAlt(item.index)}
                      failedMessage={format(copy.manga.reader.pageFailed, {
                        number: item.index + 1,
                      })}
                    />
                  );
                })}
              </div>
              {endOfChapter}
            </>
          )}
        </div>
      )}
    </div>
  );
}
