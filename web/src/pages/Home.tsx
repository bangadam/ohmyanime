import { Film, History, Info, Play, X } from "lucide-react";
import { mediaURL, type Anime, type HistoryItem } from "../api";
import {
  Button,
  EmptyState,
  ErrorState,
  Notice,
  PosterCard,
  ProgressBar,
  SectionHeader,
  SkeletonGrid,
} from "../components";
import { copy, episodeLabel } from "../copy";
import type { HomePageProps } from "../types";
import { animeStatusLabel, animeTypeLabel, cleanSynopsis, formatHistoryDate, metaLine, seasonLabel } from "../utils";
import "../styles/pages-browse.css";

interface HomeHeroProps {
  anime: Anime;
  onOpen: (anime: Anime) => void;
  onViewSeason: () => void;
}

/**
 * Cinematic feature stage built only from a real catalogue title: the title's
 * own key art carries the backdrop, a navy scrim keeps copy legible in both
 * themes, and both calls to action lead to real destinations.
 */
function HomeHero({ anime, onOpen, onViewSeason }: HomeHeroProps) {
  const meta = metaLine([animeTypeLabel(anime.type), seasonLabel(anime.season), anime.year, animeStatusLabel(anime.status)]);
  const synopsis = cleanSynopsis(anime.synopsis);
  return (
    <section className="home-hero" aria-labelledby="home-hero-title">
      <div className="home-hero-media" aria-hidden="true">
        <Film className="home-art-fallback" size={52} strokeWidth={1.2} />
        {anime.posterUrl ? (
          <img
            className="home-hero-backdrop"
            src={mediaURL(anime.posterUrl)}
            alt=""
            loading="eager"
            decoding="async"
            onError={(event) => {
              event.currentTarget.style.display = "none";
            }}
          />
        ) : null}
        <span className="home-hero-scrim" />
      </div>
      <div className="container home-hero-inner">
        <div className="home-hero-copy">
          <h1 id="home-hero-title" className="home-hero-title">
            {anime.title}
          </h1>
          {meta ? <p className="home-hero-meta">{meta}</p> : null}
          {synopsis ? <p className="home-hero-synopsis">{synopsis}</p> : null}
          <div className="home-hero-actions">
            <Button variant="primary" icon={<Info aria-hidden="true" size={18} />} onClick={() => onOpen(anime)}>
              {copy.home.viewDetails}
            </Button>
            <Button variant="secondary" onClick={onViewSeason}>
              {copy.home.seasonAction}
            </Button>
          </div>
        </div>
      </div>
    </section>
  );
}

interface ContinueWatchingProps {
  items: HistoryItem[];
  onOpen: (anime: Anime, episodeId?: string) => void;
  onViewHistory: () => void;
  onRemove: (item: HistoryItem) => void;
}

/**
 * One card per title, newest first; the caller hides this when history is
 * empty. "Remove from Continue watching" only hides the title from this shelf:
 * the watch history entry and saved playback position stay intact, and the
 * backend clears the hide as soon as the title is watched again.
 */
