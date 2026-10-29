import { Search as SearchIcon, X } from "lucide-react";
import {
  Button,
  EmptyState,
  ErrorState,
  IconButton,
  Notice,
  PageHeader,
  PosterCard,
  SectionHeader,
  SkeletonGrid,
  TextField,
} from "../components";
import { copy, format } from "../copy";
import type { SearchPageProps } from "../types";
import "../styles/pages-browse.css";

export default function Search(props: SearchPageProps) {
  const {
    input,
    query,
    onInput,
    onSubmit,
    onClear,
    results,
    loading,
    loadingMore,
    error,
    searched,
    hasMore,
    onOpen,
    onLoadMore,
    onRetry,
  } = props;
  const trimmedQuery = query.trim();
  const hasQuery = trimmedQuery.length > 0;
  const hasResults = results.length > 0;

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
        ) : loading && !hasResults ? (
          <SkeletonGrid count={6} label={copy.search.loading} />
        ) : error && !hasResults ? (
          <ErrorState
            title={copy.search.errorTitle}
            description={error}
            retry={onRetry}
            retryLabel={copy.states.retry}
          />
        ) : searched && !hasResults ? (
          <EmptyState
            title={format(copy.search.noResultsFor, { query: trimmedQuery })}
            description={copy.search.recovery}
            action={
              <Button variant="secondary" onClick={onClear}>
                {copy.search.clear}
              </Button>
            }
          />
        ) : hasResults ? (
          <section className="search-results">
            <SectionHeader title={copy.search.results} />
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
              {results.map((anime) => (
                <PosterCard key={anime.id} anime={anime} onOpen={() => onOpen(anime)} />
              ))}
            </div>
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
          </section>
        ) : (
          <EmptyState title={copy.search.emptyQueryTitle} description={copy.search.emptyQueryBody} />
        )}
      </div>
    </div>
  );
}
