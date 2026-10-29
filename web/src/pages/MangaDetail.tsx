import { useState } from "react";
import { ArrowLeft, BookmarkPlus, BookOpen, Play } from "lucide-react";
import type { MangaDetailPageProps } from "../types";
import { copy, format, plural } from "../copy";
import { animeStatusLabel, animeTypeLabel, cleanSynopsis, formatHistoryDate, metaLine } from "../utils";
import { Badge, Button, EmptyState, ErrorState, SectionHeader } from "../components";
import "../styles/pages-library.css";
import "../styles/manga.css";

function Cover({ src, title }: { src?: string; title: string }) {
  const [failed, setFailed] = useState(false);
  if (!src || failed) {
    return (
      <div className="detail-cover detail-cover--empty" aria-hidden="true">
        <BookOpen size={44} strokeWidth={1.2} aria-hidden="true" />
      </div>
    );
  }
  return (
    <img
      className="detail-cover"
      src={src}
      alt={format(copy.a11y.posterAlt, { title })}
      loading="lazy"
      decoding="async"
      referrerPolicy="no-referrer"
      onError={() => setFailed(true)}
    />
  );
}

export default function MangaDetail({
  manga,
  chapters,
  state,
  error,
  bookmarked,
  bookmarkBusy,
  resumeChapterId,
  onBack,
  onRead,
  onToggleBookmark,
  onRetry,
}: MangaDetailPageProps) {
  const loading = state === "loading" || state === "idle";
  const failed = state === "error";
  const ready = state === "ready";
  const resumeChapter = chapters.find((chapter) => chapter.id === resumeChapterId) ?? null;
  const primaryRead = () => {
    if (chapters.length > 0) onRead(resumeChapter ?? chapters[0]);
  };

  const facts = metaLine([
    animeTypeLabel(manga.type),
    manga.year,
    animeStatusLabel(manga.status),
  ]);
  const alternativeTitles = manga.alternativeTitles?.filter((title) => title.trim() !== "") ?? [];
  const tags = manga.tags ?? [];
  const synopsis = cleanSynopsis(manga.synopsis);

  return (
    <div className="page detail-page manga-detail">
      <div className="container">
        <Button variant="quiet" className="detail-back" icon={<ArrowLeft size={18} aria-hidden="true" />} onClick={onBack}>
          {copy.manga.detail.back}
        </Button>

        <section className="detail-hero" aria-labelledby="manga-detail-title">
          <div className="detail-hero__art">
            <Cover src={manga.posterUrl} title={manga.title} />
          </div>
          <div className="detail-hero__copy">
            <h1 id="manga-detail-title" className="detail-hero__title">
              {manga.title}
            </h1>
            {facts ? <p className="detail-hero__facts">{facts}</p> : null}
            {manga.author ? <p className="detail-hero__facts">{manga.author}</p> : null}
            {alternativeTitles.length > 0 ? (
              <p className="detail-hero__alt">
                <span className="detail-hero__alt-label">{copy.manga.detail.alternateTitles}</span>
                <span>{alternativeTitles.join(" / ")}</span>
              </p>
            ) : null}
            <p className="detail-hero__synopsis">{synopsis || copy.manga.detail.noSynopsis}</p>
            {tags.length > 0 ? (
              <div className="manga-tags">
                {tags.map((tag) => (
                  <Badge key={tag}>{tag}</Badge>
                ))}
              </div>
            ) : null}
            <div className="detail-hero__actions">
              <Button
                variant="primary"
                icon={<Play size={18} aria-hidden="true" />}
                disabled={!ready || chapters.length === 0}
                onClick={primaryRead}
              >
                {resumeChapter ? copy.history.resume : copy.manga.detail.readFirst}
              </Button>
              <Button
                variant="secondary"
                icon={<BookmarkPlus size={18} aria-hidden="true" />}
                busy={bookmarkBusy}
                disabled={bookmarkBusy}
                onClick={onToggleBookmark}
              >
                {bookmarked ? copy.manga.detail.saved : copy.manga.detail.add}
              </Button>
            </div>
          </div>
        </section>

        <section className="detail-episodes manga-chapters" aria-labelledby="manga-chapters-heading" aria-busy={loading}>
          <SectionHeader
            id="manga-chapters-heading"
            title={copy.manga.detail.chapters}
            count={ready && chapters.length > 0 ? chapters.length : undefined}
          />

          {loading ? (
            <>
              <p className="visually-hidden" role="status">
                {copy.manga.detail.loadingChapters}
              </p>
              <div className="detail-skeleton" aria-hidden="true">
                {Array.from({ length: 6 }, (_, index) => (
                  <div className="detail-skeleton__row" key={index}>
                    <span className="detail-skeleton__number" />
                    <span className="detail-skeleton__title" />
                  </div>
                ))}
              </div>
            </>
          ) : failed ? (
            <ErrorState
              title={copy.manga.detail.chapterError}
              description={error || undefined}
              retry={onRetry}
              retryLabel={copy.manga.detail.retry}
            />
          ) : chapters.length === 0 ? (
            <EmptyState title={copy.manga.detail.noChapters} description={copy.manga.detail.noChaptersBody} />
          ) : (
            <ul className="manga-chapter-list">
              {chapters.map((chapter) => {
                // Sources that don't report a page count (MangaFire, Comix)
                // must not render "0 pages", which reads as an empty chapter.
                const meta = metaLine([
                  chapter.groupName,
                  formatHistoryDate(chapter.publishedAt ?? ""),
                  chapter.pageCount > 0
                    ? plural(chapter.pageCount, copy.manga.detail.pageCountOne, format(copy.manga.detail.pageCount, { count: chapter.pageCount }))
                    : null,
                ]);
                const rowLabel = chapter.title
                  ? `${format(copy.manga.detail.readChapter, { label: chapter.label })}: ${chapter.title}`
                  : format(copy.manga.detail.readChapter, { label: chapter.label });
                return (
                  <li key={chapter.id}>
                    <button
                      type="button"
                      className="manga-chapter"
                      aria-label={meta ? `${rowLabel}, ${meta}` : rowLabel}
                      onClick={() => onRead(chapter)}
                    >
                      <span className="manga-chapter__number tabular" aria-hidden="true">
                        {chapter.label}
                      </span>
                      <span className="manga-chapter__body" aria-hidden="true">
                        {chapter.title ? <span className="manga-chapter__title">{chapter.title}</span> : null}
                        {meta ? <span className="manga-chapter__meta">{meta}</span> : null}
                      </span>
                      <span className="manga-chapter__read" aria-hidden="true">
                        {copy.manga.detail.read}
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </section>
      </div>
    </div>
  );
}
