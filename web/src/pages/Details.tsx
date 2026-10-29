import { memo, useMemo, useState } from "react";
import { ArrowLeft, Bookmark, Check, Clapperboard, Play } from "lucide-react";
import { mediaURL } from "../api";
import type { Episode } from "../api";
import type { DetailPageProps } from "../types";
import { copy, format } from "../copy";
import {
  animeStatusLabel,
  animeTypeLabel,
  cleanEpisodeTitle,
  cleanSynopsis,
  episodeCountLabel,
  metaLine,
  seasonLabel,
} from "../utils";
import { Badge, Button, EmptyState, ErrorState, SectionHeader, TagChips } from "../components";
import "../styles/pages-library.css";

function Cover({ src, title }: { src?: string; title: string }) {
  const [failed, setFailed] = useState(false);
  if (!src || failed) {
    return (
      <div className="detail-cover detail-cover--empty" aria-hidden="true">
        <Clapperboard size={44} strokeWidth={1.2} aria-hidden="true" />
      </div>
    );
  }
  return (
    <img
      className="detail-cover"
      src={src}
      alt={format(copy.a11y.posterAlt, { title })}
      decoding="async"
      fetchPriority="high"
      referrerPolicy="no-referrer"
      onError={() => setFailed(true)}
    />
  );
}

// Long inventories stay fully rendered. Measured with 1000/1500/3000-row
// fixtures (Details.perf.test.tsx): mount cost grows linearly (~470ms per
// 1000 rows under happy-dom, faster in Chromium) and is paid once, so DOM
// windowing would trade keyboard reachability for a mount win we don't need.
// Rows are memoized instead: toggling watched flags or typing in the episode
// search no longer re-renders unchanged rows, and every row keeps a stable
// key so focus survives re-renders.
const EpisodeRow = memo(function EpisodeRow({
  item,
  watched,
  watchedBusy,
  onPlay,
  onToggleWatched,
}: {
  item: Episode;
  watched: boolean;
  watchedBusy: boolean;
  onPlay: (episode: Episode) => void;
  onToggleWatched: (episodeId: string, watched: boolean) => void;
}) {
  const available = (item.availability?.length ?? 0) > 0;
  const displayTitle = cleanEpisodeTitle(item.title, item.label, item.ordinal);
  // The number column always carries the label, so a row without a known
  // title shows the number once instead of repeating it as "Episode 6".
  const rowLabel = displayTitle
    ? `${format(copy.details.playEpisode, { label: item.label })}: ${displayTitle}`
    : format(copy.details.playEpisode, { label: item.label });
  const accessibleLabel = available
    ? rowLabel
    : `${rowLabel}, ${copy.details.episodeUnavailable}`;
  return (
    <li>
      <div className="detail-episode-row">
        <button
          type="button"
          className="detail-episode"
          aria-label={accessibleLabel}
          disabled={!available}
          onClick={() => onPlay(item)}
        >
          <span className="detail-episode__number tabular" aria-hidden="true">
            {item.label}
          </span>
          <span className="detail-episode__body" aria-hidden="true">
            {displayTitle ? <span className="detail-episode__title">{displayTitle}</span> : null}
            {available ? null : <Badge>{copy.details.episodeUnavailable}</Badge>}
          </span>
          <span className="detail-episode__play" aria-hidden="true">
            <Play className="detail-episode__icon" size={18} strokeWidth={1.8} />
            <span>{copy.details.play}</span>
          </span>
        </button>
        <button
          type="button"
          className="detail-episode__watched"
          aria-pressed={watched}
          aria-label={`${item.label}: ${watched ? copy.details.markUnwatched : copy.details.markWatched}`}
          title={watched ? copy.details.markUnwatched : copy.details.markWatched}
          disabled={watchedBusy}
          onClick={() => onToggleWatched(item.id, !watched)}
        >
          <Check
            size={16}
            aria-hidden="true"
            strokeWidth={2.2}
            className={watched ? "detail-episode__watched-icon is-on" : "detail-episode__watched-icon"}
          />
        </button>
      </div>
    </li>
  );
});

