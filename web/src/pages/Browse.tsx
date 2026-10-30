import { useRef, useState, type KeyboardEvent } from "react";
import {
  Button,
  EmptyState,
  ErrorState,
  FilterChips,
  Notice,
  PageHeader,
  PosterCard,
  SelectField,
  SkeletonGrid,
  TextField,
} from "../components";
import { copy, format, plural } from "../copy";
import { activeFilterCount } from "../filters-url";
import type {
  BrowsePageProps,
  CatalogFilters,
  CatalogSort,
  CatalogStatus,
  CatalogType,
  Genre,
  Tag,
} from "../types";
import "../styles/pages-browse.css";

/** Year filter domain mirrors the catalogue range the product already supported. */
const FIRST_YEAR = 1940;
/** Facets stay scannable until the user asks for the whole vocabulary. */
const GENRE_PAGE = 12;
const TAG_CATEGORY_PAGE = 2;

const STATUS_OPTIONS = Object.entries(copy.browse.statusOptions).map(([value, label]) => ({
  value: value as CatalogStatus,
  label,
}));
const TYPE_OPTIONS = Object.entries(copy.browse.typeOptions).map(([value, label]) => ({
  value: value as CatalogType,
  label,
}));
const SORT_OPTIONS = Object.entries(copy.browse.sortOptions).map(([value, label]) => ({
  value: value as CatalogSort,
  label,
}));

function buildSeasonOptions(seasons: readonly string[], selected: string): { value: string; label: string }[] {
  const values = seasons.length > 0 ? [...seasons] : selected ? [selected] : [];
  if (selected && !values.includes(selected)) values.unshift(selected);
  return [
    { value: "", label: copy.browse.allSeasons },
    ...values.map((value) => ({
      value,
      label: value.charAt(0).toUpperCase() + value.slice(1),
    })),
  ];
}

function buildYearOptions(selected: number): { value: string; label: string }[] {
  const newest = new Date().getFullYear() + 1;
  const options: { value: string; label: string }[] = [{ value: "0", label: copy.browse.allYears }];
  for (let year = newest; year >= FIRST_YEAR; year -= 1) {
    options.push({ value: String(year), label: String(year) });
  }
  // A year outside the generated range (a restored or hand-edited URL) still
  // needs an option, otherwise the select would silently snap to "Any year".
  if (selected > 0 && !options.some((option) => option.value === String(selected))) {
    options.splice(1, 0, { value: String(selected), label: String(selected) });
  }
  return options;
}

type SummaryChip = { key: string; label: string; next: CatalogFilters };

/** One removable chip per active filter, each carrying the reduced filter set. */
function summarize(applied: CatalogFilters): SummaryChip[] {
  const chips: SummaryChip[] = [];
  const query = applied.query.trim();
  if (query) chips.push({ key: "query", label: query, next: { ...applied, query: "" } });
  if (applied.status !== "all") {
    chips.push({
      key: "status",
      label: copy.browse.statusOptions[applied.status],
      next: { ...applied, status: "all" },
    });
  }
  if (applied.type !== "all") {
    chips.push({
      key: "type",
      label: copy.browse.typeOptions[applied.type],
      next: { ...applied, type: "all" },
    });
  }
  const season = applied.season.trim();
  if (season) {
    chips.push({
      key: "season",
      label: season.charAt(0).toUpperCase() + season.slice(1),
      next: { ...applied, season: "" },
    });
  }
  if (applied.year > 0) {
    chips.push({ key: "year", label: String(applied.year), next: { ...applied, year: 0 } });
  }
  for (const genre of applied.genres) {
    chips.push({
      key: `genre:${genre}`,
      label: genre,
      next: { ...applied, genres: applied.genres.filter((item) => item !== genre) },
    });
  }
  for (const tag of applied.tags) {
    chips.push({
      key: `tag:${tag}`,
      label: tag,
      next: { ...applied, tags: applied.tags.filter((item) => item !== tag) },
    });
  }
  return chips;
}

/**
 * Genre picker as a combobox: type to narrow the vocabulary, arrow keys move
 * the active option, Enter toggles it, Escape closes. Selected genres stay
 * visible as removable chips, even when the vocabulary failed to load, so a
 * restored URL never strands a filter the user cannot clear.
 */
