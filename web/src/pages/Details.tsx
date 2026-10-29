import { useMemo, useState } from "react";
import { ArrowLeft, Bookmark, Clapperboard, Play } from "lucide-react";
import { mediaURL } from "../api";
import type { DetailPageProps } from "../types";
import { copy, episodeLabel, format } from "../copy";
import {
  animeStatusLabel,
  animeTypeLabel,
  cleanEpisodeTitle,
  cleanSynopsis,
  episodeCountLabel,
  metaLine,
  seasonLabel,
} from "../utils";
import { Badge, Button, EmptyState, ErrorState, SectionHeader } from "../components";
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
      onError={() => setFailed(true)}
    />
  );
}

export default function Details({
  anime,
  episodes,
  state,
  error,
  bookmarked,
  busy,
  onBack,
  onPlay,
  onToggleBookmark,
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
            <ul className="detail-episode-list">
              {orderedEpisodes.map((item) => {
                const available = (item.availability?.length ?? 0) > 0;
                const displayTitle = cleanEpisodeTitle(item.title, item.label, item.ordinal);
                // The number column always carries the label, so a row without a
                // known title shows the number once instead of repeating it as
                // "Episode 6" in the title column.
                const rowLabel = displayTitle
                  ? `${format(copy.details.playEpisode, { label: item.label })}: ${displayTitle}`
                  : format(copy.details.playEpisode, { label: item.label });
                const accessibleLabel = available
                  ? rowLabel
                  : `${rowLabel}, ${copy.details.episodeUnavailable}`;
                return (
                  <li key={item.id}>
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
                  </li>
                );
              })}
            </ul>
          )}
          </section>
        ) : null}
      </div>
    </div>
  );
}