export default function Details({
  anime,
  episodes,
  state,
  error,
  bookmarked,
  busy,
  watched,
  watchedBusy,
  onBack,
  onPlay,
  onToggleBookmark,
  onToggleWatched,
  onBulkWatched,
  onFilterByGenre,
  onFilterByTag,
  onRetry,
}: DetailPageProps) {
  const loading = state === "loading" || state === "idle";
  const ready = state === "ready";
  const failed = state === "error";

  const orderedEpisodes = useMemo(() => [...episodes].sort((a, b) => a.ordinal - b.ordinal), [episodes]);
  const firstPlayable = useMemo(
    () => orderedEpisodes.find((episode) => (episode.availability?.length ?? 0) > 0) ?? null,
    [orderedEpisodes],
  );
  const [episodeQuery, setEpisodeQuery] = useState("");
  const visibleEpisodes = useMemo(() => {
    const query = episodeQuery.trim().toLowerCase();
    if (!query) return orderedEpisodes;
    return orderedEpisodes.filter(
      (episode) =>
        episode.label.toLowerCase().includes(query) ||
        (episode.title ?? "").toLowerCase().includes(query),
    );
  }, [orderedEpisodes, episodeQuery]);

  const isMovie = (anime.type ?? "").toLowerCase() === "movie";
  const synopsis = cleanSynopsis(anime.synopsis);
  const facts = metaLine([
    animeTypeLabel(anime.type),
    seasonLabel(anime.season),
    anime.year,
    animeStatusLabel(anime.status),
    // A movie is a single work; an "episode count" would describe a series.
    !isMovie && ready && orderedEpisodes.length > 0 ? episodeCountLabel(orderedEpisodes.length) : null,
  ]);
  // A movie normally has one playable entry, so a numbered episode list would
  // only restate the hero action. The list stays when a movie entry is split
  // into several parts.
  const showEpisodeList = !isMovie || orderedEpisodes.length > 1;
  const showEpisodesSection = showEpisodeList || loading || failed || (ready && !firstPlayable);
  const playLabel = isMovie
    ? copy.details.playMovie
    : firstPlayable
      ? format(copy.details.playEpisode, { label: firstPlayable.label })
      : copy.details.playFirst;
  const alternativeTitles = anime.alternativeTitles?.filter((title) => title.trim() !== "") ?? [];
  // Facets arrive with the anime payload; they are optional so older cached
  // entries simply render no taxonomy groups.
  const genres = anime.genres ?? [];
  const tags = anime.tags ?? [];

  return (
    <div className="page detail-page">
      <div className="container">
        <Button variant="quiet" className="detail-back" icon={<ArrowLeft size={18} aria-hidden="true" />} onClick={onBack}>
          {copy.details.back}
        </Button>

        <section className="detail-hero" aria-labelledby="detail-title">
          <div className="detail-hero__art">
            <Cover src={anime.posterUrl ? mediaURL(anime.posterUrl) : undefined} title={anime.title} />
          </div>
          <div className="detail-hero__copy">
            <h1 id="detail-title" className="detail-hero__title">
              {anime.title}
            </h1>
            {facts ? <p className="detail-hero__facts">{facts}</p> : null}
            {genres.length > 0 ? (
              <TagChips
                label={copy.details.genres}
                items={genres}
                onSelect={onFilterByGenre}
                labelFor={(genre) => format(copy.details.filterByGenre, { genre })}
              />
            ) : null}
            {tags.length > 0 ? (
              <TagChips
                label={copy.details.tags}
                items={tags}
                onSelect={onFilterByTag}
                labelFor={(tag) => format(copy.details.filterByTag, { tag })}
              />
            ) : null}
            {alternativeTitles.length > 0 ? (
              <p className="detail-hero__alt">
                <span className="detail-hero__alt-label">{copy.details.alternateTitles}</span>
                <span>{alternativeTitles.join(" / ")}</span>
              </p>
            ) : null}
            <p className="detail-hero__synopsis">{synopsis || copy.details.noSynopsis}</p>
            <div className="detail-hero__actions">
              <Button
                variant="primary"
                icon={<Play size={18} aria-hidden="true" />}
                disabled={!firstPlayable}
                onClick={() => {
                  if (firstPlayable) onPlay(firstPlayable);
                }}
              >
                {playLabel}
              </Button>
              <Button
                variant="secondary"
                icon={
                  <Bookmark
                    size={18}
                    aria-hidden="true"
                    fill={bookmarked ? "currentColor" : "none"}
                  />
                }
                aria-pressed={bookmarked}
                busy={busy}
                onClick={() => {
                  if (!busy) onToggleBookmark();
                }}
              >
                {bookmarked ? copy.list.saved : copy.list.add}
              </Button>
            </div>
          </div>
        </section>

        {showEpisodesSection ? (
          <section className="detail-episodes" aria-labelledby="detail-episodes-heading" aria-busy={loading}>
          <SectionHeader
            id="detail-episodes-heading"
            title={showEpisodeList ? copy.details.episodes : copy.details.movie}
            count={showEpisodeList && ready && orderedEpisodes.length > 0 ? orderedEpisodes.length : undefined}
            action={
              showEpisodeList && ready && orderedEpisodes.length > 0 ? (
                <div className="detail-episodes__bulk">
                  <Button variant="quiet" disabled={watchedBusy} onClick={() => onBulkWatched(true)}>
                    {copy.details.markAllWatched}
                  </Button>
                  <Button variant="quiet" disabled={watchedBusy} onClick={() => onBulkWatched(false)}>
                    {copy.details.markAllUnwatched}
                  </Button>
                </div>
              ) : undefined
            }
          />

          {loading ? (
            <>
              <p className="visually-hidden" role="status">
                {copy.details.loadingEpisodes}
              </p>
              <div className="detail-skeleton" aria-hidden="true">
                {Array.from({ length: showEpisodeList ? 6 : 1 }, (_, index) => (
                  <div className="detail-skeleton__row" key={index}>
                    <span className="detail-skeleton__number" />
                    <span className="detail-skeleton__title" />
                  </div>
                ))}
              </div>
            </>
          ) : failed ? (
            <ErrorState
              title={copy.details.episodeError}
              description={error || undefined}
              retry={onRetry}
              retryLabel={copy.details.retry}
            />
          ) : orderedEpisodes.length === 0 ? (
            <EmptyState title={copy.details.unavailable} description={copy.details.unavailableBody} />
          ) : (
            <>
              {showEpisodeList && orderedEpisodes.length > 8 ? (
                <div className="detail-episode-search">
                  <label className="field">
                    <span className="field__label">{copy.details.episodeSearchLabel}</span>
                    <input
                      type="search"
                      className="field__input"
                      value={episodeQuery}
                      placeholder={copy.details.episodeSearchPlaceholder}
                      autoComplete="off"
                      spellCheck={false}
                      onChange={(event) => setEpisodeQuery(event.target.value)}
                    />
                  </label>
                  {episodeQuery && visibleEpisodes.length === 0 ? (
                    <p className="detail-episode-search__empty" role="status">
                      {copy.details.episodeSearchEmptyBody}
                    </p>
                  ) : null}
                </div>
              ) : null}
              <ul className="detail-episode-list">
                {visibleEpisodes.map((item) => (
                  <EpisodeRow
                    key={item.id}
                    item={item}
                    watched={watched[item.id] ?? false}
                    watchedBusy={watchedBusy}
                    onPlay={onPlay}
                    onToggleWatched={onToggleWatched}
                  />
                ))}
              </ul>
            </>
          )}
          </section>
        ) : null}
      </div>
    </div>
  );
}
