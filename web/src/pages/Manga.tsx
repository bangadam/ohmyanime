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
import type { MangaPageProps } from "../types";
import "../styles/pages-browse.css";
import "../styles/manga.css";

export default function Manga(props: MangaPageProps) {
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
    hasMore,
    onOpen,
    onLoadMore,
    provider,
    onProvider,
    providerOptions,
    onRetry,
  } = props;
  const trimmedQuery = query.trim();
  const hasQuery = trimmedQuery.length > 0;
  const hasResults = results.length > 0;

  return (
    <div className="container page search-page manga-page">
      <PageHeader title={copy.manga.title} />
      <form
        className="search-form"
        role="search"
        aria-label={copy.manga.label}
        onSubmit={(event) => {
          event.preventDefault();
          onSubmit();
        }}
      >
        <div className="search-field">
          <TextField
            id="manga-query"
            label={copy.manga.label}
            placeholder={copy.manga.placeholder}
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
                  label={copy.manga.clear}
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
          {copy.manga.submit}
        </Button>
      </form>

      <div className="search-body" aria-busy={loading || loadingMore}>
        {loading && !hasResults ? (
          <SkeletonGrid count={6} label={copy.manga.loading} />
        ) : error && !hasResults ? (
          <ErrorState
            title={copy.manga.errorTitle}
            description={error}
            retry={onRetry}
            retryLabel={copy.manga.detail.retry}
          />
        ) : !hasResults && hasQuery ? (
          <EmptyState
            title={format(copy.manga.noResultsFor, { query: trimmedQuery })}
            description={copy.manga.recovery}
            action={
              <Button variant="secondary" onClick={onClear}>
                {copy.manga.clear}
              </Button>
            }
          />
        ) : hasResults ? (
          <section className="search-results manga-results">
            <SectionHeader title={hasQuery ? copy.manga.results : copy.manga.popular} />
            {providerOptions.length > 1 ? (
              <FilterChips<string>
                label={copy.manga.providerFilterLabel}
                value={provider}
                options={providerOptions}
                onChange={onProvider}
              />
            ) : null}
            {error ? (
              <Notice
                tone="error"
                message={error}
                action={
                  <Button variant="quiet" onClick={onRetry}>
                    {copy.manga.detail.retry}
                  </Button>
                }
              />
            ) : null}
            <div className="poster-grid">
              {results.map((manga) => (
                <PosterCard key={manga.id} anime={manga} onOpen={() => onOpen(manga)} />
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
          <EmptyState title={copy.manga.popular} description={copy.manga.recovery} />
        )}
      </div>
    </div>
  );
}