function GenreCombobox({
  genres,
  error,
  selected,
  onToggle,
  onRetry,
}: {
  genres: Genre[];
  error: string;
  selected: readonly string[];
  onToggle: (next: string[]) => void;
  onRetry: () => void;
}) {
  const [search, setSearch] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  if (genres.length === 0 && !error && selected.length === 0) return null;

  const needle = search.trim().toLowerCase();
  const matching = needle === "" ? genres : genres.filter((item) => item.name.toLowerCase().includes(needle));
  const visible = matching.slice(0, GENRE_PAGE);

  function toggle(name: string) {
    onToggle(
      selected.includes(name)
        ? selected.filter((item) => item !== name)
        : [...selected, name],
    );
  }

  function handleKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setOpen(true);
      setActive((current) => Math.min(current + 1, visible.length - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setOpen(true);
      setActive((current) => Math.max(current - 1, 0));
    } else if (event.key === "Enter") {
      if (open && active >= 0 && active < visible.length) {
        // Picking an option must not also submit the surrounding filter form.
        event.preventDefault();
        toggle(visible[active].id);
      }
    } else if (event.key === "Escape") {
      setOpen(false);
      setActive(-1);
    }
  }

  return (
    <fieldset className="browse-group browse-combobox">
      <legend className="field__label">{copy.browse.genresLabel}</legend>
      {selected.length > 0 ? (
        <div className="chips" role="group" aria-label={copy.browse.genreSelected}>
          {selected.map((name) => (
            <button
              key={name}
              type="button"
              className="chips__chip"
              aria-label={format(copy.browse.removeGenre, { genre: name })}
              onClick={() => toggle(name)}
            >
              <span aria-hidden="true">×</span> {name}
            </button>
          ))}
        </div>
      ) : null}
      {error ? (
        <div className="browse-facets">
          <p className="field__error" role="alert">
            {copy.browse.genresUnavailable}
          </p>
          <Button variant="quiet" onClick={onRetry}>
            {copy.browse.retryFacets}
          </Button>
        </div>
      ) : (
        <>
          <div
            className="browse-combobox__control"
            onBlur={(event) => {
              if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setOpen(false);
            }}
          >
            <input
              id="browse-genre"
              className="field__input"
              type="text"
              role="combobox"
              aria-label={copy.browse.genreSearchLabel}
              aria-autocomplete="list"
              aria-expanded={open}
              aria-controls="browse-genre-listbox"
              aria-activedescendant={open && active >= 0 ? `browse-genre-option-${active}` : undefined}
              placeholder={copy.browse.genreSearchPlaceholder}
              value={search}
              onChange={(event) => {
                setSearch(event.target.value);
                setOpen(true);
                setActive(-1);
              }}
              onFocus={() => setOpen(true)}
              onKeyDown={handleKeyDown}
            />
            {open ? (
              <div className="browse-combobox__list" id="browse-genre-listbox" role="listbox">
                {visible.map((genre, index) => (
                  <div
                    key={genre.id}
                    id={`browse-genre-option-${index}`}
                    role="option"
                    tabIndex={-1}
                    aria-selected={selected.includes(genre.id)}
                    className={
                      index === active ? "browse-combobox__option is-active" : "browse-combobox__option"
                    }
                    // Keep focus in the input so a click never closes the list
                    // before the click handler runs.
                    onMouseDown={(event) => event.preventDefault()}
                    onMouseEnter={() => setActive(index)}
                    onClick={() => toggle(genre.id)}
                  >
                    {genre.name}
                  </div>
                ))}
              </div>
            ) : null}
          </div>
          {open && needle !== "" && visible.length === 0 ? (
            <p className="browse-combobox__empty" role="status">
              {format(copy.browse.genreNoMatch, { query: search.trim() })}
            </p>
          ) : null}
          <p className="field__hint">{copy.browse.genreHint}</p>
        </>
      )}
    </fieldset>
  );
}

