import type { Anime, Episode, HistoryItem, Manga, MangaChapter, Provider, Settings } from "./api";
import type { ThemeMode } from "./theme";

export type NavKey = "home" | "manga" | "search" | "browse" | "schedule" | "list" | "history" | "settings";
export type LoadState = "idle" | "loading" | "ready" | "error";
export type ListFilter = "all" | "anime" | "manga" | "series" | "movies";
export type SearchKind = "all" | "anime" | "manga";
export type ListSort = "title-asc" | "title-desc";
/** One saved title in the unified library; anime and manga ids are namespaced
 * ("anilist:…" vs "mangadex:…"), so identical titles never merge identities. */
export type LibraryEntry =
  | { kind: "anime"; item: Anime; createdAt: string }
  | { kind: "manga"; item: Manga; createdAt: string };
export type ScheduleEntry = { id: number; airingAt: number; episode: number; anime: Anime };

export interface HomePageProps {
  feature: Anime | null;
  catalogue: Anime[];
  history: HistoryItem[];
  onRemoveFromContinue: (item: HistoryItem) => void;
  loading: boolean;
  loadingMore: boolean;
  error: string;
  stale: boolean;
  hasMore: boolean;
  onOpen: (anime: Anime, episodeId?: string) => void;
  onViewSeason: () => void;
  onViewHistory: () => void;
  onLoadMore: () => void;
  onRetry: () => void;
}

export interface SearchPageProps {
  input: string;
  query: string;
  onInput: (value: string) => void;
  onSubmit: () => void;
  onClear: () => void;
  results: Anime[];
  mangaResults: Manga[];
  kind: SearchKind;
  onKind: (kind: SearchKind) => void;
  mangaProvider: string;
  onMangaProvider: (provider: string) => void;
  mangaProviderOptions: { value: string; label: string }[];
  loading: boolean;
  loadingMore: boolean;
  error: string;
  stale: boolean;
  searched: boolean;
  hasMore: boolean;
  onOpen: (item: Anime | Manga, kind: "anime" | "manga") => void;
  onLoadMore: () => void;
  onRetry: () => void;
}

export interface MangaPageProps {
  input: string;
  query: string;
  onInput: (value: string) => void;
  onSubmit: () => void;
  onClear: () => void;
  results: Manga[];
  loading: boolean;
  loadingMore: boolean;
  error: string;
  hasMore: boolean;
  provider: string;
  onProvider: (provider: string) => void;
  providerOptions: { value: string; label: string }[];
  onOpen: (manga: Manga) => void;
  onLoadMore: () => void;
  onRetry: () => void;
}

export interface MangaDetailPageProps {
  manga: Manga;
  chapters: MangaChapter[];
  state: LoadState;
  error: string;
  bookmarked: boolean;
  bookmarkBusy: boolean;
  /** Saved reading progress names the chapter to continue from. */
  resumeChapterId: string | null;
  onBack: () => void;
  onRead: (chapter: MangaChapter) => void;
  onToggleBookmark: () => void;
  onRetry: () => void;
}

export interface ReaderProps {
  manga: Manga;
  chapter: MangaChapter;
  chapters: MangaChapter[];
  onChapter: (chapter: MangaChapter) => void;
  onClose: () => void;
}

export interface SchedulePageProps {
  entries: ScheduleEntry[];
  day: number;
  loading: boolean;
  error: string;
  localTimeZone: string;
  onDay: (day: number) => void;
  onOpen: (anime: Anime) => void;
  onBrowseSeason: () => void;
  onRetry: () => void;
}

export interface ListPageProps {
  items: LibraryEntry[];
  loading: boolean;
  loadingMore: boolean;
  error: string;
  hasMore: boolean;
  filter: ListFilter;
  sort: ListSort;
  removingId: string | null;
  onFilter: (value: ListFilter) => void;
  onSort: (value: ListSort) => void;
  onOpen: (entry: LibraryEntry) => void;
  onLoadMore: () => void;
  onRetry: () => void;
  onRemove: (entry: LibraryEntry) => void;
  onExplore: () => void;
}

export interface HistoryPageProps {
  items: HistoryItem[];
  loading: boolean;
  loadingMore: boolean;
  error: string;
  hasMore: boolean;
  onResume: (item: HistoryItem) => void;
  onEpisodes: (item: HistoryItem) => void;
  onClear: () => void;
  onLoadMore: () => void;
  onRetry: () => void;
  onExplore: () => void;
}

export interface PreferencesPageProps {
  settings: Settings;
  providers: Provider[];
  healthBusy: boolean;
  healthError: string;
  onRefreshProviderHealth: () => void;
  theme: ThemeMode;
  sessionSpeed: number;
  busy: boolean;
  loading: boolean;
  saveError: string;
  onTheme: (mode: ThemeMode) => void;
  onSave: (next: Settings) => void;
  onReset: () => void;
  onReload: () => void;
  onSpeedChange: (speed: number) => void;
  onClearHistory: () => void;
  onClearList: () => void;
  onClearCache: () => void;
  /** Whether media is currently playing; gates restart consent wording. */
  playing?: boolean;
}

export interface DetailPageProps {
  anime: Anime;
  episodes: Episode[];
  state: LoadState;
  error: string;
  bookmarked: boolean;
  busy: boolean;
  watched: Record<string, boolean>;
  watchedBusy: boolean;
  onBack: () => void;
  onPlay: (episode: Episode) => void;
  onToggleBookmark: () => void;
  onToggleWatched: (episodeId: string, watched: boolean) => void;
  onBulkWatched: (watched: boolean) => void;
  onFilterByGenre: (genre: string) => void;
  onFilterByTag: (tag: string) => void;
  onRetry: () => void;
}

export type Genre = { id: string; name: string };
export type Tag = { id: string; name: string; category: string };

/** Facet values accepted by the catalogue query; "all" means unfiltered. */
export type CatalogStatus = "all" | "releasing" | "finished" | "not_yet_released" | "cancelled" | "hiatus";
export type CatalogType = "all" | "tv" | "tv_short" | "movie" | "special" | "ova" | "ona" | "music";
export type CatalogSort = "popular" | "trending" | "score" | "newest" | "updated" | "title_asc" | "title_desc";

/** Catalogue filters as edited in the browse sidebar. */
export interface CatalogFilters {
  query: string;
  status: CatalogStatus;
  type: CatalogType;
  sort: CatalogSort;
  genres: string[];
  tags: string[];
  season: string;
  /** 0 means no year filter. */
  year: number;
}

export interface BrowsePageProps {
  filters: CatalogFilters;
  applied: CatalogFilters;
  onFilters: (next: CatalogFilters) => void;
  onApply: () => void;
  onClear: () => void;
  seasons: readonly string[];
  genres: Genre[];
  genresError: string;
  tags: Tag[];
  tagsError: string;
  onRetryFacets: () => void;
  items: Anime[];
  loading: boolean;
  loadingMore: boolean;
  error: string;
  stale: boolean;
  hasMore: boolean;
  onOpen: (anime: Anime) => void;
  onLoadMore: () => void;
  onRetry: () => void;
}

