import type { Anime, Episode, HistoryItem, Provider, Settings } from "./api";
import type { ThemeMode } from "./theme";

export type NavKey = "home" | "search" | "season" | "schedule" | "list" | "history" | "settings";
export type LoadState = "idle" | "loading" | "ready" | "error";
export type ListFilter = "all" | "series" | "movies";
export type ListSort = "title-asc" | "title-desc";
export type ScheduleEntry = { id: number; airingAt: number; episode: number; anime: Anime };

export interface HomePageProps {
  feature: Anime | null;
  catalogue: Anime[];
  history: HistoryItem[];
  loading: boolean;
  loadingMore: boolean;
  error: string;
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
  loading: boolean;
  loadingMore: boolean;
  error: string;
  searched: boolean;
  hasMore: boolean;
  onOpen: (anime: Anime) => void;
  onLoadMore: () => void;
  onRetry: () => void;
}

export interface SeasonPageProps {
  season: string;
  year: number;
  genre: string;
  seasons: readonly string[];
  genres: { id: string; name: string }[];
  onSeason: (value: string) => void;
  onYear: (value: number) => void;
  onGenre: (value: string) => void;
  items: Anime[];
  loading: boolean;
  loadingMore: boolean;
  error: string;
  hasMore: boolean;
  onOpen: (anime: Anime) => void;
  onLoadMore: () => void;
  onRetry: () => void;
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
  items: Anime[];
  loading: boolean;
  loadingMore: boolean;
  error: string;
  hasMore: boolean;
  filter: ListFilter;
  sort: ListSort;
  removingId: string | null;
  onFilter: (value: ListFilter) => void;
  onSort: (value: ListSort) => void;
  onOpen: (anime: Anime) => void;
  onLoadMore: () => void;
  onRetry: () => void;
  onRemove: (anime: Anime) => void;
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
}

export interface DetailPageProps {
  anime: Anime;
  episodes: Episode[];
  state: LoadState;
  error: string;
  bookmarked: boolean;
  busy: boolean;
  onBack: () => void;
  onPlay: (episode: Episode) => void;
  onToggleBookmark: () => void;
  onRetry: () => void;
}

