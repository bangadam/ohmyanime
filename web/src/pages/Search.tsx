import { Search as SearchIcon, X } from "lucide-react";
import {
  Button,
  EmptyState,
  ErrorState,
  FilterChips,
  IconButton,
  Notice,
  PageHeader,
  PosterCard,
  SectionHeader,
  SkeletonGrid,
  TextField,
} from "../components";
import { copy, format } from "../copy";
import type { SearchKind, SearchPageProps } from "../types";
import "../styles/pages-browse.css";

const KINDS: { value: SearchKind; label: string }[] = [
  { value: "all", label: copy.search.kindAll },
  { value: "anime", label: copy.search.kindAnime },
  { value: "manga", label: copy.search.kindManga },
];

export default function Search(props: SearchPageProps) {
  const {
    input,
    query,
    onInput,
    onSubmit,
    onClear,
    results,
    mangaResults,
    kind,
    onKind,
    mangaProvider,
    onMangaProvider,
    mangaProviderOptions,
    loading,
    loadingMore,
    error,
    stale,
    searched,
    hasMore,
    onOpen,
    onLoadMore,
    onRetry,
  } = props;
  const trimmedQuery = query.trim();
  const hasQuery = trimmedQuery.length > 0;
  // The kind filter only switches which datasets are visible; anime and manga
  // ids are namespaced, so same-title results from both sources stay separate
  // cards with separate identities.
  const animeResults = kind === "manga" ? [] : results;
  const mangaShown = kind === "anime" ? [] : mangaResults;
  const visibleCount = animeResults.length + mangaShown.length;

  return (
    <div className="container page search-page">
      <PageHeader title={copy.search.title} />
      <form
        className="search-form"
        role="search"
        aria-label={copy.search.label}
        onSubmit={(event) => {
          event.preventDefault();
          onSubmit();
        }}
      >
        <div className="search-field">
          <TextField
            id="search-query"
            label={copy.search.label}
            placeholder={copy.search.placeholder}
            type="search"
            value={input}
            onChange={(event) => onInput(event.target.value)}
            autoComplete="off"
            spellCheck={false}
            enterKeyHint="search"
            trailing={
              input ? (
                <IconButton
                  type="button"
                  label={copy.search.clear}
                  icon={<X aria-hidden="true" size={18} />}
                  onClick={onClear}
                />
              ) : (
                <SearchIcon className="search-field-icon" aria-hidden="true" size={18} />
              )
            }
          />
        </div>
        <Button variant="primary" type="submit" busy={loading} disabled={loading}>
          {copy.search.submit}
        </Button>
      </form>

      <div className="search-body" aria-busy={loading || loadingMore}>
        {!hasQuery ? (
          <EmptyState title={copy.search.emptyQueryTitle} description={copy.search.emptyQueryBody} />
        ) : loading && visibleCount === 0 ? (
          <SkeletonGrid count={6} label={copy.search.loading} />
        ) : error && visibleCount === 0 ? (
          <ErrorState
            title={copy.search.errorTitle}
            description={error}
            retry={onRetry}
            retryLabel={copy.states.retry}
          />
        ) : searched && visibleCount === 0 ? (
          <EmptyState
            title={format(copy.search.noResultsFor, { query: trimmedQuery })}
            description={copy.search.recovery}
            action={
              <Button variant="secondary" onClick={onClear}>
                {copy.search.clear}
              </Button>
            }
          />
        ) : visibleCount > 0 ? (
          <div className="search-results">
            <FilterChips<SearchKind>
              label={copy.search.kindFilterLabel}
              value={kind}
              options={KINDS}
              onChange={onKind}
            />
            {/*
              The provider filter only affects manga, so it stays next to the
              kind filter and preserves the aggregated view by default.
            */}
            {kind !== "anime" && mangaProviderOptions.length > 1 ? (
              <FilterChips<string>
                label={copy.manga.providerFilterLabel}
                value={mangaProvider}
                options={mangaProviderOptions}
                onChange={onMangaProvider}
              />
            ) : null}
            {/*
              Partial failure: a failed source surfaces as a notice while the
              successful kind stays visible below.
            */}
            {error ? (
              <Notice
                tone="error"
                message={copy.search.partialError}
                action={
                  <Button variant="quiet" onClick={onRetry}>
                    {copy.states.retry}
                  </Button>
                }
              />
            ) : null}
            {stale ? (
              <Notice
                tone="status"
                message={copy.states.staleCatalogue}
                action={
                  <Button variant="quiet" onClick={onRetry}>
                    {copy.states.retry}
                  </Button>
                }
              />
            ) : null}
            {animeResults.length > 0 ? (
              <section className="search-results">
                <SectionHeader title={copy.search.animeResults} />
                <div className="poster-grid">
                  {animeResults.map((anime) => (
                    <PosterCard key={anime.id} anime={anime} kind="anime" onOpen={() => onOpen(anime, "anime")} />
                  ))}
                </div>
              </section>
            ) : null}
            {mangaShown.length > 0 ? (
              <section className="search-results">
                <SectionHeader title={copy.manga.results} />
                <div className="poster-grid">
                  {mangaShown.map((manga) => (
                    <PosterCard key={manga.id} anime={manga} kind="manga" onOpen={() => onOpen(manga, "manga")} />
                  ))}
                </div>
              </section>
            ) : null}
            {hasMore ? (
              <div className="search-more">
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
          </div>
        ) : (
          <EmptyState title={copy.search.emptyQueryTitle} description={copy.search.emptyQueryBody} />
        )}
      </div>
    </div>
  );
}
