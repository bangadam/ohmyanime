/*
  App shell — restrained cinematic top navigation on wide screens and a
  compact header plus safe-area-aware bottom navigation on narrow screens.
  One skip link, one main landmark, and named utility controls.
*/
import { useEffect, useState, type ReactNode } from "react";
import {
  ArrowUp,
  Bookmark,
  CalendarDays,
  BookOpen,
  Film,
  History,
  Home,
  Search,
  Settings,
  type LucideIcon,
} from "lucide-react";
import { isDesktop } from "../api";
import { copy } from "../copy";
import type { NavKey } from "../types";
import { IconButton } from "./ui";
import "../styles/shell.css";

export interface AppShellProps {
  /** Active primary destination; null while an overlay context owns the screen. */
  view: NavKey | null;
  onNavigate: (view: NavKey) => void;
  onSearch: () => void;
  children: ReactNode;
}

const PRIMARY_NAV: { key: NavKey; label: string }[] = [
  { key: "home", label: copy.nav.home },
  { key: "manga", label: copy.nav.manga },
  { key: "browse", label: copy.nav.browse },
  { key: "schedule", label: copy.nav.schedule },
  { key: "list", label: copy.nav.list },
];

/* Contract label for the narrow-screen destination is "Settings". */
const MOBILE_NAV: { key: NavKey; label: string; Icon: LucideIcon }[] = [
  { key: "home", label: copy.nav.home, Icon: Home },
  { key: "manga", label: "Manga", Icon: BookOpen },
  { key: "browse", label: copy.nav.browse, Icon: Film },
  { key: "schedule", label: copy.nav.schedule, Icon: CalendarDays },
  { key: "list", label: copy.nav.list, Icon: Bookmark },
  { key: "settings", label: "Settings", Icon: Settings },
];


export function AppShell({ view, onNavigate, onSearch, children }: AppShellProps) {
  const desktop = isDesktop();

  return (
    <div className={desktop ? "app-shell app-shell--desktop" : "app-shell"}>
      <a className="skip-link" href="#main">
        {copy.skip}
      </a>

      <header className="top-nav">
        <div className="top-nav__inner">
          <button
            type="button"
            className="wordmark top-nav__wordmark"
            onClick={() => onNavigate("home")}
          >
            {copy.brand}
          </button>

          <nav className="top-nav__primary" aria-label={copy.nav.primary}>
            {PRIMARY_NAV.map((item) => (
              <button
                key={item.key}
                type="button"
                className="top-nav__link"
                aria-current={view === item.key ? "page" : undefined}
                onClick={() => onNavigate(item.key)}
              >
                {item.label}
              </button>
            ))}
          </nav>

          <div className="top-nav__utilities">
            <IconButton
              label={copy.nav.search}
              icon={<Search size={20} strokeWidth={1.7} />}
              aria-current={view === "search" ? "page" : undefined}
              onClick={onSearch}
            />
            <IconButton
              label={copy.nav.history}
              icon={<History size={20} strokeWidth={1.7} />}
              aria-current={view === "history" ? "page" : undefined}
              onClick={() => onNavigate("history")}
            />
            <IconButton
              label={copy.nav.preferences}
              icon={<Settings size={20} strokeWidth={1.7} />}
              aria-current={view === "settings" ? "page" : undefined}
              onClick={() => onNavigate("settings")}
            />
          </div>
        </div>
      </header>

      <header className="app-shell__mobile-header">
        <button
          type="button"
          className="wordmark"
          onClick={() => onNavigate("home")}
        >
          {copy.brand}
        </button>
        <div className="app-shell__mobile-utilities">
          <IconButton
            label={copy.nav.search}
            icon={<Search size={20} strokeWidth={1.7} />}
            aria-current={view === "search" ? "page" : undefined}
            onClick={onSearch}
          />
          <IconButton
            label={copy.nav.history}
            icon={<History size={20} strokeWidth={1.7} />}
            aria-current={view === "history" ? "page" : undefined}
            onClick={() => onNavigate("history")}
          />
        </div>
      </header>

      <main id="main" tabIndex={-1} className="app-main">
        {children}
      </main>

      <BackToTop />

      <nav className="mobile-nav" aria-label={copy.nav.mobile}>
        <ul className="mobile-nav__list">
          {MOBILE_NAV.map((item) => (
            <li className="mobile-nav__item" key={item.key}>
              <button
                type="button"
                className="mobile-nav__link"
                aria-current={view === item.key ? "page" : undefined}
                onClick={() => onNavigate(item.key)}
              >
                <item.Icon size={21} strokeWidth={1.7} aria-hidden="true" />
                <span className="mobile-nav__label">{item.label}</span>
              </button>
            </li>
          ))}
        </ul>
      </nav>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Back-to-top widget                                                  */
/* ------------------------------------------------------------------ */

/*
  Long episode and catalogue pages need a single, predictable way back to the
  top. The widget stays mounted so it can fade, but it is inert and hidden
  from assistive technology while the page is still near the top.
*/
const BACK_TO_TOP_OFFSET = 480;

function BackToTop() {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    const sync = () => setVisible(window.scrollY > BACK_TO_TOP_OFFSET);
    sync();
    window.addEventListener("scroll", sync, { passive: true });
    return () => window.removeEventListener("scroll", sync);
  }, []);

  return (
    <button
      type="button"
      className="back-to-top"
      data-visible={visible ? "true" : "false"}
      aria-hidden={visible ? undefined : true}
      inert={!visible}
      aria-label={copy.backToTop}
      title={copy.backToTop}
      onClick={() => {
        const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
        window.scrollTo({ top: 0, behavior: reduced ? "auto" : "smooth" });
        document.getElementById("main")?.focus({ preventScroll: true });
      }}
    >
      <ArrowUp size={18} strokeWidth={1.8} aria-hidden="true" />
      <span className="back-to-top__label">{copy.backToTop}</span>
    </button>
  );
}

