/*
  Shared UI components — OhMyAnime Streaming V2 / Cinematic Blue.
  Native elements only, tokens only, 44px targets, named icon controls,
  light/dark/system and forced-colors safe, reduced-motion friendly.
*/
import {
  useEffect,
  useId,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
} from "react";
import { ChevronDown, ImageOff, X } from "lucide-react";
import { mediaURL, type Anime } from "../api";
import { copy, format } from "../copy";
import { animeStatusLabel, animeTypeLabel, metaLine } from "../utils";
import "../styles/components.css";

/* ------------------------------------------------------------------ */
/* Spinner                                                             */
/* ------------------------------------------------------------------ */

/** Decorative rotating ring; the announcing wrapper is Spinner below. */
function SpinnerRing({ className }: { className?: string }) {
  return (
    <span
      className={className ? "spinner-ring " + className : "spinner-ring"}
      aria-hidden="true"
    />
  );
}

export function Spinner({ label }: { label: string }) {
  return (
    <span className="spinner" role="status">
      <SpinnerRing />
      <span className="visually-hidden">{label}</span>
    </span>
  );
}

/* ------------------------------------------------------------------ */
/* Buttons                                                             */
/* ------------------------------------------------------------------ */

export type ButtonVariant = "primary" | "secondary" | "quiet" | "danger";

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  icon?: ReactNode;
  iconAfter?: ReactNode;
  busy?: boolean;
}

export function Button({
  variant = "secondary",
  icon,
  iconAfter,
  busy = false,
  className,
  type = "button",
  disabled,
  children,
  ...rest
}: ButtonProps) {
  const iconSlot = busy ? (
    <SpinnerRing className="btn__spinner" />
  ) : icon ? (
    <span className="btn__icon" aria-hidden="true">
      {icon}
    </span>
  ) : null;

  return (
    <button
      {...rest}
      type={type}
      className={["btn", "btn--" + variant, className].filter(Boolean).join(" ")}
      disabled={disabled || busy}
      aria-busy={busy || undefined}
    >
      {iconSlot}
      <span className="btn__label">{children}</span>
      {iconAfter ? (
        <span className="btn__icon" aria-hidden="true">
          {iconAfter}
        </span>
      ) : null}
    </button>
  );
}

export interface IconButtonProps
  extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, "children" | "aria-label"> {
  label: string;
  icon: ReactNode;
  variant?: "plain" | "raised";
}

export function IconButton({
  label,
  icon,
  variant = "plain",
  className,
  type = "button",
  title,
  ...rest
}: IconButtonProps) {
  return (
    <button
      {...rest}
      type={type}
      className={["icon-btn", "icon-btn--" + variant, className].filter(Boolean).join(" ")}
      aria-label={label}
      title={title ?? label}
    >
      <span className="icon-btn__glyph" aria-hidden="true">
        {icon}
      </span>
    </button>
  );
}

/* ------------------------------------------------------------------ */
/* Form fields                                                         */
/* ------------------------------------------------------------------ */

export interface TextFieldProps extends Omit<InputHTMLAttributes<HTMLInputElement>, "id"> {
  id: string;
  label: string;
  hint?: string;
  error?: string;
  trailing?: ReactNode;
}

export function TextField({
  id,
  label,
  hint,
  error,
  trailing,
  className,
  "aria-describedby": ariaDescribedBy,
  ...rest
}: TextFieldProps) {
  const hintId = hint ? id + "-hint" : undefined;
  const errorId = error ? id + "-error" : undefined;
  const describedBy =
    [ariaDescribedBy, hintId, errorId].filter(Boolean).join(" ") || undefined;

  return (
    <div className={["field", error ? "field--invalid" : "", className].filter(Boolean).join(" ")}>
      <label className="field__label" htmlFor={id}>
        {label}
      </label>
      <div
        className={
          trailing ? "field__control field__control--trailing" : "field__control"
        }
      >
        <input
          {...rest}
          id={id}
          className="field__input"
          aria-invalid={error ? true : undefined}
          aria-describedby={describedBy}
        />
        {trailing ? <span className="field__trailing">{trailing}</span> : null}
      </div>
      {hint ? (
        <p className="field__hint" id={hintId}>
          {hint}
        </p>
      ) : null}
      {error ? (
        <p className="field__error" id={errorId} role="alert">
          {error}
        </p>
      ) : null}
    </div>
  );
}

export interface SelectFieldProps
  extends Omit<SelectHTMLAttributes<HTMLSelectElement>, "id"> {
  id: string;
  label: string;
  hint?: string;
  options: { value: string; label: string; disabled?: boolean }[];
}