/** Tag facets grouped by category so the long vocabulary stays navigable. */
function TagFacets({
  tags,
  error,
  selected,
  onToggle,
  onRetry,
}: {
  tags: Tag[];
  error: string;
  selected: readonly string[];
  onToggle: (next: string[]) => void;
  onRetry: () => void;
}) {
  const [search, setSearch] = useState("");
  const [expanded, setExpanded] = useState(false);
  if (tags.length === 0 && !error) return null;

  const needle = search.trim().toLowerCase();
  const groups = new Map<string, Tag[]>();
  for (const tag of tags) {
    if (
      needle !== "" &&
      !tag.name.toLowerCase().includes(needle) &&
      !tag.category.toLowerCase().includes(needle)
    ) {
      continue;
    }
    const bucket = groups.get(tag.category) ?? [];
    bucket.push(tag);
    groups.set(tag.category, bucket);
  }
  const entries = [...groups.entries()];
  const visible = expanded ? entries : entries.slice(0, TAG_CATEGORY_PAGE);

  return (
    <div className="browse-group">
      <h2 className="field__label" id="browse-tags-label">
        {copy.browse.tagsLabel}
      </h2>
      {error ? (
        <div className="browse-facets">
          <p className="field__error" role="alert">
            {copy.browse.tagsUnavailable}
          </p>
          <Button variant="quiet" onClick={onRetry}>
            {copy.browse.retryFacets}
          </Button>
        </div>
      ) : (
        <>
          <div className="browse-facet-search">
            <TextField
              id="browse-tag-search"
              type="search"
              label={copy.browse.tagSearchLabel}
              placeholder={copy.browse.tagSearchPlaceholder}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
          </div>
          {visible.map(([category, list]) => (
            <fieldset key={category} className="browse-facets">
              <legend className="field__label">{category}</legend>
              <div className="chips">
                {list.map((tag) => (
                  <button
                    key={tag.id}
                    type="button"
                    className="chips__chip"
                    aria-pressed={selected.includes(tag.id)}
                    onClick={() =>
                      onToggle(
                        selected.includes(tag.id)
                          ? selected.filter((item) => item !== tag.id)
                          : [...selected, tag.id],
                      )
                    }
                  >
                    {tag.name}
                  </button>
                ))}
              </div>
            </fieldset>
          ))}
          {entries.length > TAG_CATEGORY_PAGE ? (
            <button
              type="button"
              className="browse-facet-toggle"
              aria-expanded={expanded}
              onClick={() => setExpanded((current) => !current)}
            >
              {expanded ? copy.browse.showFewerTags : copy.browse.showAllTags}
            </button>
          ) : null}
        </>
      )}
    </div>
  );
}