function ContinueWatching({ items, onOpen, onViewHistory, onRemove }: ContinueWatchingProps) {
  return (
    <section className="home-section" aria-labelledby="home-continue-title">
      <SectionHeader
        id="home-continue-title"
        title={copy.home.continue}
        action={
          <Button variant="secondary" icon={<History aria-hidden="true" size={18} />} onClick={onViewHistory}>
            {copy.home.viewHistory}
          </Button>
        }
      />
      <ul className="home-history-grid">
        {items.map((item) => {
          const duration = item.progress.durationSeconds;
          const completed = item.progress.completed;
          const max = completed ? 1 : duration && duration > 0 ? duration : 0;
          const value = completed ? 1 : Math.min(item.progress.positionSeconds, max);
          const watchedOn = formatHistoryDate(item.lastWatchedAt);
          return (
            <li key={item.anime.id} className="home-history-card">
              <div className="home-history-thumb" aria-hidden="true">
                <Film size={22} strokeWidth={1.25} />
                {item.anime.posterUrl ? (
                  <img
                    src={mediaURL(item.anime.posterUrl)}
                    alt=""
                    loading="lazy"
                    decoding="async"
                    onError={(event) => {
                      event.currentTarget.style.display = "none";
                    }}
                  />
                ) : null}
              </div>
              <div className="home-history-body">
                <h3 className="home-history-title" title={item.anime.title}>
                  {item.anime.title}
                </h3>
                <p className="home-history-meta">
                  {metaLine([episodeLabel(item.episode.label), watchedOn, completed ? copy.history.completed : null])}
                </p>
                {max > 0 ? <ProgressBar label={copy.a11y.progress} value={value} max={max} /> : null}
                <div className="home-history-actions">
                  <Button
                    variant="secondary"
                    icon={<Play aria-hidden="true" size={16} />}
                    aria-label={copy.home.resume + ": " + item.anime.title}
                    onClick={() => onOpen(item.anime, item.episode.id)}
                  >
                    {copy.home.resume}
                  </Button>
                  <Button
                    variant="quiet"
                    icon={<X aria-hidden="true" size={16} />}
                    aria-label={copy.home.removeFromContinue + ": " + item.anime.title}
                    onClick={() => onRemove(item)}
                  >
                    {copy.home.removeFromContinue}
                  </Button>
                </div>
              </div>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

/** Layout-stable decorative skeleton for the first Home load. */
function HomeSkeleton() {
  return (
    <div className="home-page">
      <div className="home-hero home-hero--skeleton" aria-hidden="true">
        <div className="home-hero-media home-skeleton-media" />
        <div className="container home-hero-inner">
          <div className="home-hero-copy">
            <span className="home-skeleton-line home-skeleton-line--title" />
            <span className="home-skeleton-line home-skeleton-line--body" />
            <span className="home-skeleton-line home-skeleton-line--body home-skeleton-line--short" />
            <span className="home-skeleton-line home-skeleton-line--action" />
          </div>
        </div>
      </div>
      <div className="container home-body">
        <div className="home-section">
          <SkeletonGrid count={6} label={copy.states.loadingCatalog} />
        </div>
      </div>
    </div>
  );
}

export default function Home(props: HomePageProps) {
  const {
    feature,
    catalogue,
    history,
    loading,
    loadingMore,
    error,
    stale,
    hasMore,
    onOpen,
    onRemoveFromContinue,
    onViewSeason,
    onViewHistory,
    onLoadMore,
    onRetry,
  } = props;
  const hero = feature ?? catalogue[0] ?? null;
  const continueItems = history
    .filter((item, index, all) => all.findIndex((candidate) => candidate.anime.id === item.anime.id) === index)
    .slice(0, 6);

  if (error && catalogue.length === 0) {
    return (
      <div className="container page home-page">
        <h1 className="visually-hidden">{copy.nav.home}</h1>
        <ErrorState title={copy.home.errorTitle} description={error} retry={onRetry} retryLabel={copy.states.retry} />
      </div>
    );
  }

  if (loading && !hero && catalogue.length === 0) {
    return (
      <div className="home-page" aria-busy="true">
        <h1 className="visually-hidden">{copy.nav.home}</h1>
        <HomeSkeleton />
      </div>
    );
  }

  return (
    <div className="home-page">
      {hero ? (
        <HomeHero anime={hero} onOpen={onOpen} onViewSeason={onViewSeason} />
      ) : (
        <div className="container page">
          <h1 className="visually-hidden">{copy.nav.home}</h1>
          <EmptyState
            title={copy.home.noFeature}
            description={copy.home.emptyBody}
            action={
              <div className="home-empty-actions">
                <Button variant="secondary" onClick={onRetry}>
                  {copy.states.retry}
                </Button>
                <Button variant="primary" onClick={onViewSeason}>
                  {copy.home.seasonAction}
                </Button>
              </div>
            }
          />
        </div>
      )}

      <div className="container home-body">
        {continueItems.length > 0 ? (
          <ContinueWatching items={continueItems} onOpen={onOpen} onViewHistory={onViewHistory} onRemove={onRemoveFromContinue} />
        ) : null}

        {catalogue.length > 0 || loading ? (
          <section className="home-section" aria-labelledby="home-popular-title">
            <SectionHeader id="home-popular-title" title={copy.home.popular} />
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
            {catalogue.length > 0 ? (
              <div className="poster-grid">
                {catalogue.map((anime) => (
                  <PosterCard key={anime.id} anime={anime} onOpen={() => onOpen(anime)} />
                ))}
              </div>
            ) : (
              <SkeletonGrid count={6} label={copy.states.loading} />
            )}
            {hasMore && catalogue.length > 0 ? (
              <div className="home-more">
                <Button variant="secondary" onClick={onLoadMore} busy={loadingMore} disabled={loadingMore}>
                  {copy.home.loadMore}
                </Button>
                {loadingMore ? (
                  <span className="visually-hidden" role="status">
                    {copy.states.loadingMore}
                  </span>
                ) : null}
              </div>
            ) : null}
          </section>
        ) : null}
      </div>
    </div>
  );
}
