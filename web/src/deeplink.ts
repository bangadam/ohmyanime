import { event, isMyGo } from "mygo-runtime";

/** A validated title deep link sent by the desktop shell (internal/desktop/deeplink.go). */
export interface TitleLink {
  kind: string;
  titleId: string;
}

/** The mygo event name the Go side emits; the URL itself never reaches the page. */
const TITLE_LINK_EVENT = "deeplink:title";

export function isTitleLink(value: unknown): value is TitleLink {
  if (typeof value !== "object" || value === null) return false;
  const link = value as { kind?: unknown; titleId?: unknown };
  return typeof link.kind === "string" && typeof link.titleId === "string" && link.titleId !== "";
}

/**
 * Subscribes to title deep links from the desktop shell. Cold and warm
 * launches both arrive here; the payload is already allowlisted Go-side, and
 * isTitleLink keeps the web side defensive. Returns an unsubscribe function;
 * outside the desktop shell this is a no-op.
 */
export function onTitleLink(handler: (link: TitleLink) => void): () => void {
  if (!isMyGo()) return () => {};
  return event<TitleLink>(TITLE_LINK_EVENT).on((payload) => {
    if (isTitleLink(payload)) handler(payload);
  });
}