export default function Browse(props: BrowsePageProps) {
  const {
    filters,
    applied,
    onFilters,
    onApply,
    onClear,
    seasons,
    genres,
    genresError,
    tags,
    tagsError,
    onRetryFacets,
    items,
    loading,
    loadingMore,
    error,
    stale,
    hasMore,
    onOpen,
    onLoadMore,
    onRetry,
  } = props;

  const summaryRef = useRef<HTMLElement | null>(null);
  const count = items.length;
  const loadingFirst = loading && count === 0;
  const activeCount = activeFilterCount(applied);
  const chips = summarize(applied);

  /**
   * Applying moves focus to the applied-filter summary: the results reload
   * asynchronously, so keyboard users need a stable place to continue from.
   */
  function applyAndFocus() {
    onApply();
    summaryRef.current?.focus();
  }

  /** Applying immediately is one user action: reduce the draft, then apply it. */
  function removeFilter(next: CatalogFilters) {
    onFilters(next);
    applyAndFocus();
  }

  return (
    <div className="container page">
      <PageHeader title={copy.browse.title} description={copy.browse.description} />

      <form
        className="browse-panel"
        aria-label={copy.a11y.filterGroup}
        onSubmit={(event) => {
          event.preventDefault();
          applyAndFocus();
        }}
      >
        <div className="browse-form">
          <TextField
            id="browse-query"
            type="search"
            label={copy.browse.titleLabel}
            placeholder={copy.browse.titlePlaceholder}
            value={filters.query}
            onChange={(event) => onFilters({ ...filters, query: event.target.value })}
          />

          <div className="browse-form__grid">
            <fieldset className="browse-group">
              <legend className="field__label">{copy.browse.statusLabel}</legend>
              <FilterChips<CatalogStatus>
                label={copy.browse.statusLabel}
                value={filters.status}
                options={STATUS_OPTIONS}
                onChange={(value) => onFilters({ ...filters, status: value })}
              />
            </fieldset>
            <fieldset className="browse-group">
              <legend className="field__label">{copy.browse.typeLabel}</legend>
              <FilterChips<CatalogType>
                label={copy.browse.typeLabel}
                value={filters.type}
                options={TYPE_OPTIONS}
                onChange={(value) => onFilters({ ...filters, type: value })}
              />
            </fieldset>
            <fieldset className="browse-group">
              <legend className="field__label">{copy.browse.sortLabel}</legend>
              <FilterChips<CatalogSort>
                label={copy.browse.sortLabel}
                value={filters.sort}
                options={SORT_OPTIONS}
                onChange={(value) => onFilters({ ...filters, sort: value })}
              />
            </fieldset>
          </div>

          <div className="browse-form__grid browse-form__grid--pair">
            <SelectField
              id="browse-season"
              label={copy.browse.seasonLabel}
              value={filters.season}
              options={buildSeasonOptions(seasons, filters.season)}
              onChange={(event) => onFilters({ ...filters, season: event.target.value })}
            />
            <SelectField
              id="browse-year"
              label={copy.browse.yearLabel}
              value={String(filters.year)}
              options={buildYearOptions(filters.year)}
              onChange={(event) => onFilters({ ...filters, year: Number(event.target.value) })}
            />
          </div>

          <GenreCombobox
            genres={genres}
            error={genresError}
            selected={filters.genres}
            onToggle={(next) => onFilters({ ...filters, genres: next })}
            onRetry={onRetryFacets}
          />

          <TagFacets
            tags={tags}
            error={tagsError}
            selected={filters.tags}
            onToggle={(next) => onFilters({ ...filters, tags: next })}
            onRetry={onRetryFacets}
          />

          <div className="browse-actions">
            <Button type="submit" variant="primary" busy={loading} disabled={loading}>
              {loading ? copy.browse.applying : copy.browse.apply}
            </Button>
            <Button variant="quiet" onClick={onClear}>
              {copy.browse.clearAll}
            </Button>
          </div>
        </div>
      </form>

      <section
        className="browse-summary"
        aria-label={copy.browse.activeFilters}
        ref={summaryRef}
        tabIndex={-1}
      >
        {activeCount === 0 ? (
          <p className="browse-summary__empty">{copy.browse.noFilters}</p>
        ) : (
          <>
            <p className="browse-summary__title">
              {copy.browse.activeFilters} ({activeCount})
            </p>
            <div className="chips browse-summary__chips">
              {chips.map((chip) => (
                <button
                  key={chip.key}
                  type="button"
                  className="chips__chip"
                  aria-label={format(copy.browse.removeFilter, { filter: chip.label })}
                  onClick={() => removeFilter(chip.next)}
                >
                  <span aria-hidden="true">×</span> {chip.label}
                </button>
              ))}
            </div>
          </>
        )}
      </section>

      <div className="browse-body" aria-busy={loading || loadingMore}>
        {loadingFirst ? (
          <SkeletonGrid count={12} label={copy.browse.loading} />
        ) : error && count === 0 ? (
          <ErrorState
            title={copy.browse.errorTitle}
            description={error}
            retry={onRetry}
            retryLabel={copy.states.retry}
          />
        ) : count === 0 ? (
          <EmptyState
            title={copy.browse.emptyTitle}
            description={copy.browse.emptyBody}
            action={
              <Button variant="secondary" onClick={onClear}>
                {copy.browse.clearAll}
              </Button>
            }
          />
        ) : (
          <>
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
            <p className="browse-count" role="status">
              {format(plural(count, copy.browse.resultCountOne, copy.browse.resultCount), { count })}
            </p>
            <div className="poster-grid">
              {items.map((anime) => (
                <PosterCard key={anime.id} anime={anime} onOpen={() => onOpen(anime)} />
              ))}
            </div>
            {hasMore ? (
              <div className="browse-more">
                <Button variant="secondary" onClick={onLoadMore} busy={loadingMore} disabled={loadingMore}>
                  {copy.browse.loadMore}
                </Button>
                {loadingMore ? (
                  <span className="visually-hidden" role="status">
                    {copy.browse.loadingMore}
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
