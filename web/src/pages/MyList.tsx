import { useMemo, type ReactNode } from "react";
import { Trash2 } from "lucide-react";
import type { LibraryEntry, ListFilter, ListPageProps, ListSort } from "../types";
import { copy } from "../copy";
import { isMovie, isSeries, sortByTitle } from "../utils";
import {
  Button,
  EmptyState,
  ErrorState,
  FilterChips,
  IconButton,
  Notice,
  PageHeader,
  PosterCard,
  SkeletonGrid,
} from "../components";
import "../styles/pages-library.css";

const FILTERS: { value: ListFilter; label: string }[] = [
  { value: "anime", label: copy.list.anime },
  { value: "manga", label: copy.list.manga },
  { value: "series", label: copy.list.series },
  { value: "movies", label: copy.list.movies },
];

const SORTS: { value: ListSort; label: string }[] = [
  { value: "title-asc", label: copy.list.sortTitleAsc },
  { value: "title-desc", label: copy.list.sortTitleDesc },
];


export default function MyList({
  items,
  loading,
  loadingMore,
  error,
  hasMore,
  filter,
  sort,
  removingId,
  onFilter,
  onSort,
  onOpen,
  onLoadMore,
  onRetry,
  onRemove,
  onExplore,
}: ListPageProps) {
  const visible = useMemo(() => {
    // The kind filters switch datasets; series/movies refines anime entries.
    // Anime and manga ids are namespaced, so identical titles never merge.
    const filtered = items.filter((entry) => {
      if (filter === "all") return true;
      if (filter === "anime") return entry.kind === "anime";
      if (filter === "manga") return entry.kind === "manga";
      if (entry.kind !== "anime") return false;
      return filter === "series" ? isSeries(entry.item) : isMovie(entry.item);
    });
    return sortByTitle(filtered, sort === "title-desc" ? "desc" : "asc", (entry) => entry.item.title);
  }, [items, filter, sort]);

  const loadMoreButton = hasMore ? (
    <Button
      variant="secondary"
      busy={loadingMore}
      onClick={() => {
        if (!loadingMore) onLoadMore();
      }}
    >
      {copy.home.loadMore}
    </Button>
  ) : null;

  let body: ReactNode;
  if (error && items.length === 0) {
    body = (
      <ErrorState
        title={copy.list.errorTitle}
        description={error}
        retry={onRetry}
        retryLabel={copy.states.retry}
      />
    );
  } else if (loading && items.length === 0) {
    body = <SkeletonGrid count={6} label={copy.list.loading} />;
  } else if (items.length === 0) {
    body = (
      <EmptyState
        title={copy.list.emptyTitle}
        description={copy.list.emptyBody}
        action={
          <Button variant="primary" onClick={onExplore}>
            {copy.list.emptyAction}
          </Button>
        }
      />
    );
  } else if (error) {
    // Partial failure: the unreachable source surfaces as a notice while
    // saved titles from the other kind stay visible.
    body = (
      <>
        <Notice
          tone="error"
          message={copy.search.partialError}
          action={
            <Button variant="quiet" onClick={onRetry}>
              {copy.states.retry}
            </Button>
          }
        />
        <div className="poster-grid">
          {visible.map((entry) => (
            <PosterCard
              key={entry.item.id}
              anime={entry.item}
              kind={entry.kind}
              onOpen={() => onOpen(entry)}
              action={
                <IconButton
                  variant="raised"
                  label={`${copy.list.remove}: ${entry.item.title}`}
                  icon={<Trash2 size={18} aria-hidden="true" />}
                  aria-busy={removingId === entry.item.id ? true : undefined}
                  disabled={removingId === entry.item.id}
                  onClick={() => onRemove(entry)}
                />
              }
            />
          ))}
        </div>
        {loadMoreButton ? <div className="list-more">{loadMoreButton}</div> : null}
      </>
    );
  } else if (visible.length === 0) {
    body = (
      <EmptyState
        title={copy.list.noMatchesTitle}
        description={copy.list.noMatchesBody}
        action={loadMoreButton ?? undefined}
      />
    );
  } else {
    body = (
      <>
        <div className="poster-grid">
          {visible.map((entry) => (
            <PosterCard
              key={entry.item.id}
              anime={entry.item}
              kind={entry.kind}
              onOpen={() => onOpen(entry)}
              action={
                <IconButton
                  variant="raised"
                  label={`${copy.list.remove}: ${entry.item.title}`}
                  icon={<Trash2 size={18} aria-hidden="true" />}
                  aria-busy={removingId === entry.item.id ? true : undefined}
                  disabled={removingId === entry.item.id}
                  onClick={() => onRemove(entry)}
                />
              }
            />
          ))}
        </div>
        {loadMoreButton ? <div className="list-more">{loadMoreButton}</div> : null}
      </>
    );
  }

  return (
    <div className="page list-page">
      <div className="container">
        <PageHeader title={copy.list.title} description={copy.list.description} />

        <div className="list-toolbar">
          <FilterChips<ListFilter>
            label={copy.a11y.filterGroup}
            value={filter}
            options={FILTERS}
            onChange={onFilter}
          />
          <label className="list-sort" htmlFor="list-sort">
            <span className="list-sort__label">{copy.list.sort}</span>
            <select
              id="list-sort"
              className="list-sort__control"
              value={sort}
              onChange={(event) => onSort(event.target.value as ListSort)}
            >
              {SORTS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
        </div>

        {body}
      </div>
    </div>
  );
}
