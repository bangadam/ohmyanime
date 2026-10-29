import {
  Button,
  EmptyState,
  ErrorState,
  Notice,
  PageHeader,
  PosterCard,
  SelectField,
  SkeletonGrid,
} from "../components";
import { copy, format, plural } from "../copy";
import type { SeasonPageProps } from "../types";
import "../styles/pages-browse.css";

/** Year filter domain mirrors the catalogue range the product already supported. */
const FIRST_YEAR = 1940;

function seasonLabel(value: string): string {
  return value ? value.charAt(0).toUpperCase() + value.slice(1) : value;
}

function buildSeasonOptions(seasons: readonly string[], selected: string): { value: string; label: string }[] {
  const values = seasons.length > 0 ? [...seasons] : [selected];
  if (!values.includes(selected)) values.unshift(selected);
  return values.map((value) => ({ value, label: seasonLabel(value) }));
}

function buildYearOptions(selected: number): { value: string; label: string }[] {
  const newest = new Date().getFullYear() + 1;
  const options: { value: string; label: string }[] = [];
  for (let year = newest; year >= FIRST_YEAR; year -= 1) {
    options.push({ value: String(year), label: String(year) });
  }
  if (!options.some((option) => option.value === String(selected))) {
    options.unshift({ value: String(selected), label: String(selected) });
  }
  return options;
}

export default function Season(props: SeasonPageProps) {
  const {
    season,
    year,
    genre,
    seasons,
    genres,
    onSeason,
    onYear,
    onGenre,
    items,
    loading,
    loadingMore,
    error,
    hasMore,
    onOpen,
    onLoadMore,
    onRetry,
  } = props;
  const count = items.length;
  const loadingFirst = loading && count === 0;
  const showCount = !loading && !error && !hasMore && count > 0;
  const genreOptions = [
    { value: "", label: copy.catalog.allGenres },
    ...genres.map((item) => ({ value: item.id, label: item.name })),
  ];

  return (
    <div className="container page season-page">
      <PageHeader title={copy.catalog.seasonTitle} description={copy.catalog.seasonDescription} />

      <div className="season-filters" role="group" aria-label={copy.a11y.filterGroup}>
        <SelectField
          id="season-filter"
          label={copy.catalog.season}
          value={season}
          options={buildSeasonOptions(seasons, season)}
          onChange={(event) => onSeason(event.target.value)}
        />
        <SelectField
          id="season-year"
          label={copy.catalog.year}
          value={String(year)}
          options={buildYearOptions(year)}
          onChange={(event) => onYear(Number(event.target.value))}
        />
        <SelectField
          id="season-genre"
          label={copy.catalog.genre}
          value={genre}
          options={genreOptions}
          onChange={(event) => onGenre(event.target.value)}
        />
      </div>

      <div className="season-body" aria-busy={loading || loadingMore}>
        {loadingFirst ? (
          <SkeletonGrid count={12} label={copy.states.loading} />
        ) : error && count === 0 ? (
          <ErrorState
            title={copy.catalog.errorTitle}
            description={error}
            retry={onRetry}
            retryLabel={copy.states.retry}
          />
        ) : count === 0 ? (
          <EmptyState title={copy.catalog.emptyTitle} description={copy.catalog.emptyBody} />
        ) : (
          <>
            {showCount ? (
              <p className="season-count" role="status">
                {format(plural(count, copy.catalog.resultCountOne, copy.catalog.resultCount), { count })}
              </p>
            ) : null}
            {error ? (
              <Notice
                tone="error"
                message={error}
                action={
                  <Button variant="quiet" onClick={onRetry}>
                    {copy.states.retry}
                  </Button>
                }
              />
            ) : null}
            <div className="poster-grid">
              {items.map((anime) => (
                <PosterCard key={anime.id} anime={anime} onOpen={() => onOpen(anime)} />
              ))}
            </div>
            {hasMore ? (
              <div className="season-more">
                <Button variant="secondary" onClick={onLoadMore} busy={loadingMore} disabled={loadingMore}>
                  {copy.catalog.loadMore}
                </Button>
                {loadingMore ? (
                  <span className="visually-hidden" role="status">
                    {copy.states.loadingMore}
                  </span>
                ) : null}
              </div>
            ) : null}
          </>
        )}
      </div>
    </div>
  );
}
