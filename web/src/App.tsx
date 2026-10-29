import { useEffect, useRef, useState, type ReactNode } from "react";
import {
  api,
  APIError,
  errorMessage,
  id,
  initialize,
  mangaChapters as fetchMangaChapters,
  refreshProvidersHealth,
  json,
  type Anime,
  type Episode,
  type HistoryItem,
  type Manga,
  type MangaChapter,
  type Page,
  type Provider,
  type ReadingProgress,
  type Settings,
} from "./api";
import { onTitleLink, type TitleLink } from "./deeplink";
import { copy } from "./copy";
import { catalogFiltersKey, defaultCatalogFilters, parseCatalogHash, serializeCatalogHash } from "./filters-url";
import type {
  CatalogFilters,
  Genre,
  LibraryEntry,
  ListFilter,
  ListSort,
  LoadState,
  NavKey,
  ScheduleEntry,
  SearchKind,
  Tag,
} from "./types";
import { AppShell, ConfirmDialog, ErrorState, Notice, Spinner } from "./components";
import Home from "./pages/Home";
import Search from "./pages/Search";
import Browse from "./pages/Browse";
import Schedule from "./pages/Schedule";
import Details from "./pages/Details";
import MyList from "./pages/MyList";
import Preferences from "./pages/Preferences";
import History from "./pages/History";

import { Player } from "./Player";
import MangaPage from "./pages/Manga";
import MangaDetail from "./pages/MangaDetail";
import { Reader } from "./Reader";
import { focusElement, isAbortError, localTimeZoneLabel } from "./utils";
import { useThemeController } from "./theme";
import "./styles/app.css";

/** Reset target for PATCH /settings; playback speed and theme are never sent. */
const defaults: Settings = {
  quality: "auto",
  subtitleLanguage: "id",
  autoplay: false,
  preferredProvider: "auto",
  revision: 0,
};

const seasons = ["winter", "spring", "summer", "fall"] as const;

type BootState = "loading" | "ready" | "error";
type NoticeState = { message: string; tone: "status" | "error" } | null;
type ConfirmKind = "history" | "list" | "cache" | null;


function currentSeason(): string {
  return seasons[Math.min(3, Math.max(0, Math.floor(new Date().getMonth() / 3)))];
}

function BootFrame({ children }: { children: ReactNode }) {
  return (
    <main className="app-boot">
      <p className="app-boot__wordmark">{copy.brand}</p>
      <div className="app-boot__panel">{children}</div>
    </main>
  );
}

