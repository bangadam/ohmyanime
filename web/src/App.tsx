import { useEffect, useRef, useState, type ReactNode } from "react";
import {
  api,
  APIError,
  errorMessage,
  id,
  initialize,
  json,
  type Anime,
  type Episode,
  type HistoryItem,
  type Page,
  type Provider,
  type Settings,
} from "./api";
import { copy } from "./copy";
import type { ListFilter, ListSort, LoadState, NavKey, ScheduleEntry } from "./types";
import { AppShell, ConfirmDialog, ErrorState, Notice, Spinner } from "./components";
import Home from "./pages/Home";
import Search from "./pages/Search";
import Season from "./pages/Season";
import Schedule from "./pages/Schedule";
import Details from "./pages/Details";
import MyList from "./pages/MyList";
import History from "./pages/History";
import Preferences from "./pages/Preferences";
import { Player } from "./Player";
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
type Genre = { id: string; name: string };

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
  const [view, setView] = useState<NavKey>("home");
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
  const [autoplayEpisode, setAutoplayEpisode] = useState<string | null>(null);

  // Home
  const [homeItems, setHomeItems] = useState<Anime[]>([]);
  const [homeHistory, setHomeHistory] = useState<HistoryItem[]>([]);
  const [homeCursor, setHomeCursor] = useState<string | null>(null);
  const [homeLoading, setHomeLoading] = useState(false);
  const [homeLoadingMore, setHomeLoadingMore] = useState(false);
  const [homeError, setHomeError] = useState("");

  // Search
  const [queryInput, setQueryInput] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [searchResults, setSearchResults] = useState<Anime[]>([]);
  const [searchCursor, setSearchCursor] = useState<string | null>(null);
  const [searchLoading, setSearchLoading] = useState(false);
  const [searchLoadingMore, setSearchLoadingMore] = useState(false);
  const [searchError, setSearchError] = useState("");

  // This season
  const [season, setSeason] = useState(currentSeason);
  const [year, setYear] = useState(() => new Date().getFullYear());
  const [genre, setGenre] = useState("");
  const [seasonItems, setSeasonItems] = useState<Anime[]>([]);
  const [seasonCursor, setSeasonCursor] = useState<string | null>(null);
  const [seasonLoading, setSeasonLoading] = useState(false);
  const [seasonLoadingMore, setSeasonLoadingMore] = useState(false);
  const [seasonError, setSeasonError] = useState("");

  // Release schedule (one GET /schedule; the page groups and filters by local weekday)
  const [scheduleEntries, setScheduleEntries] = useState<ScheduleEntry[]>([]);
  const [scheduleDay, setScheduleDay] = useState(() => new Date().getDay());
  const [scheduleLoading, setScheduleLoading] = useState(false);
  const [scheduleError, setScheduleError] = useState("");

  // My list
  const [listItems, setListItems] = useState<Anime[]>([]);
  const [listCursor, setListCursor] = useState<string | null>(null);
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

  const listController = useRef<AbortController | null>(null);
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

  useEffect(() => {
    void boot();
    return () => {
      detailController.current?.abort();
      listController.current?.abort();
    };
  }, []);

  // Load local preferences and catalogue filters once startup completes.
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
    void api<Page<Genre>>("/genres", { signal })
      .then((value) => {
        if (!signal.aborted) setGenres(value.items ?? []);
      })
      .catch(() => {
        /* Genre filters fall back to "All genres" when the list is unavailable. */
      });
    return () => controller.abort();
  }, [bootState]);

  // One abortable loader per view. Cursor pagination appends in the load-more handlers.
  useEffect(() => {
    if (bootState !== "ready") return;
    const controller = new AbortController();
    listController.current?.abort();
    listController.current = controller;
    const signal = controller.signal;
    if (view === "home") void loadHome(signal);
    else if (view === "search") void loadSearch(signal);
    else if (view === "season") void loadSeason(signal);
    else if (view === "schedule") void loadSchedule(signal);
    else if (view === "list") void loadList(signal);
    else if (view === "history") void loadHistory(signal);
    return () => {
      controller.abort();
      if (listController.current && listController.current !== controller) listController.current.abort();
      listController.current = null;
    };
  }, [bootState, view, searchQuery, genre, season, year, reload]);

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
          : view === "season"
            ? copy.catalog.seasonTitle
            : view === "schedule"
              ? copy.schedule.title
              : view === "list"
                ? copy.list.title
                : view === "history"
                  ? copy.history.title
                  : copy.preferences.title;
    document.title = (anime?.title ?? pageTitle) + " · " + copy.brandName;
  }, [bootState, view, anime?.title]);

  async function loadHome(signal: AbortSignal) {
    setHomeLoading(true);
    setHomeError("");
    setHomeItems([]);
    setHomeHistory([]);
    setHomeCursor(null);
    const [catalogueResult, historyResult] = await Promise.allSettled([
      api<Page<Anime>>("/catalog?limit=24", { signal }),
      api<Page<HistoryItem>>("/history?limit=12", { signal }),
    ]);
    if (signal.aborted) return;
    if (catalogueResult.status === "fulfilled") {
      setHomeItems(catalogueResult.value.items ?? []);
      setHomeCursor(catalogueResult.value.nextCursor ?? null);
    } else if (!isAbortError(catalogueResult.reason)) {
      setHomeError(errorMessage(catalogueResult.reason, copy.states.requestFailed));
    }
    if (historyResult.status === "fulfilled") {
      setHomeHistory(historyResult.value.items ?? []);
    }
    setHomeLoading(false);
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
    setSearchError("");
    if (!query) {
      setSearchLoading(false);
      return;
    }
    setSearchLoading(true);
    try {
      const params = new URLSearchParams({ limit: "24", query });
      const result = await api<Page<Anime>>(`/catalog?${params}`, { signal });
      if (signal.aborted) return;
      setSearchResults(result.items ?? []);
      setSearchCursor(result.nextCursor ?? null);
    } catch (reason) {
      if (!signal.aborted && !isAbortError(reason)) {
        setSearchError(errorMessage(reason, copy.states.requestFailed));
      }
    } finally {
      if (!signal.aborted) setSearchLoading(false);
    }
  }

  async function loadSearchMore() {
    if (!searchCursor || searchLoadingMore || !searchQuery.trim()) return;
    listController.current?.abort();
    const controller = new AbortController();
    listController.current = controller;
    setSearchLoadingMore(true);
    try {
      const params = new URLSearchParams({ limit: "24", query: searchQuery.trim(), cursor: searchCursor });
      const result = await api<Page<Anime>>(`/catalog?${params}`, { signal: controller.signal });
      if (controller.signal.aborted) return;
      setSearchResults((current) => [...current, ...(result.items ?? [])]);
      setSearchCursor(result.nextCursor ?? null);
    } catch (reason) {
      if (!controller.signal.aborted && !isAbortError(reason)) {
        setNotice({ message: errorMessage(reason, copy.states.requestFailed), tone: "error" });
      }
    } finally {
      if (!controller.signal.aborted) setSearchLoadingMore(false);
    }
  }

  async function loadSeason(signal: AbortSignal) {
    setSeasonLoading(true);
    setSeasonError("");
    setSeasonItems([]);
    setSeasonCursor(null);
    try {
      const params = new URLSearchParams({ limit: "24", season, year: String(year) });
      if (genre) params.set("genre", genre);
      const result = await api<Page<Anime>>(`/catalog?${params}`, { signal });
      if (signal.aborted) return;
      setSeasonItems(result.items ?? []);
      setSeasonCursor(result.nextCursor ?? null);
    } catch (reason) {
      if (!signal.aborted && !isAbortError(reason)) {
        setSeasonError(errorMessage(reason, copy.states.requestFailed));
      }
    } finally {
      if (!signal.aborted) setSeasonLoading(false);
    }
  }

  async function loadSeasonMore() {
    if (!seasonCursor || seasonLoadingMore) return;
    listController.current?.abort();
    const controller = new AbortController();
    listController.current = controller;
    setSeasonLoadingMore(true);
    try {
      const params = new URLSearchParams({ limit: "24", season, year: String(year), cursor: seasonCursor });
      if (genre) params.set("genre", genre);
      const result = await api<Page<Anime>>(`/catalog?${params}`, { signal: controller.signal });
      if (controller.signal.aborted) return;
      setSeasonItems((current) => [...current, ...(result.items ?? [])]);
      setSeasonCursor(result.nextCursor ?? null);
    } catch (reason) {
      if (!controller.signal.aborted && !isAbortError(reason)) {
        setNotice({ message: errorMessage(reason, copy.states.requestFailed), tone: "error" });
      }
    } finally {
      if (!controller.signal.aborted) setSeasonLoadingMore(false);
    }
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
    try {
      const result = await api<Page<Anime>>("/bookmarks?limit=24", { signal });
      if (signal.aborted) return;
      setListItems(result.items ?? []);
      setListCursor(result.nextCursor ?? null);
    } catch (reason) {
      if (!signal.aborted && !isAbortError(reason)) {
        setListError(errorMessage(reason, copy.states.requestFailed));
      }
    } finally {
      if (!signal.aborted) setListLoading(false);
    }
  }

  async function loadListMore() {
    if (!listCursor || listLoadingMore) return;
    listController.current?.abort();
    const controller = new AbortController();
    listController.current = controller;
    setListLoadingMore(true);
    try {
      const params = new URLSearchParams({ limit: "24", cursor: listCursor });
      const result = await api<Page<Anime>>(`/bookmarks?${params}`, { signal: controller.signal });
      if (controller.signal.aborted) return;
      setListItems((current) => [...current, ...(result.items ?? [])]);
      setListCursor(result.nextCursor ?? null);
    } catch (reason) {
      if (!controller.signal.aborted && !isAbortError(reason)) {
        setNotice({ message: errorMessage(reason, copy.states.requestFailed), tone: "error" });
      }
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
    setDetailState("idle");
    setDetailError("");
    setNotice(null);
    window.scrollTo({ top: 0 });
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

  async function removeBookmark(target: Anime) {
    if (removingId) return;
    setRemovingId(target.id);
    try {
      await api(`/bookmarks/${id(target.id)}`, { method: "DELETE" });
      setListItems((current) => current.filter((item) => item.id !== target.id));
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

  const feature = homeItems.find((item) => item.posterUrl) ?? homeItems[0] ?? null;

  function renderView() {
    if (view === "home") {
      return (
        <Home
          feature={feature}
          catalogue={homeItems}
          history={homeHistory}
          loading={homeLoading}
          loadingMore={homeLoadingMore}
          error={homeError}
          hasMore={homeCursor !== null}
          onOpen={(item, episodeId) => void openAnime(item, episodeId)}
          onViewSeason={() => navigate("season")}
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
          loading={searchLoading}
          loadingMore={searchLoadingMore}
          error={searchError}
          searched={searchQuery.trim().length > 0}
          hasMore={searchCursor !== null}
          onOpen={(item) => void openAnime(item)}
          onLoadMore={() => void loadSearchMore()}
          onRetry={retryView}
        />
      );
    }
    if (view === "season") {
      return (
        <Season
          season={season}
          year={year}
          genre={genre}
          seasons={seasons}
          genres={genres}
          onSeason={setSeason}
          onYear={setYear}
          onGenre={setGenre}
          items={seasonItems}
          loading={seasonLoading}
          loadingMore={seasonLoadingMore}
          error={seasonError}
          hasMore={seasonCursor !== null}
          onOpen={(item) => void openAnime(item)}
          onLoadMore={() => void loadSeasonMore()}
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
          onBrowseSeason={() => navigate("season")}
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
          hasMore={listCursor !== null}
          filter={listFilter}
          sort={listSort}
          removingId={removingId}
          onFilter={setListFilter}
          onSort={setListSort}
          onOpen={(item) => void openAnime(item)}
          onLoadMore={() => void loadListMore()}
          onRetry={retryView}
          onRemove={(item) => void removeBookmark(item)}
          onExplore={() => navigate("season")}
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
          onExplore={() => navigate("season")}
        />
      );
    }
    return (
      <Preferences
        settings={settings}
        providers={providers}
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
      view={anime ? null : view}
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
      {anime ? (
        <Details
          anime={anime}
          episodes={episodes}
          state={detailState}
          error={detailError}
          bookmarked={bookmarked}
          busy={bookmarkBusy}
          onBack={closeDetail}
          onPlay={openPlayer}
          onToggleBookmark={() => void toggleBookmark()}
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
