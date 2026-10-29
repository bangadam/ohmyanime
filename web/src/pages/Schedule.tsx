import { useState } from "react";
import { ChevronRight, Film } from "lucide-react";
import { mediaURL } from "../api";
import { Button, EmptyState, ErrorState, FilterChips, Notice, PageHeader } from "../components";
import { copy, format } from "../copy";
import type { ScheduleEntry, SchedulePageProps } from "../types";
import { formatLocalDate, formatLocalTime, localDay, weekdayName } from "../utils";
import "../styles/pages-browse.css";

/** Start the week on Monday; values stay native getDay() indices (0 = Sunday). */
const WEEK_ORDER = [1, 2, 3, 4, 5, 6, 0] as const;

function normaliseDay(value: number): number {
  if (!Number.isFinite(value)) return new Date().getDay();
  return ((Math.trunc(value) % 7) + 7) % 7;
}

function ScheduleSkeleton() {
  return (
    <div className="schedule-loading" role="status">
      <span className="visually-hidden">{copy.schedule.loading}</span>
      <div className="schedule-skeleton-list" aria-hidden="true">
        {[0, 1, 2, 3, 4].map((row) => (
          <div className="schedule-skeleton-row" key={row}>
            <span className="schedule-skeleton-thumb" />
            <span className="schedule-skeleton-lines">
              <span />
              <span />
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

export default function Schedule(props: SchedulePageProps) {
  const { entries, day, loading, error, localTimeZone, onDay, onOpen, onBrowseSeason, onRetry } = props;
  const selected = normaliseDay(day);
  const dayLabel = weekdayName(selected);
  const dayOptions = WEEK_ORDER.map((value) => ({ value: String(value), label: weekdayName(value) }));
  const weekEmpty = entries.length === 0;
  const dayEntries = entries
    .filter((entry) => localDay(entry.airingAt) === selected)
    .sort((a, b) => a.airingAt - b.airingAt);

  // A future airing has no watchable episode yet, so the row warns in place
  // instead of opening a detail page with nothing to play.
  const [notAired, setNotAired] = useState<ScheduleEntry | null>(null);
  const blockedId = notAired && dayEntries.some((entry) => entry.id === notAired.id) ? notAired.id : null;

  function openEntry(entry: ScheduleEntry) {
    if (entry.airingAt * 1000 > Date.now()) {
      setNotAired(entry);
      return;
    }
    setNotAired(null);
    onOpen(entry.anime);
  }

  return (
    <div className="container page schedule-page">
      <PageHeader title={copy.schedule.title} description={copy.schedule.description} />

      <div className="schedule-controls">
        <FilterChips
          label={copy.a11y.dayGroup}
          value={String(selected)}
          options={dayOptions}
          onChange={(value) => onDay(Number(value))}
        />
        <p className="schedule-local-time">
          {copy.schedule.localTime}
          {localTimeZone ? (
            <span className="schedule-timezone">{localTimeZone}</span>
          ) : null}
        </p>
      </div>

      <div className="schedule-body" aria-busy={loading}>
        {!loading && !error ? <h2 className="schedule-day-title">{dayLabel}</h2> : null}
        {loading && weekEmpty ? (
          <ScheduleSkeleton />
        ) : error ? (
          <ErrorState
            title={copy.schedule.errorTitle}
            description={error}
            retry={onRetry}
            retryLabel={copy.states.retry}
          />
        ) : weekEmpty ? (
          <EmptyState
            title={copy.schedule.emptyTitle}
            description={copy.schedule.emptyBody}
            action={
              <Button variant="primary" onClick={onBrowseSeason}>
                {copy.schedule.action}
              </Button>
            }
          />
        ) : dayEntries.length === 0 ? (
          <EmptyState
            title={copy.schedule.emptyTitle}
            description={format(copy.schedule.emptyForDay, { day: dayLabel }) + " " + copy.schedule.emptyBody}
            action={
              <Button variant="primary" onClick={onBrowseSeason}>
                {copy.schedule.action}
              </Button>
            }
          />
        ) : (
          <section className="schedule-day">
            <ul className="schedule-list">
              {dayEntries.map((entry) => (
                <li key={entry.id}>
                  <button type="button" className="schedule-row" onClick={() => openEntry(entry)}>
                    <span className="schedule-thumb" aria-hidden="true">
                      <Film size={20} strokeWidth={1.25} />
                      {entry.anime.posterUrl ? (
                        <img
                          src={mediaURL(entry.anime.posterUrl)}
                          alt=""
                          loading="lazy"
                          decoding="async"
                          onError={(event) => {
                            event.currentTarget.style.display = "none";
                          }}
                        />
                      ) : null}
                    </span>
                    <span className="schedule-time">
                      <time className="schedule-clock tabular" dateTime={new Date(entry.airingAt * 1000).toISOString()}>
                        {formatLocalTime(entry.airingAt)}
                      </time>
                      <span className="schedule-date">{formatLocalDate(entry.airingAt)}</span>
                    </span>
                    <span className="schedule-copy">
                      <span className="schedule-title">{entry.anime.title}</span>
                      <span className="schedule-episode">
                        {format(copy.schedule.episode, { label: String(entry.episode) })}
                      </span>
                    </span>
                    <ChevronRight className="schedule-chevron" aria-hidden="true" size={20} />
                  </button>
                  {blockedId === entry.id ? (
                    <Notice
                      message={format(copy.schedule.notAired, {
                        episode: String(entry.episode),
                        date: formatLocalDate(entry.airingAt),
                        time: formatLocalTime(entry.airingAt),
                      })}
                      onDismiss={() => setNotAired(null)}
                      dismissLabel={copy.states.closeNotice}
                    />
                  ) : null}
                </li>
              ))}
            </ul>
          </section>
        )}
      </div>
    </div>
  );
}