export default function App() {
  // Startup and shell
  const [bootState, setBootState] = useState<BootState>("loading");
  const [bootError, setBootError] = useState("");
  const [view, setView] = useState<NavKey>(() => (parseCatalogHash(window.location.hash) ? "browse" : "home"));
  const [notice, setNotice] = useState<NoticeState>(null);
  const [confirmKind, setConfirmKind] = useState<ConfirmKind>(null);
  const [theme, setTheme] = useThemeController();
  const [sessionSpeed, setSessionSpeed] = useState(1);
  const [reload, setReload] = useState(0);

  // Settings, providers, genres
  const [settings, setSettings] = useState<Settings>(defaults);
  const [settingsLoading, setSettingsLoading] = useState(false);
  const [settingsBusy, setSettingsBusy] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [providers, setProviders] = useState<Provider[]>([]);
  const [providerHealthBusy, setProviderHealthBusy] = useState(false);
  const [providerHealthError, setProviderHealthError] = useState("");
  const [genres, setGenres] = useState<Genre[]>([]);

  // Title detail overlay and player
  const [anime, setAnime] = useState<Anime | null>(null);
  const [episode, setEpisode] = useState<Episode | null>(null);
  const [episodes, setEpisodes] = useState<Episode[]>([]);
  const [detailState, setDetailState] = useState<LoadState>("idle");
  const [detailError, setDetailError] = useState("");
  const [provider, setProvider] = useState("auto");
  const [mode, setMode] = useState("sub");
  const [bookmarked, setBookmarked] = useState(false);
  const [bookmarkBusy, setBookmarkBusy] = useState(false);
  const [watched, setWatched] = useState<Record<string, boolean>>({});
  const [watchedBusy, setWatchedBusy] = useState(false);
  const [autoplayEpisode, setAutoplayEpisode] = useState<string | null>(null);

  // Home
  const [homeItems, setHomeItems] = useState<Anime[]>([]);
  const [homeHistory, setHomeHistory] = useState<HistoryItem[]>([]);
  const [homeCursor, setHomeCursor] = useState<string | null>(null);
  const [homeLoading, setHomeLoading] = useState(false);
  const [homeLoadingMore, setHomeLoadingMore] = useState(false);
  const [homeError, setHomeError] = useState("");

  const [homeStale, setHomeStale] = useState(false);
  // Search
  const [queryInput, setQueryInput] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [searchResults, setSearchResults] = useState<Anime[]>([]);
  const [searchCursor, setSearchCursor] = useState<string | null>(null);
  const [searchLoading, setSearchLoading] = useState(false);
  const [searchLoadingMore, setSearchLoadingMore] = useState(false);
  const [searchError, setSearchError] = useState("");

  const [searchStale, setSearchStale] = useState(false);
  // Unified search: manga results ride alongside anime results so one failed
  // source can never hide the other kind's successful results.
  const [searchKind, setSearchKind] = useState<SearchKind>("all");
  const [searchMangaResults, setSearchMangaResults] = useState<Manga[]>([]);
  const [searchMangaCursor, setSearchMangaCursor] = useState<string | null>(null);
  // Manga search (empty query returns popular titles)
  const [mangaQueryInput, setMangaQueryInput] = useState("");
  const [mangaQuery, setMangaQuery] = useState("");
  // Provider filter for manga searches. "all" keeps the aggregated view; any
  // other value scopes the request, and pagination follows that provider.
  const [mangaProvider, setMangaProvider] = useState("all");
  const [mangaResults, setMangaResults] = useState<Manga[]>([]);
  const [mangaCursor, setMangaCursor] = useState<string | null>(null);
  const [mangaLoading, setMangaLoading] = useState(false);
  const [mangaLoadingMore, setMangaLoadingMore] = useState(false);
  const [mangaError, setMangaError] = useState("");

  // Only providers that actually declare manga support become filter options,
  // so the control can never offer a source that cannot answer the query.
  const mangaProviderOptions = [
    { value: "all", label: copy.manga.providerAll },
    ...providers
      .filter((item) => item.capabilities?.kinds?.includes("manga"))
      .map((item) => ({ value: item.id, label: item.id.charAt(0).toUpperCase() + item.id.slice(1) })),
  ];

  // Manga detail overlay and reader
  const [mangaDetail, setMangaDetail] = useState<Manga | null>(null);
  const [mangaChapters, setMangaChapters] = useState<MangaChapter[]>([]);
  const [mangaResume, setMangaResume] = useState<string | null>(null);
  const [mangaDetailState, setMangaDetailState] = useState<LoadState>("idle");
  const [mangaDetailError, setMangaDetailError] = useState("");
  // Deep links: the desktop shell emits an allowlisted title link; until
  // startup completes the newest link is held and dispatched once ready, so
  // cold and warm launches take the same path.
  const [pendingTitleLink, setPendingTitleLink] = useState<TitleLink | null>(null);
  const [mangaChapter, setMangaChapter] = useState<MangaChapter | null>(null);
  const [mangaBookmarked, setMangaBookmarked] = useState(false);
  const [mangaBookmarkBusy, setMangaBookmarkBusy] = useState(false);



  // Catalogue browse: the draft is edited in the filter panel while the
  // applied copy drives requests, so typing never re-queries the catalogue.
  // The hash is read once on mount so a reload restores the same view.
  const [filters, setFilters] = useState<CatalogFilters>(
    () => parseCatalogHash(window.location.hash) ?? defaultCatalogFilters(),
  );
  const [applied, setApplied] = useState<CatalogFilters>(
    () => parseCatalogHash(window.location.hash) ?? defaultCatalogFilters(),
  );
  const appliedKey = catalogFiltersKey(applied);
  const [catalogItems, setCatalogItems] = useState<Anime[]>([]);
  const [catalogCursor, setCatalogCursor] = useState<string | null>(null);
  const [catalogLoading, setCatalogLoading] = useState(false);
  const [catalogLoadingMore, setCatalogLoadingMore] = useState(false);
  const [catalogError, setCatalogError] = useState("");
  const [catalogStale, setCatalogStale] = useState(false);
  // Facet vocabularies fail independently: one unavailable list must never
  // blank the other, and the retry reloads both.
  const [tags, setTags] = useState<Tag[]>([]);
  const [genresError, setGenresError] = useState("");
  const [tagsError, setTagsError] = useState("");
  const [facetsReload, setFacetsReload] = useState(0);
  // Release schedule (one GET /schedule; the page groups and filters by local weekday)
  const [scheduleEntries, setScheduleEntries] = useState<ScheduleEntry[]>([]);
  const [scheduleDay, setScheduleDay] = useState(() => new Date().getDay());
  const [scheduleLoading, setScheduleLoading] = useState(false);
  const [scheduleError, setScheduleError] = useState("");

  // My list: the unified library holds anime and manga bookmarks side by
  // side; each kind pages independently and merges in the view.
  const [listItems, setListItems] = useState<LibraryEntry[]>([]);
  const [listCursor, setListCursor] = useState<string | null>(null);
  const [listMangaCursor, setListMangaCursor] = useState<string | null>(null);
  const [listLoading, setListLoading] = useState(false);
  const [listLoadingMore, setListLoadingMore] = useState(false);
  const [listError, setListError] = useState("");
  const [listFilter, setListFilter] = useState<ListFilter>("all");
  const [listSort, setListSort] = useState<ListSort>("title-asc");
  const [removingId, setRemovingId] = useState<string | null>(null);

  // Watch history
  const [historyItems, setHistoryItems] = useState<HistoryItem[]>([]);
  const [historyCursor, setHistoryCursor] = useState<string | null>(null);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [historyLoadingMore, setHistoryLoadingMore] = useState(false);
  const [historyError, setHistoryError] = useState("");
  // Anime ids removed from the Continue watching shelf (hidden only; the
  // backend clears a hide when the title is watched again).
  const [continueHidden, setContinueHidden] = useState<string[]>([]);
  // Synchronous mirror of the draft filters: a summary chip removes a filter
  // and applies it in one click, before the new draft state is committed.
  const draftFilters = useRef<CatalogFilters>(filters);

  const listController = useRef<AbortController | null>(null);
  const mangaController = useRef<AbortController | null>(null);
  const detailController = useRef<AbortController | null>(null);
  const mainFocusReady = useRef(false);

  async function boot() {
    setBootState("loading");
    setBootError("");
    try {
      const capabilities = await initialize();
      setProviders(capabilities.providers);
      setBootState("ready");
    } catch (reason) {
      setBootError(errorMessage(reason, copy.startup.connectBody));
      setBootState("error");
    }
  }

  // Manual provider health refresh: one bounded homepage probe per provider.
  // Reachability evidence only — it never switches the selected provider.
  async function refreshProviderHealth() {
    if (providerHealthBusy) return;
    setProviderHealthBusy(true);
    setProviderHealthError("");
    try {
      setProviders(await refreshProvidersHealth());
    } catch (reason) {
      setProviderHealthError(errorMessage(reason, copy.preferences.healthRefreshError));
    } finally {
      setProviderHealthBusy(false);
    }
  }


  useEffect(() => {
    void boot();
    return () => {
      detailController.current?.abort();
      mangaController.current?.abort();
      listController.current?.abort();
    };
  }, []);

  // Title deep links from the desktop shell (URL scheme open and
  // single-instance forwarding both arrive as the same event).
  useEffect(() => onTitleLink((link) => setPendingTitleLink(link)), []);

  // Dispatch the held deep link once startup is complete: the link ID is
  // allowlisted Go-side, an unavailable title lands in the existing detail
  // error recovery, and any other kind is ignored.
  useEffect(() => {
    if (bootState !== "ready" || !pendingTitleLink) return;
    setPendingTitleLink(null);
    if (pendingTitleLink.kind === "manga") {
      void openManga({
        id: pendingTitleLink.titleId,
        title: pendingTitleLink.titleId,
        availability: [],
      });
    } else if (pendingTitleLink.kind === "anime") {
      void openAnime({
        id: pendingTitleLink.titleId,
        title: pendingTitleLink.titleId,
        availability: [],
      });
    }
  }, [bootState, pendingTitleLink]);

  // Load local preferences once startup completes.
  useEffect(() => {
    if (bootState !== "ready") return;
    const controller = new AbortController();
    const signal = controller.signal;
    setSettingsLoading(true);
    void api<Settings>("/settings", { signal })
      .then((value) => {
        if (signal.aborted) return;
        setSettings(value);
        setProvider(value.preferredProvider);
        setSaveError("");
      })
      .catch((reason: unknown) => {
        if (signal.aborted) return;
        setNotice({ message: errorMessage(reason, copy.preferences.loadError), tone: "error" });
      })
      .finally(() => {
        if (!signal.aborted) setSettingsLoading(false);
      });
    return () => controller.abort();
  }, [bootState]);

  // Facet vocabularies load once and again only when the user retries them, so
  // a retry never re-reads the preferences it cannot affect.
  useEffect(() => {
    if (bootState !== "ready") return;
    const controller = new AbortController();
    const signal = controller.signal;
    setGenresError("");
    setTagsError("");
    void api<Page<Genre>>("/genres", { signal })
      .then((value) => {
        if (!signal.aborted) setGenres(value.items ?? []);
      })
      .catch((reason: unknown) => {
        if (signal.aborted || isAbortError(reason)) return;
        setGenresError(copy.browse.genresUnavailable);
      });
    void api<Page<Tag>>("/tags", { signal })
      .then((value) => {
        if (!signal.aborted) setTags(value.items ?? []);
      })
      .catch((reason: unknown) => {
        if (signal.aborted || isAbortError(reason)) return;
        setTagsError(copy.browse.tagsUnavailable);
      });
    return () => controller.abort();
  }, [bootState, facetsReload]);

  // One abortable loader per view. Cursor pagination appends in the load-more handlers.
  useEffect(() => {
    if (bootState !== "ready") return;
    const controller = new AbortController();
    listController.current?.abort();
    listController.current = controller;
    const signal = controller.signal;
    if (view === "home") void loadHome(signal);
    else if (view === "search") void loadSearch(signal);
    else if (view === "manga") void loadManga(signal);
    else if (view === "browse") void loadBrowse(signal);
    else if (view === "schedule") void loadSchedule(signal);
    else if (view === "list") void loadList(signal);
    else if (view === "history") void loadHistory(signal);
    return () => {
      controller.abort();
      if (listController.current && listController.current !== controller) listController.current.abort();
      listController.current = null;
    };
  }, [bootState, view, searchQuery, mangaQuery, mangaProvider, appliedKey, reload]);

  // The address bar mirrors the applied filters so a reload restores the same
  // catalogue view; other destinations keep a clean URL.
  useEffect(() => {
    const hash = view === "browse" ? serializeCatalogHash(applied) : "";
    window.history.replaceState(
      window.history.state,
      "",
      window.location.pathname + window.location.search + hash,
    );
  }, [view, appliedKey]);

  // Move focus into the shell when the title overlay opens or closes. The player
  // owns focus while it replaces the shell. The first ready render keeps native
  // document focus so the first Tab reaches the skip link.
  useEffect(() => {
    if (bootState !== "ready") {
      mainFocusReady.current = false;
      return;
    }
    if (!mainFocusReady.current) {
      mainFocusReady.current = true;
      return;
    }
    focusElement("#main");
  }, [bootState, anime?.id]);

  // Keep the document title aligned with the visible destination.
  useEffect(() => {
    if (bootState === "loading") {
      document.title = copy.brandName;
      return;
    }
    if (bootState === "error") {
      document.title = copy.startup.connectTitle + " · " + copy.brandName;
      return;
    }
    const pageTitle =
      view === "home"
        ? copy.nav.home
        : view === "search"
          ? copy.search.title
          : view === "browse"
            ? copy.browse.title
            : view === "schedule"
              ? copy.schedule.title
              : view === "list"
                ? copy.list.title
                : view === "history"
                  ? copy.history.title
                  : view === "manga"
                    ? copy.manga.title
                    : view === "settings"
                      ? copy.preferences.title
                      : copy.brandName;
    document.title = (anime?.title ?? mangaDetail?.title ?? pageTitle) + " · " + copy.brandName;
  }, [bootState, view, anime?.title, mangaDetail?.title]);

  async function loadHome(signal: AbortSignal) {
    setHomeLoading(true);
    setHomeError("");
    setHomeStale(false);
    setHomeItems([]);
    setHomeHistory([]);
    setHomeCursor(null);
    const [catalogueResult, historyResult, hiddenResult] = await Promise.allSettled([
      api<Page<Anime>>("/catalog?limit=24", { signal }),
      api<Page<HistoryItem>>("/history?limit=12", { signal }),
      api<{ animeIds: string[] }>("/continue-hidden", { signal }),
    ]);
    if (signal.aborted) return;
    if (catalogueResult.status === "fulfilled") {
      setHomeItems(catalogueResult.value.items ?? []);
      setHomeCursor(catalogueResult.value.nextCursor ?? null);
      setHomeStale(catalogueResult.value.stale === true);
    } else if (!isAbortError(catalogueResult.reason)) {
      setHomeError(errorMessage(catalogueResult.reason, copy.states.requestFailed));
    }
    if (hiddenResult.status === "fulfilled") {
      setContinueHidden(hiddenResult.value.animeIds ?? []);
    }
    setHomeLoading(false);
  }

  async function removeFromContinue(item: HistoryItem) {
    try {
      await api(`/continue-hidden/${encodeURIComponent(item.anime.id)}`, { method: "PUT" });
      setContinueHidden((current) =>
        current.includes(item.anime.id) ? current : [...current, item.anime.id],
      );
      setNotice({ message: copy.home.removedFromContinue, tone: "status" });
    } catch (reason) {
      setNotice({ message: errorMessage(reason, copy.states.requestFailed), tone: "error" });
    }
  }

  async function loadHomeMore() {
    if (!homeCursor || homeLoadingMore) return;
    listController.current?.abort();
    const controller = new AbortController();
    listController.current = controller;
    setHomeLoadingMore(true);
    try {
      const params = new URLSearchParams({ limit: "24", cursor: homeCursor });
      const result = await api<Page<Anime>>(`/catalog?${params}`, { signal: controller.signal });
      if (controller.signal.aborted) return;
      setHomeItems((current) => [...current, ...(result.items ?? [])]);
      setHomeCursor(result.nextCursor ?? null);
    } catch (reason) {
      if (!controller.signal.aborted && !isAbortError(reason)) {
        setNotice({ message: errorMessage(reason, copy.states.requestFailed), tone: "error" });
      }
    } finally {
      if (!controller.signal.aborted) setHomeLoadingMore(false);
    }
  }

  async function loadSearch(signal: AbortSignal) {
    const query = searchQuery.trim();
    setSearchResults([]);
    setSearchCursor(null);
    setSearchMangaResults([]);
    setSearchMangaCursor(null);
    setSearchError("");
    setSearchStale(false);
    if (!query) {
      setSearchLoading(false);
      return;
    }
    setSearchLoading(true);
    // Both sources load in parallel; one failing never hides the other's
    // results (partial failure keeps the successful kind visible).
    const params = new URLSearchParams({ limit: "24", query });
    const mangaParams = new URLSearchParams(params);
    if (mangaProvider !== "all") mangaParams.set("provider", mangaProvider);
    const [animeResult, mangaResult] = await Promise.allSettled([
      api<Page<Anime>>(`/catalog?${params}`, { signal }),
      api<Page<Manga>>(`/manga?${mangaParams}`, { signal }),
    ]);
    if (signal.aborted) return;
    let animeFailed = false;
    let mangaFailed = false;
    let stale = false;
    let animeReason: unknown = null;
    if (animeResult.status === "fulfilled") {
      setSearchResults(animeResult.value.items ?? []);
      setSearchCursor(animeResult.value.nextCursor ?? null);
      stale = animeResult.value.stale === true;
    } else if (!isAbortError(animeResult.reason)) {
      animeFailed = true;
      animeReason = animeResult.reason;
    }
    if (mangaResult.status === "fulfilled") {
      setSearchMangaResults(mangaResult.value.items ?? []);
      setSearchMangaCursor(mangaResult.value.nextCursor ?? null);
    } else if (!isAbortError(mangaResult.reason)) {
      mangaFailed = true;
    }
    setSearchStale(stale);
    if (animeFailed && mangaFailed) setSearchError(errorMessage(animeReason, copy.states.requestFailed));
    else if (animeFailed || mangaFailed) setSearchError(copy.search.partialError);
    setSearchLoading(false);
  }

  async function loadSearchMore() {
    const query = searchQuery.trim();
    if ((!searchCursor && !searchMangaCursor) || searchLoadingMore || !query) return;
    listController.current?.abort();
    const controller = new AbortController();
    listController.current = controller;
    setSearchLoadingMore(true);
    try {
      const requests: Promise<void>[] = [];
      if (searchCursor) {
        const params = new URLSearchParams({ limit: "24", query, cursor: searchCursor });
        requests.push(
          api<Page<Anime>>(`/catalog?${params}`, { signal: controller.signal }).then((result) => {
            setSearchResults((current) => [...current, ...(result.items ?? [])]);
            setSearchCursor(result.nextCursor ?? null);
          }),
        );
      }
      if (searchMangaCursor) {
        const params = new URLSearchParams({ limit: "24", query, cursor: searchMangaCursor });
        if (mangaProvider !== "all") params.set("provider", mangaProvider);
        requests.push(
          api<Page<Manga>>(`/manga?${params}`, { signal: controller.signal }).then((result) => {
            setSearchMangaResults((current) => [...current, ...(result.items ?? [])]);
            setSearchMangaCursor(result.nextCursor ?? null);
          }),
        );
      }
      await Promise.allSettled(requests);
    } finally {
      if (!controller.signal.aborted) setSearchLoadingMore(false);
    }
  }


  async function loadManga(signal: AbortSignal) {
    const query = mangaQuery.trim();
    setMangaLoading(true);
    setMangaError("");
    setMangaResults([]);
    setMangaCursor(null);
    try {
      const params = new URLSearchParams({ limit: "24" });
      if (query) params.set("query", query);
      if (mangaProvider !== "all") params.set("provider", mangaProvider);
      const result = await api<Page<Manga>>(`/manga?${params}`, { signal });
      if (signal.aborted) return;
      setMangaResults(result.items ?? []);
      setMangaCursor(result.nextCursor ?? null);
    } catch (reason) {
      if (!signal.aborted && !isAbortError(reason)) {
        setMangaError(errorMessage(reason, copy.states.requestFailed));
      }
    } finally {
      if (!signal.aborted) setMangaLoading(false);
    }
  }

  async function loadMangaMore() {
    if (!mangaCursor || mangaLoadingMore) return;
    listController.current?.abort();
    const controller = new AbortController();
    listController.current = controller;
    setMangaLoadingMore(true);
    try {
      const params = new URLSearchParams({ limit: "24", cursor: mangaCursor });
      const query = mangaQuery.trim();
      if (query) params.set("query", query);
      if (mangaProvider !== "all") params.set("provider", mangaProvider);
      const result = await api<Page<Manga>>(`/manga?${params}`, { signal: controller.signal });
      if (controller.signal.aborted) return;
      setMangaResults((current) => [...current, ...(result.items ?? [])]);
      setMangaCursor(result.nextCursor ?? null);
    } catch (reason) {
      if (!controller.signal.aborted && !isAbortError(reason)) {
        setNotice({ message: errorMessage(reason, copy.states.requestFailed), tone: "error" });
      }
    } finally {
      if (!controller.signal.aborted) setMangaLoadingMore(false);
    }
  }

  async function openManga(selected: Manga) {
    mangaController.current?.abort();
    const controller = new AbortController();
    mangaController.current = controller;
    const signal = controller.signal;
    setMangaDetail(selected);
    setMangaChapters([]);
    setMangaDetailState("loading");
    setMangaDetailError("");
    setMangaChapter(null);
    setMangaResume(null);
    setMangaBookmarked(false);
    setNotice(null);
    window.scrollTo({ top: 0 });
    void mangaIsBookmarked(selected.id, signal)
      .then((saved) => {
        if (!signal.aborted) setMangaBookmarked(saved);
      })
      .catch((reason: unknown) => {
        // Bookmark state is a local enhancement; failure leaves the toggle off.
        if (!signal.aborted && !isAbortError(reason)) setMangaBookmarked(false);
      });
    // Resume reading: saved progress names the chapter to continue from.
    void api<ReadingProgress | null>(`/manga/${id(selected.id)}/reading-progress`, { signal })
      .then((progress) => {
        if (!signal.aborted && progress) setMangaResume(progress.chapterId);
      })
      .catch((reason: unknown) => {
        // No progress yet starts at the first chapter, like a fresh title.
        if (!signal.aborted && !isAbortError(reason)) setMangaResume(null);
      });
    try {
      const [detail, chapters] = await Promise.all([
        api<Manga>(`/manga/${id(selected.id)}`, { signal }),
        fetchMangaChapters(selected.id, { signal }),
      ]);
      if (signal.aborted) return;
      setMangaDetail(detail);
      setMangaChapters(chapters);
      setMangaDetailState("ready");
    } catch (reason) {
      if (!signal.aborted && !isAbortError(reason)) {
        setMangaDetailState("error");
        setMangaDetailError(errorMessage(reason, copy.states.requestFailed));
      }
    }
  }

  /** Bookmarked state for the open manga, paged from the manga bookmark list. */
  async function mangaIsBookmarked(mangaId: string, signal: AbortSignal): Promise<boolean> {
    let cursor: string | null = null;
    do {
      const page: Page<Manga> = await api<Page<Manga>>(
        `/manga/bookmarks?limit=50${cursor ? `&cursor=${id(cursor)}` : ""}`,
        { signal },
      );
      if (page.items.some((item) => item.id === mangaId)) return true;
      cursor = page.nextCursor;
    } while (cursor && !signal.aborted);
    return false;
  }

  async function toggleMangaBookmark() {
    if (!mangaDetail || mangaBookmarkBusy) return;
    const target = mangaDetail;
    const wasBookmarked = mangaBookmarked;
    setMangaBookmarkBusy(true);
    try {
      await api(`/manga/${id(target.id)}/bookmark`, { method: wasBookmarked ? "DELETE" : "PUT" });
      setMangaBookmarked(!wasBookmarked);
      setNotice({
        message: wasBookmarked ? copy.details.removed : copy.details.added,
        tone: "status",
      });
      setReload((value) => value + 1);
    } catch (reason) {
      setNotice({ message: errorMessage(reason, copy.details.bookmarkError), tone: "error" });
    } finally {
      setMangaBookmarkBusy(false);
    }
  }

  function closeManga() {
    mangaController.current?.abort();
    setMangaDetail(null);
    setMangaChapters([]);
    setMangaDetailState("idle");
    setMangaDetailError("");
    setMangaChapter(null);
    setMangaBookmarked(false);
    setNotice(null);
    window.scrollTo({ top: 0 });
  }

  function openMangaChapter(chapter: MangaChapter) {
    setMangaChapter(chapter);
  }

  function closeReader() {
    setMangaChapter(null);
  }

  function handleMangaInput(value: string) {
    setMangaQueryInput(value);
    if (!mangaLoading) return;
    listController.current?.abort();
    const controller = new AbortController();
    listController.current = controller;
    void loadManga(controller.signal);
  }

  function submitMangaSearch() {
    listController.current?.abort();
    listController.current = null;
    if (mangaLoading) setMangaLoading(false);
    setMangaQuery(mangaQueryInput.trim());
    setReload((value) => value + 1);
  }

  function clearMangaSearch() {
    listController.current?.abort();
    listController.current = null;
    setMangaLoading(false);
    setMangaQueryInput("");
    setMangaQuery("");
    setReload((value) => value + 1);
  }

  /**
   * One catalogue request in API terms. Facets travel as repeated params so the
   * backend ANDs them; defaults are omitted to keep the URL and cache key tight.
   */
  function catalogParams(next: CatalogFilters, cursor?: string | null): URLSearchParams {
    const params = new URLSearchParams({ limit: "24" });
    const query = next.query.trim();
    if (query) params.set("query", query);
    if (next.status !== "all") params.set("status", next.status);
    if (next.type !== "all") params.set("type", next.type);
    if (next.sort !== "popular") params.set("sort", next.sort);
    const season = next.season.trim();
    if (season) params.set("season", season);
    if (next.year > 0) params.set("year", String(next.year));
    for (const genre of next.genres) params.append("genre", genre);
    for (const tag of next.tags) params.append("tag", tag);
    if (cursor) params.set("cursor", cursor);
    return params;
  }

  async function loadBrowse(signal: AbortSignal) {
    setCatalogLoading(true);
    setCatalogError("");
    setCatalogStale(false);
    setCatalogItems([]);
    setCatalogCursor(null);
    try {
      const result = await api<Page<Anime>>(`/catalog?${catalogParams(applied)}`, { signal });
      if (signal.aborted) return;
      setCatalogItems(result.items ?? []);
      setCatalogCursor(result.nextCursor ?? null);
      setCatalogStale(result.stale === true);
    } catch (reason) {
      if (!signal.aborted && !isAbortError(reason)) {
        setCatalogError(errorMessage(reason, copy.states.requestFailed));
      }
    } finally {
      if (!signal.aborted) setCatalogLoading(false);
    }
  }

  async function loadBrowseMore() {
    if (!catalogCursor || catalogLoadingMore) return;
    listController.current?.abort();
    const controller = new AbortController();
    listController.current = controller;
    setCatalogLoadingMore(true);
    try {
      const result = await api<Page<Anime>>(`/catalog?${catalogParams(applied, catalogCursor)}`, {
        signal: controller.signal,
      });
      if (controller.signal.aborted) return;
      setCatalogItems((current) => [...current, ...(result.items ?? [])]);
      setCatalogCursor(result.nextCursor ?? null);
    } catch (reason) {
      if (!controller.signal.aborted && !isAbortError(reason)) {
        setNotice({ message: errorMessage(reason, copy.states.requestFailed), tone: "error" });
      }
    } finally {
      if (!controller.signal.aborted) setCatalogLoadingMore(false);
    }
  }

  function updateFilters(patch: Partial<CatalogFilters>) {
    const next = { ...draftFilters.current, ...patch };
    draftFilters.current = next;
    setFilters(next);
  }

  function applyFilters() {
    setApplied(draftFilters.current);
  }

  function applyPresetFilters(preset: CatalogFilters) {
    draftFilters.current = preset;
    setFilters(preset);
    setApplied(preset);
    navigate("browse");
  }

  function openSeasonPreset() {
    applyPresetFilters({
      ...defaultCatalogFilters(),
      season: currentSeason(),
      year: new Date().getFullYear(),
    });
  }

  function openGenreFilter(genre: string) {
    applyPresetFilters({ ...defaultCatalogFilters(), genres: [genre] });
  }

  function openTagFilter(tag: string) {
    applyPresetFilters({ ...defaultCatalogFilters(), tags: [tag] });
  }

  function clearFilters() {
    const cleared = defaultCatalogFilters();
    draftFilters.current = cleared;
    setFilters(cleared);
    setApplied(cleared);
  }

  function retryFacets() {
    setFacetsReload((value) => value + 1);
  }

  async function loadSchedule(signal: AbortSignal) {
    setScheduleLoading(true);
    setScheduleError("");
    try {
      const result = await api<Page<ScheduleEntry>>("/schedule", { signal });
      if (signal.aborted) return;
      setScheduleEntries(result.items ?? []);
    } catch (reason) {
      if (!signal.aborted && !isAbortError(reason)) {
        setScheduleEntries([]);
        setScheduleError(errorMessage(reason, copy.schedule.errorBody));
      }
    } finally {
      if (!signal.aborted) setScheduleLoading(false);
    }
  }

  async function loadList(signal: AbortSignal) {
    setListLoading(true);
    setListError("");
    setListItems([]);
    setListCursor(null);
    setListMangaCursor(null);
    // Both bookmark kinds load in parallel; a partial failure keeps the other
    // kind's saved titles visible with an error notice.
    const [animeResult, mangaResult] = await Promise.allSettled([
      api<Page<Anime>>("/bookmarks?limit=24", { signal }),
      api<Page<Manga>>("/manga/bookmarks?limit=24", { signal }),
    ]);
    if (signal.aborted) return;
    const entries: LibraryEntry[] = [];
    let animeFailed = false;
    let mangaFailed = false;
    let animeReason: unknown = null;
    if (animeResult.status === "fulfilled") {
      setListCursor(animeResult.value.nextCursor ?? null);
      for (const item of animeResult.value.items ?? []) entries.push({ kind: "anime", item, createdAt: "" });
    } else if (!isAbortError(animeResult.reason)) {
      animeFailed = true;
      animeReason = animeResult.reason;
    }
    if (mangaResult.status === "fulfilled") {
      setListMangaCursor(mangaResult.value.nextCursor ?? null);
      for (const item of mangaResult.value.items ?? []) entries.push({ kind: "manga", item, createdAt: "" });
    } else if (!isAbortError(mangaResult.reason)) {
      mangaFailed = true;
    }
    setListItems(entries);
    if (animeFailed && mangaFailed) setListError(errorMessage(animeReason, copy.states.requestFailed));
    else if (animeFailed || mangaFailed) setListError(copy.search.partialError);
    setListLoading(false);
  }

  async function loadListMore() {
    if ((!listCursor && !listMangaCursor) || listLoadingMore) return;
    listController.current?.abort();
    const controller = new AbortController();
    listController.current = controller;
    setListLoadingMore(true);
    try {
      const requests: Promise<void>[] = [];
      if (listCursor) {
        const params = new URLSearchParams({ limit: "24", cursor: listCursor });
        requests.push(
          api<Page<Anime>>(`/bookmarks?${params}`, { signal: controller.signal }).then((result) => {
            setListItems((current) => [
              ...current,
              ...(result.items ?? []).map((item) => ({ kind: "anime", item, createdAt: "" }) as LibraryEntry),
            ]);
            setListCursor(result.nextCursor ?? null);
          }),
        );
      }
      if (listMangaCursor) {
        const params = new URLSearchParams({ limit: "24", cursor: listMangaCursor });
        requests.push(
          api<Page<Manga>>(`/manga/bookmarks?${params}`, { signal: controller.signal }).then((result) => {
            setListItems((current) => [
              ...current,
              ...(result.items ?? []).map((item) => ({ kind: "manga", item, createdAt: "" }) as LibraryEntry),
            ]);
            setListMangaCursor(result.nextCursor ?? null);
          }),
        );
      }
      await Promise.allSettled(requests);
    } finally {
      if (!controller.signal.aborted) setListLoadingMore(false);
    }
  }

  async function loadHistory(signal: AbortSignal) {
    setHistoryLoading(true);
    setHistoryError("");
    setHistoryItems([]);
    setHistoryCursor(null);
    try {
      const result = await api<Page<HistoryItem>>("/history?limit=24", { signal });
      if (signal.aborted) return;
      setHistoryItems(result.items ?? []);
      setHistoryCursor(result.nextCursor ?? null);
    } catch (reason) {
      if (!signal.aborted && !isAbortError(reason)) {
        setHistoryError(errorMessage(reason, copy.states.requestFailed));
      }
    } finally {
      if (!signal.aborted) setHistoryLoading(false);
    }
  }

  async function loadHistoryMore() {
    if (!historyCursor || historyLoadingMore) return;
    listController.current?.abort();
    const controller = new AbortController();
    listController.current = controller;
    setHistoryLoadingMore(true);
    try {
      const params = new URLSearchParams({ limit: "24", cursor: historyCursor });
      const result = await api<Page<HistoryItem>>(`/history?${params}`, { signal: controller.signal });
      if (controller.signal.aborted) return;
      setHistoryItems((current) => [...current, ...(result.items ?? [])]);
      setHistoryCursor(result.nextCursor ?? null);
    } catch (reason) {
      if (!controller.signal.aborted && !isAbortError(reason)) {
        setNotice({ message: errorMessage(reason, copy.states.requestFailed), tone: "error" });
      }
    } finally {
      if (!controller.signal.aborted) setHistoryLoadingMore(false);
    }
  }

  async function openAnime(selected: Anime, startEpisodeId?: string) {
    detailController.current?.abort();
    const controller = new AbortController();
    detailController.current = controller;
    const signal = controller.signal;
    setAutoplayEpisode(null);
    setAnime(selected);
    setEpisode(null);
    setEpisodes([]);
    setDetailState("loading");
    setDetailError("");
    setNotice(null);
    setBookmarked(false);
    setWatched({});
    setProvider(settings.preferredProvider);
    window.scrollTo({ top: 0 });
    try {
      const detailRequest = api<Anime>(`/anime/${id(selected.id)}`, { signal }).then((detail) => {
        if (!signal.aborted) setAnime(detail);
      });
      const bookmarkRequest = (async () => {
        let cursor: string | null = null;
        do {
          const page: Page<Anime> = await api<Page<Anime>>(
            `/bookmarks?limit=50${cursor ? `&cursor=${id(cursor)}` : ""}`,
            { signal },
          );
          if (page.items.some((item) => item.id === selected.id)) {
            if (!signal.aborted) setBookmarked(true);
            return;
          }
          cursor = page.nextCursor;
        } while (cursor && !signal.aborted);
      })();
      void bookmarkRequest.catch((reason: unknown) => {
        if (!signal.aborted && !isAbortError(reason)) {
          setNotice({ message: errorMessage(reason, copy.list.errorTitle), tone: "error" });
        }
      });
      const episodeRequest = (async () => {
        const page: Page<Episode> = await api<Page<Episode>>(`/anime/${id(selected.id)}/episodes`, { signal });
        return page.items ?? [];
      })();
      void api<{ episodes: { episodeId: string; watched: boolean }[] }>(
        `/watched?animeId=${id(selected.id)}`,
        { signal },
      )
        .then((value) => {
          if (signal.aborted) return;
          const flags: Record<string, boolean> = {};
          for (const item of value.episodes ?? []) flags[item.episodeId] = item.watched;
          setWatched(flags);
        })
        .catch((reason: unknown) => {
          // Watched flags are a local enhancement; a failure never blocks the
          // detail view, it only leaves the toggles off until a retry.
          if (!signal.aborted && !isAbortError(reason)) setWatched({});
        });
      const [allEpisodes] = await Promise.all([episodeRequest, detailRequest]);
      if (signal.aborted) return;
      const ordered = [...allEpisodes].sort((a, b) => a.ordinal - b.ordinal);
      setEpisodes(ordered);
      setAnime((current) =>
        current?.id === selected.id
          ? {
              ...current,
              availability: [
                ...new Set([
                  ...(current.availability ?? []),
                  ...ordered.flatMap((item) => item.availability ?? []),
                ]),
              ],
            }
          : current,
      );
      setDetailState("ready");
      if (startEpisodeId) {
        const target = ordered.find((item) => item.id === startEpisodeId);
        if (target) {
          setAutoplayEpisode(target.id);
          setEpisode(target);
        }
      }
    } catch (reason) {
      if (!signal.aborted && !isAbortError(reason)) {
        setDetailState("error");
        setDetailError(errorMessage(reason, copy.states.requestFailed));
      }
    }
  }

  function closeDetail() {
    detailController.current?.abort();
    setAnime(null);
    setEpisode(null);
    setEpisodes([]);
    setAutoplayEpisode(null);
    setWatched({});
    setDetailState("idle");
    setDetailError("");
    setNotice(null);
    window.scrollTo({ top: 0 });
  }

  async function toggleWatched(episodeId: string, next: boolean) {
    if (watchedBusy) return;
    setWatchedBusy(true);
    try {
      await api(`/watched/${id(episodeId)}`, { method: next ? "PUT" : "DELETE" });
      setWatched((current) => ({ ...current, [episodeId]: next }));
      setNotice({
        message: next ? copy.details.markedWatched : copy.details.markedUnwatched,
        tone: "status",
      });
    } catch (reason) {
      setNotice({ message: errorMessage(reason, copy.details.watchedError), tone: "error" });
    } finally {
      setWatchedBusy(false);
    }
  }

  async function bulkWatched(next: boolean) {
    if (!anime || watchedBusy) return;
    const target = anime;
    setWatchedBusy(true);
    try {
      await api("/watched/bulk", { method: "POST", body: json({ animeId: target.id, watched: next }) });
      setWatched((current) => {
        const flags: Record<string, boolean> = { ...current };
        for (const item of episodes) flags[item.id] = next;
        return flags;
      });
      setNotice({
        message: next ? copy.details.markedWatched : copy.details.markedUnwatched,
        tone: "status",
      });
    } catch (reason) {
      setNotice({ message: errorMessage(reason, copy.details.watchedError), tone: "error" });
    } finally {
      setWatchedBusy(false);
    }
  }

  async function toggleBookmark() {
    if (!anime || bookmarkBusy) return;
    const target = anime;
    const wasBookmarked = bookmarked;
    setBookmarkBusy(true);
    try {
      await api(`/bookmarks/${id(target.id)}`, { method: wasBookmarked ? "DELETE" : "PUT" });
      setBookmarked(!wasBookmarked);
      setNotice({
        message: wasBookmarked ? copy.details.removed : copy.details.added,
        tone: "status",
      });
      setReload((value) => value + 1);
    } catch (reason) {
      setNotice({ message: errorMessage(reason, copy.details.bookmarkError), tone: "error" });
    } finally {
      setBookmarkBusy(false);
    }
  }

  async function removeBookmark(target: LibraryEntry) {
    if (removingId) return;
    const targetId = target.item.id;
    setRemovingId(targetId);
    try {
      const path = target.kind === "manga" ? `/manga/${id(targetId)}/bookmark` : `/bookmarks/${id(targetId)}`;
      await api(path, { method: "DELETE" });
      setListItems((current) => current.filter((entry) => entry.item.id !== targetId));
      setNotice({ message: copy.list.removed, tone: "status" });
    } catch (reason) {
      setNotice({ message: errorMessage(reason, copy.details.bookmarkError), tone: "error" });
    } finally {
      setRemovingId(null);
    }
  }

  async function saveSettings(next: Settings) {
    if (settingsBusy) return;
    setSettingsBusy(true);
    setSaveError("");
    try {
      const saved = await api<Settings>("/settings", {
        method: "PATCH",
        body: json({
          subtitleLanguage: next.subtitleLanguage,
          quality: next.quality,
          autoplay: next.autoplay,
          preferredProvider: next.preferredProvider,
          revision: next.revision,
        }),
      });
      setSettings(saved);
      setNotice({ message: copy.preferences.saved, tone: "status" });
    } catch (reason) {
      if (reason instanceof APIError && reason.status === 409) setSaveError(copy.preferences.conflict);
      else setSaveError(errorMessage(reason, copy.preferences.saveError));
    } finally {
      setSettingsBusy(false);
    }
  }

  async function reloadSettings() {
    if (settingsLoading) return;
    setSettingsLoading(true);
    setSaveError("");
    try {
      const value = await api<Settings>("/settings");
      setSettings(value);
      setNotice({ message: copy.preferences.reloaded, tone: "status" });
    } catch (reason) {
      setSaveError(errorMessage(reason, copy.preferences.loadError));
    } finally {
      setSettingsLoading(false);
    }
  }

  async function confirmClear() {
    const kind = confirmKind;
    if (!kind) return;
    setConfirmKind(null);
    setSettingsBusy(true);
    try {
      if (kind === "history") {
        await api("/history?confirmed=true", { method: "DELETE" });
        setHistoryItems([]);
        setHomeHistory([]);
        setNotice({ message: copy.history.cleared, tone: "status" });
      } else if (kind === "list") {
        await api("/bookmarks?confirmed=true", { method: "DELETE" });
        setListItems([]);
        setBookmarked(false);
        setNotice({ message: copy.preferences.listCleared, tone: "status" });
      } else {
        await api("/cache?confirmed=true", { method: "DELETE" });
        setNotice({ message: copy.preferences.cacheCleared, tone: "status" });
      }
      setReload((value) => value + 1);
    } catch (reason) {
      setNotice({ message: errorMessage(reason, copy.states.requestFailed), tone: "error" });
    } finally {
      setSettingsBusy(false);
    }
  }

  function navigate(target: NavKey) {
    detailController.current?.abort();
    setAnime(null);
    setEpisode(null);
    setEpisodes([]);
    setAutoplayEpisode(null);
    setDetailState("idle");
    setDetailError("");
    mangaController.current?.abort();
    setMangaDetail(null);
    setMangaChapters([]);
    setMangaDetailState("idle");
    setMangaDetailError("");
    setMangaChapter(null);
    setConfirmKind(null);
    setNotice(null);
    setSaveError("");
    setView(target);
    window.scrollTo({ top: 0 });
  }

  function handleSearchInput(value: string) {
    setQueryInput(value);
    // Abort any in-flight request for the submitted query and restart it so the
    // visible results always belong to the query the user actually submitted.
    if (!searchLoading) return;
    listController.current?.abort();
    const controller = new AbortController();
    listController.current = controller;
    void loadSearch(controller.signal);
  }

  function submitSearch() {
    const query = queryInput.trim();
    listController.current?.abort();
    listController.current = null;
    if (searchLoading) setSearchLoading(false);
    setSearchQuery(query);
    setReload((value) => value + 1);
  }

  function clearSearch() {
    listController.current?.abort();
    listController.current = null;
    setSearchLoading(false);
    setQueryInput("");
    setSearchQuery("");
    setSearchMangaResults([]);
    setSearchMangaCursor(null);
    setReload((value) => value + 1);
  }

  function openSearch() {
    navigate("search");
    focusElement("#search-query");
  }

  function openPlayer(next: Episode) {
    // A direct Play action is explicit play intent, not autoplay-next preference.
    setAutoplayEpisode(next.id);
    setEpisode(next);
  }

  function changeEpisode(next: Episode) {
    // Manual previous/next navigation also starts the chosen episode.
    setAutoplayEpisode(next.id);
    setEpisode(next);
  }

  function closePlayer() {
    setAutoplayEpisode(null);
    setEpisode(null);
  }

  function retryView() {
    setReload((value) => value + 1);
  }

  if (bootState === "loading") {
    return (
      <BootFrame>
        <Spinner label={copy.startup.loading} />
      </BootFrame>
    );
  }

  if (bootState === "error") {
    return (
      <BootFrame>
        <ErrorState
          title={copy.startup.connectTitle}
          description={bootError}
          retry={() => void boot()}
          retryLabel={copy.startup.retry}
        />
      </BootFrame>
    );
  }

  if (anime && episode) {
    return (
      <Player
        key={`${episode.id}:${provider}:${mode}`}
        anime={anime}
        episode={episode}
        episodes={episodes}
        settings={settings}
        provider={provider}
        mode={mode}
        providers={providers}
        startPlaying={autoplayEpisode === episode.id}
        sessionSpeed={sessionSpeed}
        onSpeedChange={setSessionSpeed}
        onProviderChange={setProvider}
        onEpisode={changeEpisode}
        onClose={closePlayer}
      />
    );
  }

  if (mangaDetail && mangaChapter) {
    return (
      <Reader
        manga={mangaDetail}
        chapter={mangaChapter}
        chapters={mangaChapters}
        onChapter={openMangaChapter}
        onClose={closeReader}
      />
    );
  }


  const feature = homeItems.find((item) => item.posterUrl) ?? homeItems[0] ?? null;

  function renderView() {
    if (view === "home") {
      return (
        <Home
          feature={feature}
          catalogue={homeItems}
          history={homeHistory.filter((item) => !continueHidden.includes(item.anime.id))}
          onRemoveFromContinue={(item) => void removeFromContinue(item)}
          loading={homeLoading}
          loadingMore={homeLoadingMore}
          error={homeError}
          stale={homeStale}
          hasMore={homeCursor !== null}
          onOpen={(item, episodeId) => void openAnime(item, episodeId)}
          onViewSeason={openSeasonPreset}
          onViewHistory={() => navigate("history")}
          onLoadMore={() => void loadHomeMore()}
          onRetry={retryView}
        />
      );
    }
    if (view === "search") {
      return (
        <Search
          input={queryInput}
          query={searchQuery}
          onInput={handleSearchInput}
          onSubmit={submitSearch}
          onClear={clearSearch}
          results={searchResults}
          mangaResults={searchMangaResults}
          kind={searchKind}
          onKind={setSearchKind}
          mangaProvider={mangaProvider}
          onMangaProvider={(value) => setMangaProvider(value)}
          mangaProviderOptions={mangaProviderOptions}
          loading={searchLoading}
          loadingMore={searchLoadingMore}
          error={searchError}
          stale={searchStale}
          searched={searchQuery.trim().length > 0}
          hasMore={searchCursor !== null || searchMangaCursor !== null}
          onOpen={(item, kind) => void (kind === "manga" ? openManga(item as Manga) : openAnime(item as Anime))}
          onLoadMore={() => void loadSearchMore()}
          onRetry={retryView}
        />
      );
    }
    if (view === "manga") {
      return (
        <MangaPage
          input={mangaQueryInput}
          query={mangaQuery}
          onInput={handleMangaInput}
          onSubmit={submitMangaSearch}
          onClear={clearMangaSearch}
          results={mangaResults}
          loading={mangaLoading}
          loadingMore={mangaLoadingMore}
          error={mangaError}
          hasMore={mangaCursor !== null}
          onOpen={(item) => void openManga(item)}
          onLoadMore={() => void loadMangaMore()}
          provider={mangaProvider}
          onProvider={(value) => setMangaProvider(value)}
          providerOptions={mangaProviderOptions}
          onRetry={retryView}
        />
      );
    }

    if (view === "browse") {
      return (
        <Browse
          filters={filters}
          applied={applied}
          onFilters={updateFilters}
          onApply={applyFilters}
          onClear={clearFilters}
          seasons={seasons}
          genres={genres}
          genresError={genresError}
          tags={tags}
          tagsError={tagsError}
          onRetryFacets={retryFacets}
          items={catalogItems}
          loading={catalogLoading}
          loadingMore={catalogLoadingMore}
          error={catalogError}
          stale={catalogStale}
          hasMore={catalogCursor !== null}
          onOpen={(item) => void openAnime(item)}
          onLoadMore={() => void loadBrowseMore()}
          onRetry={retryView}
        />
      );
    }
    if (view === "schedule") {
      return (
        <Schedule
          entries={scheduleEntries}
          day={scheduleDay}
          loading={scheduleLoading}
          error={scheduleError}
          localTimeZone={localTimeZoneLabel()}
          onDay={setScheduleDay}
          onOpen={(item) => void openAnime(item)}
          onBrowseSeason={openSeasonPreset}
          onRetry={retryView}
        />
      );
    }
    if (view === "list") {
      return (
        <MyList
          items={listItems}
          loading={listLoading}
          loadingMore={listLoadingMore}
          error={listError}
          hasMore={listCursor !== null || listMangaCursor !== null}
          filter={listFilter}
          sort={listSort}
          removingId={removingId}
          onFilter={setListFilter}
          onSort={setListSort}
          onOpen={(entry) => void (entry.kind === "manga" ? openManga(entry.item) : openAnime(entry.item))}
          onLoadMore={() => void loadListMore()}
          onRetry={retryView}
          onRemove={(entry) => void removeBookmark(entry)}
          onExplore={() => navigate("browse")}
        />
      );
    }
    if (view === "history") {
      return (
        <History
          items={historyItems}
          loading={historyLoading}
          loadingMore={historyLoadingMore}
          error={historyError}
          hasMore={historyCursor !== null}
          onResume={(item) => void openAnime(item.anime, item.episode.id)}
          onEpisodes={(item) => void openAnime(item.anime)}
          onClear={() => setConfirmKind("history")}
          onLoadMore={() => void loadHistoryMore()}
          onRetry={retryView}
          onExplore={() => navigate("browse")}
        />
      );
    }
    return (
      <Preferences
        settings={settings}
        providers={providers}
        healthBusy={providerHealthBusy}
        healthError={providerHealthError}
        onRefreshProviderHealth={() => void refreshProviderHealth()}
        theme={theme}
        sessionSpeed={sessionSpeed}
        busy={settingsBusy}
        loading={settingsLoading}
        saveError={saveError}
        onTheme={setTheme}
        onSave={(next) => void saveSettings(next)}
        onReset={() => void saveSettings({ ...defaults, revision: settings.revision })}
        onReload={() => void reloadSettings()}
        onSpeedChange={setSessionSpeed}
        onClearHistory={() => setConfirmKind("history")}
        onClearList={() => setConfirmKind("list")}
        onClearCache={() => setConfirmKind("cache")}
        playing={episode !== null}
      />
    );
  }

  const confirm =
    confirmKind === "history"
      ? {
          title: copy.history.clearConfirmTitle,
          description: copy.history.clearConfirmBody,
          confirmLabel: copy.history.clearConfirmAction,
        }
      : confirmKind === "list"
        ? {
            title: copy.preferences.confirmListTitle,
            description: copy.preferences.confirmListBody,
            confirmLabel: copy.preferences.clearList,
          }
        : confirmKind === "cache"
          ? {
              title: copy.preferences.confirmCacheTitle,
              description: copy.preferences.confirmCacheBody,
              confirmLabel: copy.preferences.clearCache,
            }
          : null;

  return (
    <AppShell
      view={anime || mangaDetail ? null : view}
      onNavigate={navigate}
      onSearch={openSearch}
    >
      {notice ? (
        <div className="app-notices">
          <Notice
            message={notice.message}
            tone={notice.tone}
            onDismiss={() => setNotice(null)}
            dismissLabel={copy.states.closeNotice}
          />
        </div>
      ) : null}
      {mangaDetail ? (
        <MangaDetail
          manga={mangaDetail}
          chapters={mangaChapters}
          state={mangaDetailState}
          error={mangaDetailError}
          bookmarked={mangaBookmarked}
          bookmarkBusy={mangaBookmarkBusy}
          resumeChapterId={mangaResume}
          onBack={closeManga}
          onRead={openMangaChapter}
          onToggleBookmark={() => void toggleMangaBookmark()}
          onRetry={() => void openManga(mangaDetail)}
        />
      ) : anime ? (
        <Details
          anime={anime}
          episodes={episodes}
          state={detailState}
          error={detailError}
          bookmarked={bookmarked}
          busy={bookmarkBusy}
          watched={watched}
          watchedBusy={watchedBusy}
          onBack={closeDetail}
          onPlay={openPlayer}
          onToggleBookmark={() => void toggleBookmark()}
          onToggleWatched={(episodeId, next) => void toggleWatched(episodeId, next)}
          onBulkWatched={(next) => void bulkWatched(next)}
          onFilterByGenre={openGenreFilter}
          onFilterByTag={openTagFilter}
          onRetry={() => void openAnime(anime)}
        />
      ) : (
        renderView()
      )}
      {confirm ? (
        <ConfirmDialog
          open
          title={confirm.title}
          description={confirm.description}
          confirmLabel={confirm.confirmLabel}
          cancelLabel={copy.preferences.cancel}
          destructive
          onConfirm={() => void confirmClear()}
          onCancel={() => setConfirmKind(null)}
        />
      ) : null}
    </AppShell>
  );
}