export function SelectField({
  id,
  label,
  hint,
  options,
  className,
  "aria-describedby": ariaDescribedBy,
  ...rest
}: SelectFieldProps) {
  return (
    <div className={["field", "field--select", className].filter(Boolean).join(" ")}>
      <label className="field__label" htmlFor={id}>
        {label}
      </label>
      <div className="field__control field__control--select">
        <select
          {...rest}
          id={id}
          className="select__control"
          aria-describedby={[ariaDescribedBy, hint ? id + "-hint" : undefined].filter(Boolean).join(" ") || undefined}
        >
          {options.map((option) => (
            <option key={option.value} value={option.value} disabled={option.disabled}>
              {option.label}
            </option>
          ))}
        </select>
        <ChevronDown className="select__icon" size={18} strokeWidth={1.8} aria-hidden="true" />
      </div>
      {hint ? (
        <p className="field__hint" id={id + "-hint"}>
          {hint}
        </p>
      ) : null}
    </div>
  );
}

export function SettingRow({
  title,
  description,
  htmlFor,
  children,
}: {
  title: string;
  description?: string;
  htmlFor?: string;
  children: ReactNode;
}) {
  return (
    <div className="setting-row">
      <div className="setting-row__text">
        {htmlFor ? (
          <label className="setting-row__title" htmlFor={htmlFor}>
            {title}
          </label>
        ) : (
          <p className="setting-row__title">{title}</p>
        )}
        {description ? <p className="setting-row__description">{description}</p> : null}
      </div>
      <div className="setting-row__control">{children}</div>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Headers                                                             */
/* ------------------------------------------------------------------ */

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  return (
    <header className="page-header">
      <div className="page-header__text">
        <h1 className="page-header__title">{title}</h1>
        {description ? <p className="page-header__description">{description}</p> : null}
      </div>
      {actions ? <div className="page-header__actions">{actions}</div> : null}
    </header>
  );
}

export function SectionHeader({
  title,
  action,
  id,
  count,
}: {
  title: string;
  action?: ReactNode;
  id?: string;
  count?: number;
}) {
  return (
    <div className="section-header">
      <h2 className="section-header__title" id={id}>
        {title}
        {typeof count === "number" ? (
          <>
            {" "}
            <span className="section-header__count">{count}</span>
          </>
        ) : null}
      </h2>
      {action ? <div className="section-header__action">{action}</div> : null}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Poster card                                                         */
/* ------------------------------------------------------------------ */

export function PosterCard({
  anime,
  onOpen,
  footer,
  action,
  priority = false,
}: {
  anime: Anime;
  onOpen: () => void;
  footer?: ReactNode;
  action?: ReactNode;
  priority?: boolean;
}) {
  const [failedUrl, setFailedUrl] = useState<string | null>(null);
  const showArtwork = Boolean(anime.posterUrl) && failedUrl !== anime.posterUrl;
  const meta = metaLine([animeTypeLabel(anime.type), anime.year, animeStatusLabel(anime.status)]);
  const openLabel = format(copy.home.viewDetails + ": {title}", { title: anime.title });

  return (
    <article className="poster-card">
      <div className="poster-card__media">
        <button
          type="button"
          className="poster-card__open"
          onClick={onOpen}
          aria-label={openLabel}
        >
          {showArtwork ? (
            <img
              className="poster-card__art"
              src={mediaURL(anime.posterUrl ?? "")}
              alt=""
              loading={priority ? "eager" : "lazy"}
              decoding="async"
              fetchPriority={priority ? "high" : "auto"}
              onError={() => setFailedUrl(anime.posterUrl ?? null)}
            />
          ) : (
            <span className="poster-card__fallback" aria-hidden="true">
              <ImageOff size={32} strokeWidth={1.25} />
            </span>
          )}
        </button>
        {action ? <div className="poster-card__action">{action}</div> : null}
      </div>
      <div className="poster-card__body">
        <h3 className="poster-card__title" title={anime.title}>
          <button type="button" className="poster-card__title-button" onClick={onOpen}>
            {anime.title}
          </button>
        </h3>
        {meta ? <p className="poster-card__meta">{meta}</p> : null}
        {footer ? <div className="poster-card__footer">{footer}</div> : null}
      </div>
    </article>
  );
}

/* ------------------------------------------------------------------ */
/* Loading, empty, error                                               */
/* ------------------------------------------------------------------ */

export function SkeletonGrid({ count = 6, label }: { count?: number; label?: string }) {
  const total = Math.max(1, Math.round(count));
  const message = label ?? copy.states.loading;

  return (
    <div className="skeleton-grid">
      <p className="visually-hidden" role="status">
        {message}
      </p>
      <div className="poster-grid" aria-busy="true">
        {Array.from({ length: total }, (_, index) => (
          <div className="skeleton-card" key={index} aria-hidden="true">
            <div className="skeleton skeleton-card__art" />
            <div className="skeleton skeleton-card__line skeleton-card__line--title" />
            <div className="skeleton skeleton-card__line skeleton-card__line--meta" />
          </div>
        ))}
      </div>
    </div>
  );
}

export function EmptyState({
  title,
  description,
  action,
  icon,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
  icon?: ReactNode;
}) {
  return (
    <div className="empty-state">
      {icon ? (
        <div className="empty-state__icon" aria-hidden="true">
          {icon}
        </div>
      ) : null}
      <h2 className="empty-state__title">{title}</h2>
      {description ? <p className="empty-state__body">{description}</p> : null}
      {action ? <div className="empty-state__actions">{action}</div> : null}
    </div>
  );
}

export function ErrorState({
  title,
  description,
  retry,
  retryLabel,
  children,
}: {
  title: string;
  description?: string;
  retry?: () => void;
  retryLabel?: string;
  children?: ReactNode;
}) {
  const label = retryLabel ?? copy.states.retry;

  return (
    <div className="error-state" role="alert">
      <h2 className="error-state__title">{title}</h2>
      {description ? <p className="error-state__body">{description}</p> : null}
      {retry || children ? (
        <div className="error-state__actions">
          {retry ? (
            <Button variant="secondary" onClick={retry}>
              {label}
            </Button>
          ) : null}
          {children}
        </div>
      ) : null}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Notice                                                              */
/* ------------------------------------------------------------------ */

export function Notice({
  message,
  tone = "status",
  action,
  onDismiss,
  dismissLabel,
}: {
  message: string;
  tone?: "status" | "error";
  action?: ReactNode;
  onDismiss?: () => void;
  dismissLabel?: string;
}) {
  if (!message) return <div className="notice-slot" role={tone === "error" ? "alert" : "status"} />;

  return (
    <div
      className={"notice notice--" + tone}
      role={tone === "error" ? "alert" : "status"}
      aria-atomic="true"
    >
      <p className="notice__message">{message}</p>
      {action ? <div className="notice__action">{action}</div> : null}
      {onDismiss ? (
        <IconButton
          className="notice__dismiss"
          label={dismissLabel ?? copy.states.closeNotice}
          icon={<X size={18} aria-hidden="true" />}
          onClick={onDismiss}
        />
      ) : null}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Confirm dialog                                                      */
/* ------------------------------------------------------------------ */

export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel,
  cancelLabel,
  destructive = false,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  description: string;
  confirmLabel: string;
  cancelLabel?: string;
  destructive?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const returnFocusRef = useRef<HTMLElement | null>(null);
  const titleId = useId();
  const descriptionId = useId();
  const cancelText = cancelLabel ?? copy.preferences.cancel;

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!open || !dialog) return;

    returnFocusRef.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;

    if (!dialog.open) dialog.showModal();
    const frame = window.requestAnimationFrame(() => cancelRef.current?.focus());

    return () => {
      window.cancelAnimationFrame(frame);
      if (dialog.open) dialog.close();
      const target = returnFocusRef.current;
      if (target && target.isConnected) target.focus();
    };
  }, [open]);

  if (!open) return null;

  return (
    <dialog
      ref={dialogRef}
      className="dialog"
      aria-labelledby={titleId}
      aria-describedby={descriptionId}
      onCancel={(event) => {
        event.preventDefault();
        onCancel();
      }}
      onClick={(event) => {
        if (event.target === dialogRef.current) onCancel();
      }}
    >
      <div className="dialog__panel">
        <h2 className="dialog__title" id={titleId}>
          {title}
        </h2>
        <p className="dialog__description" id={descriptionId}>
          {description}
        </p>
        <div className="dialog__actions">
          <button
            ref={cancelRef}
            type="button"
            className="btn btn--secondary"
            onClick={onCancel}
          >
            {cancelText}
          </button>
          <button
            type="button"
            className={destructive ? "btn btn--danger" : "btn btn--primary"}
            onClick={onConfirm}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </dialog>
  );
}

/* ------------------------------------------------------------------ */
/* Filter chips                                                        */
/* ------------------------------------------------------------------ */

export function FilterChips<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T;
  options: { value: T; label: string }[];
  onChange: (value: T) => void;
}) {
  return (
    <div className="chips" role="group" aria-label={label}>
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          className="chips__chip"
          aria-pressed={option.value === value}
          onClick={() => onChange(option.value)}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Progress and badges                                                 */
/* ------------------------------------------------------------------ */

export function ProgressBar({
  value,
  label,
  max = 100,
}: {
  value: number;
  label: string;
  max?: number;
}) {
  const safeMax = Number.isFinite(max) && max > 0 ? max : 100;
  const safeValue = Math.min(
    safeMax,
    Math.max(0, Number.isFinite(value) ? value : 0),
  );

  return (
    <progress
      className="progress-meter"
      value={safeValue}
      max={safeMax}
      aria-label={label}
    />
  );
}

export function Badge({
  children,
  tone = "neutral",
}: {
  children: ReactNode;
  tone?: "neutral" | "accent";
}) {
  return <span className={"badge badge--" + tone}>{children}</span>;
}
