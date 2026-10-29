import { useState, type ReactNode } from "react";
import { Clapperboard, Play } from "lucide-react";
import { mediaURL } from "../api";
import type { HistoryPageProps } from "../types";
import { copy, episodeLabel, format } from "../copy";
import { cleanEpisodeTitle, formatClock, formatHistoryDate, metaLine } from "../utils";
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  PageHeader,
  ProgressBar,
} from "../components";
import "../styles/pages-library.css";

function Thumb({ src }: { src?: string }) {
  const [failed, setFailed] = useState(false);
  if (!src || failed) {
    return (
      <div className="history-item__art history-item__art--empty" aria-hidden="true">
        <Clapperboard size={28} strokeWidth={1.2} aria-hidden="true" />
      </div>
    );
  }
  return (
    <img
      className="history-item__art"
      src={src}
      alt=""
      loading="lazy"
      decoding="async"
      onError={() => setFailed(true)}
    />
  );
}

export default function History({
  items,
  loading,
  loadingMore,
  error,
  hasMore,
  onResume,
  onEpisodes,
  onClear,
  onLoadMore,
  onRetry,
  onExplore,
}: HistoryPageProps) {
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
  if (error) {
    body = (
      <ErrorState
        title={copy.history.errorTitle}
        description={error}
        retry={onRetry}
        retryLabel={copy.states.retry}
      />
    );
  } else if (loading && items.length === 0) {
    body = (
      <>
        <p className="visually-hidden" role="status">
          {copy.history.loading}
        </p>
        <div className="history-skeleton" aria-hidden="true" aria-busy="true">
          {Array.from({ length: 4 }, (_, index) => (
            <div className="history-skeleton__row" key={index}>
              <span className="history-skeleton__art" />
              <span className="history-skeleton__body">
                <span className="history-skeleton__title" />
                <span className="history-skeleton__meta" />
              </span>
            </div>
          ))}
        </div>
      </>
    );
  } else if (items.length === 0) {
    body = (
      <EmptyState
        title={copy.history.emptyTitle}
        description={copy.history.emptyBody}
        action={
          <Button variant="primary" onClick={onExplore}>
            {copy.history.emptyAction}
          </Button>
        }
      />
    );
  } else {
    body = (
      <>
        <ul className="history-list">
          {items.map((item) => {
            const duration = item.progress.durationSeconds;
            const hasDuration = typeof duration === "number" && Number.isFinite(duration) && duration > 0;
            const watchedAt = formatHistoryDate(item.lastWatchedAt);
            const episodeTitle = cleanEpisodeTitle(item.episode.title, item.episode.label, item.episode.ordinal);
            const position = item.progress.completed && hasDuration ? duration : item.progress.positionSeconds;
            return (
              <li className="history-item" key={item.episode.id}>
                <Thumb src={item.anime.posterUrl ? mediaURL(item.anime.posterUrl) : undefined} />
                <div className="history-item__body">
                  <h2 className="history-item__title">{item.anime.title}</h2>
                  <p className="history-item__meta">
                    {metaLine([episodeLabel(item.episode.label), episodeTitle, watchedAt])}
                  </p>
                  <div className="history-item__progress">
                    {hasDuration ? (
                      <ProgressBar
                        value={Math.min(position, duration)}
                        max={duration}
                        label={copy.a11y.progress}
                      />
                    ) : null}
                    <p className="history-item__progress-copy">
                      {item.progress.completed ? (
                        <Badge tone="accent">{copy.history.completed}</Badge>
                      ) : null}
                      {hasDuration ? (
                        <span>
                          {format(copy.history.progress, {
                            position: formatClock(position),
                            duration: formatClock(duration),
                          })}
                        </span>
                      ) : item.progress.completed ? null : (
                        <span>
                          <span className="visually-hidden">{copy.a11y.progress} </span>
                          {formatClock(item.progress.positionSeconds)}
                        </span>
                      )}
                    </p>
                  </div>
                </div>
                <div className="history-item__actions">
                  <Button
                    variant="secondary"
                    icon={<Play size={17} aria-hidden="true" />}
                    onClick={() => onResume(item)}
                  >
                    {copy.history.resume}
                  </Button>
                  <Button variant="quiet" onClick={() => onEpisodes(item)}>
                    {copy.history.episodes}
                  </Button>
                </div>
              </li>
            );
          })}
        </ul>
        {loadMoreButton ? <div className="history-more">{loadMoreButton}</div> : null}
      </>
    );
  }

  return (
    <div className="page history-page">
      <div className="container">
        <PageHeader
          title={copy.history.title}
          description={copy.history.description}
          actions={
            <Button
              variant="danger"
              disabled={loading || items.length === 0}
              onClick={onClear}
            >
              {copy.history.clear}
            </Button>
          }
        />
        {body}
      </div>
    </div>
  );
}
