# Roadmap

Personal desktop anime streaming, with more providers and a multi-provider manga reader next. One local library; no account platform or multi-tenant hosting.

Tracking issue: https://github.com/bangadam/ohmyanime/issues/29

## Priorities and sequencing

- **P1:** provider contracts, safe manga foundations, diagnostics and regression coverage.
- **P2:** product depth and desktop integration after their prerequisites.
- **P3:** optional signing/distribution polish.
- Milestones are workstreams, not dated release promises. Logging and tests start early, not after the UI work.
- Use shared provider identity plus separate anime/manga interfaces. Prefer in-process registered adapters; do not introduce downloaded executable plugins.
- A native deep-link handler is not the same thing as a WebView custom-content protocol. Validate platform support before choosing an API.
- Health signals describe observed reachability, not a guarantee that every episode plays.

## Initial delivery slice

1. Provider registry + capability model + adapter-local mapping.
2. Manga identities + one suitable provider + bounded image proxy.
3. Reader + chapter navigation + restart/resume.
4. Unified anime/manga library and search.

Desktop integration and player improvements can proceed independently where no dependency is listed. No multi-user, Docker, admin service, DRM or native mobile app is planned. Provider terms and content rights still need review for each integration.

## M1 — Provider architecture

### 1.1 Introduce typed provider interfaces and a registry

**Issue:** [#1](https://github.com/bangadam/ohmyanime/issues/1) · **Priority:** P1

**Depends on:** None.

**Scope:** Separate anime and manga contracts with shared provider identity; keep caching, cancellation and explicit-provider selection in orchestration. Migrate the three existing anime adapters; do not add dynamically downloaded executable plugins.

**Acceptance:**
- [ ] Existing anime search, inventory and playback retain behavior.
- [ ] adding an adapter requires registration rather than editing resolver branches.
- [ ] explicit selection never silently switches provider.
- [ ] cancellation closes upstream work.

### 1.2 Expose provider capabilities without claiming title availability

**Issue:** [#2](https://github.com/bangadam/ohmyanime/issues/2) · **Priority:** P1

**Depends on:** [1.1](https://github.com/bangadam/ohmyanime/issues/1)

**Scope:** Define typed capabilities for supported media kinds, audio modes and reader formats. Keep provider-level support separate from episode/chapter availability and session-discovered tracks.

**Acceptance:**
- [ ] Unsupported actions are absent.
- [ ] missing capabilities fail conservatively.
- [ ] a globally supported dub mode does not imply every title has dub.
- [ ] changing providers refreshes actual track options.

### 1.3 Report bounded provider health with last-check timestamps

**Issue:** [#3](https://github.com/bangadam/ohmyanime/issues/3) · **Priority:** P2

**Depends on:** [1.1](https://github.com/bangadam/ohmyanime/issues/1)

**Scope:** Represent unknown, reachable, degraded and unavailable states; add manual refresh and a low-frequency bounded probe. Distinguish homepage reachability from successful playback.

**Acceptance:**
- [ ] Timeout and offline results carry checkedAt.
- [ ] rendering a page does not trigger probes for every title.
- [ ] manual selection remains explicit.
- [ ] recovery restores eligibility without restart.

### 1.4 Move site-specific mapping rules into adapters

**Issue:** [#4](https://github.com/bangadam/ohmyanime/issues/4) · **Priority:** P1

**Depends on:** [1.1](https://github.com/bangadam/ohmyanime/issues/1)

**Scope:** Move title matching, season distinctions and episode labels into adapter-owned parsing; retain shared normalization only where semantics genuinely match.

**Acceptance:**
- [ ] Ambiguous matches are rejected.
- [ ] season two never maps to season one.
- [ ] decimal episodes and specials preserve identity.
- [ ] stored progress is unchanged after migration.

## M2 — Manga reader

### 2.1 Add manga and chapter identities with safe SQLite migrations

**Issue:** [#5](https://github.com/bangadam/ohmyanime/issues/5) · **Priority:** P1

**Depends on:** [1.1](https://github.com/bangadam/ohmyanime/issues/1)

**Scope:** Add manga, chapter, provider mappings and reading-progress domain types. Define stable IDs that preserve language, volume, decimal chapter and special distinctions; migrate existing installations without resetting anime data.

**Acceptance:**
- [ ] Migration preserves anime bookmarks and progress.
- [ ] chapters 12, 12.5 and specials remain distinct.
- [ ] chapter ordering is deterministic.
- [ ] rerunning migrations is safe.

### 2.2 Serve manga page images through a bounded image proxy

**Issue:** [#6](https://github.com/bangadam/ohmyanime/issues/6) · **Priority:** P1

**Depends on:** [2.1](https://github.com/bangadam/ohmyanime/issues/5)

**Scope:** Resolve page inventories to capability-scoped local image URLs; reuse safe network transport without forcing images through HLS parsing. Bound image size, cache, decoding inputs and request concurrency.

**Acceptance:**
- [ ] Private-network and redirect SSRF attempts fail.
- [ ] expired capabilities reject access.
- [ ] HTML disguised as an image is rejected.
- [ ] missing page produces a recoverable error.
- [ ] abandoned preloads are cancelled.

### 2.3 Build paged and vertical-scroll manga reading modes

**Issue:** [#7](https://github.com/bangadam/ohmyanime/issues/7) · **Priority:** P1

**Depends on:** [2.1](https://github.com/bangadam/ohmyanime/issues/5), [2.2](https://github.com/bangadam/ohmyanime/issues/6), [2.7](https://github.com/bangadam/ohmyanime/issues/11)

**Scope:** Implement vertical strip and paged LTR/RTL modes, fit width/height, zoom and optional two-page spread. Keep logical page index independent of presentation and use bounded preloading.

**Acceptance:**
- [ ] RTL changes page-turn direction correctly.
- [ ] changing mode or resizing retains the visible logical page.
- [ ] spreads handle odd final pages.
- [ ] keyboard works without intercepting text inputs.
- [ ] retry restores a failed page.

### 2.4 Add manga chapter navigation and reading-state indicators

**Issue:** [#8](https://github.com/bangadam/ohmyanime/issues/8) · **Priority:** P1

**Depends on:** [2.1](https://github.com/bangadam/ohmyanime/issues/5), [2.3](https://github.com/bangadam/ohmyanime/issues/7)

**Scope:** Add ordered chapter picker, previous/next navigation and an explicit end-of-chapter action. Preserve volume, language and specials rather than sorting lexically.

**Acceptance:**
- [ ] First/last chapters have correct controls.
- [ ] unavailable chapters cannot be opened.
- [ ] chapter 10 follows 9.
- [ ] changing chapter cancels prior page loads and moves focus predictably.

### 2.5 Persist and restore manga reading progress

**Issue:** [#9](https://github.com/bangadam/ohmyanime/issues/9) · **Priority:** P1

**Depends on:** [2.1](https://github.com/bangadam/ohmyanime/issues/5), [2.3](https://github.com/bangadam/ohmyanime/issues/7)

**Scope:** Save chapter ID, logical page index and completion state with debounced writes and a final save on close; handle changed page inventories and concurrent windows.

**Acceptance:**
- [ ] Restart resumes the exact logical page.
- [ ] scrolling past one page does not mark the whole chapter complete.
- [ ] removed pages clamp safely.
- [ ] stale writes do not overwrite newer progress.

### 2.6 Unify anime and manga discovery and the personal library

**Issue:** [#10](https://github.com/bangadam/ohmyanime/issues/10) · **Priority:** P2

**Depends on:** [2.1](https://github.com/bangadam/ohmyanime/issues/5), [2.5](https://github.com/bangadam/ohmyanime/issues/9), [2.7](https://github.com/bangadam/ohmyanime/issues/11)

**Scope:** Add Anime/Manga filters and kind-aware cards, details and resume actions. Keep namespaces separate and show partial failures when only one provider responds.

**Acceptance:**
- [ ] Same title text in anime and manga does not merge identities.
- [ ] opening each kind reaches the proper viewer.
- [ ] bookmark changes persist.
- [ ] one failed source does not hide successful results.

### 2.7 Integrate the first manga provider end to end

**Issue:** [#11](https://github.com/bangadam/ohmyanime/issues/11) · **Priority:** P1

**Depends on:** [1.1](https://github.com/bangadam/ohmyanime/issues/1), [1.2](https://github.com/bangadam/ohmyanime/issues/2), [1.4](https://github.com/bangadam/ohmyanime/issues/4), [2.1](https://github.com/bangadam/ohmyanime/issues/5)

**Scope:** Choose a provider with documented access terms and suitable public or licensed fixtures; record the choice before implementation. Implement search, details, chapter inventory and ordered page resolution behind the manga interface.

**Acceptance:**
- [ ] Search to chapter pages works using real responses.
- [ ] pagination and supported languages are preserved.
- [ ] parser fixtures cover missing chapters and malformed responses.
- [ ] no TLS or access-control bypass is introduced.

## M3 — Desktop integration

### 3.1 Add native menu and optional tray playback controls

**Issue:** [#12](https://github.com/bangadam/ohmyanime/issues/12) · **Priority:** P2

**Depends on:** None.

**Scope:** Create menu/tray actions for show window, play/pause, current title and quit. Define close-versus-quit behavior explicitly; tray is optional, not mandatory background operation.

**Acceptance:**
- [ ] Actions reflect actual playback.
- [ ] play/pause is disabled when no media exists.
- [ ] reopening focuses the window.
- [ ] quit closes listeners and playback.
- [ ] unsupported platforms hide unsupported actions.

### 3.2 Integrate OS media keys and now-playing metadata

**Issue:** [#13](https://github.com/bangadam/ohmyanime/issues/13) · **Priority:** P2

**Depends on:** None.

**Scope:** Use supported Media Session or native APIs for title/artwork, position and play/pause/seek. Capability-detect on each target platform; do not assume Chromium support in WKWebView.

**Acceptance:**
- [ ] Media keys control the active player only.
- [ ] metadata clears on stop.
- [ ] paused and playing states match media.
- [ ] unsupported hosts keep normal controls.
- [ ] no duplicate key handling occurs.

### 3.3 Add verified desktop update checks and installation

**Issue:** [#14](https://github.com/bangadam/ohmyanime/issues/14) · **Priority:** P2

**Depends on:** None.

**Scope:** Validate the framework updater against actual package targets before adopting it. Add manual check, version comparison, signed metadata or equivalent authenticity verification, download progress and restart consent.

**Acceptance:**
- [ ] Tampered or incompatible packages are rejected.
- [ ] interrupted download leaves the installed version intact.
- [ ] data survives update.
- [ ] no restart during playback without consent.
- [ ] unavailable update backend has a retryable state.

### 3.4 Support single-instance launch and safe title deep links

**Issue:** [#15](https://github.com/bangadam/ohmyanime/issues/15) · **Priority:** P2

**Depends on:** None.

**Scope:** Focus the existing instance on a second launch; register a native OS URL handler separately from the framework custom-content protocol. Parse a small allowlisted URL grammar for anime/manga IDs.

**Acceptance:**
- [ ] Second launch does not create a second database writer.
- [ ] valid title links work on cold and warm launch.
- [ ] malformed URLs and arbitrary external URLs are rejected.
- [ ] unavailable title shows recovery.

### 3.5 Create an original app icon for packaged desktop builds

**Issue:** [#16](https://github.com/bangadam/ohmyanime/issues/16) · **Priority:** P2

**Depends on:** None.

**Scope:** Design an original icon suitable for small sizes and light/dark docks; generate required platform assets and configure packaging. Preserve ownership/source information for the artwork.

**Acceptance:**
- [ ] Packaged app, dock/taskbar and launcher show the icon.
- [ ] it remains legible at small sizes.
- [ ] no third-party anime artwork or trademark is used as the app identity.

### 3.6 Add safe copy-diagnostics and open-log-folder actions

**Issue:** [#17](https://github.com/bangadam/ohmyanime/issues/17) · **Priority:** P2

**Depends on:** [5.1](https://github.com/bangadam/ohmyanime/issues/25)

**Scope:** Expose app version, OS, provider identifiers and recent error categories; require a deliberate copy action and explain what is included. Reuse structured redaction from logging.

**Acceptance:**
- [ ] Copied report contains no cookies, authorization, signed media URLs or personal history.
- [ ] log folder action works on supported hosts.
- [ ] report remains available after a provider failure.

### 3.7 Sign and notarize release artifacts where credentials permit

**Issue:** [#18](https://github.com/bangadam/ohmyanime/issues/18) · **Priority:** P3

**Depends on:** None.

**Scope:** Document per-platform credentials and configure signing without embedding secrets. Keep explicitly marked unsigned builds available when credentials are absent.

**Acceptance:**
- [ ] Signed artifacts validate on target systems.
- [ ] macOS notarization/stapling is checked where configured.
- [ ] CI cannot print signing credentials.
- [ ] unsigned output is never described as signed.

## M4 — Player improvements

### 4.1 Add capability-detected Picture-in-Picture playback

**Issue:** [#19](https://github.com/bangadam/ohmyanime/issues/19) · **Priority:** P2

**Depends on:** None.

**Scope:** Expose PiP when supported by the native browser engine, with enter/exit state and a graceful unsupported path. Keep the same media session and progress-saving lifecycle.

**Acceptance:**
- [ ] PiP preserves playback position, provider and captions where supported.
- [ ] closing PiP restores UI state.
- [ ] exiting the player disposes playback.
- [ ] unsupported engines never show a broken control.

### 4.2 Add manual watched and unwatched actions

**Issue:** [#20](https://github.com/bangadam/ohmyanime/issues/20) · **Priority:** P2

**Depends on:** None.

**Scope:** Support per-episode and bulk title/season watched state using real episode inventory. Define precedence with automatic completion and preserve positions when toggling flags.

**Acceptance:**
- [ ] A user can mark externally watched episodes without inventing duration.
- [ ] undo/unwatch behaves consistently.
- [ ] bulk actions affect only the selected title.
- [ ] history and resume agree after restart.

### 4.3 Remove one item from Continue watching

**Issue:** [#21](https://github.com/bangadam/ohmyanime/issues/21) · **Priority:** P2

**Depends on:** None.

**Scope:** Define removal as hiding a title from the resume shelf, distinct from deleting all history or playback position. Add a named action with feedback and undo where practical.

**Acceptance:**
- [ ] Only the chosen title disappears.
- [ ] unrelated progress/history remains intact.
- [ ] behavior survives restart.
- [ ] watching the title again returns it to the shelf according to documented semantics.

### 4.4 Optimize long episode lists without losing keyboard navigation

**Issue:** [#22](https://github.com/bangadam/ohmyanime/issues/22) · **Priority:** P2

**Depends on:** None.

**Scope:** Measure long inventories before choosing virtualization. Apply windowing only if measured performance warrants it; keep search/jump-to-episode and stable row IDs.

**Acceptance:**
- [ ] A 1000-plus-entry fixture stays responsive.
- [ ] first, middle and final entries remain reachable by keyboard.
- [ ] focus survives scrolling.
- [ ] fractional episodes and long labels are not lost.
- [ ] small lists remain simple.

### 4.5 Add subtitle background and edge-style preferences

**Issue:** [#23](https://github.com/bangadam/ohmyanime/issues/23) · **Priority:** P2

**Depends on:** None.

**Scope:** Extend existing subtitle size controls with background opacity and edge styles; persist locally and scope to soft subtitles. Explain limitations for hardsubbed video.

**Acceptance:**
- [ ] Settings survive restart.
- [ ] previews match rendered captions.
- [ ] text remains readable on bright/dark scenes.
- [ ] hardsubs are never claimed to be restyled.
- [ ] controls remain accessible.

### 4.6 Expose actionable playback failure stages

**Issue:** [#24](https://github.com/bangadam/ohmyanime/issues/24) · **Priority:** P1

**Depends on:** [5.1](https://github.com/bangadam/ohmyanime/issues/25)

**Scope:** Map resolver, manifest and segment errors to stable safe categories with retry, choose-provider and back actions. Reuse request IDs and sanitized diagnostics, not raw upstream responses.

**Acceptance:**
- [ ] A timeout is distinct from title absence.
- [ ] explicit provider failures never silently switch.
- [ ] UI does not reveal credentials or signed URLs.
- [ ] retry targets the appropriate stage without creating orphan sessions.

## M5 — Durability and quality

### 5.1 Add bounded local structured logs with redaction

**Issue:** [#25](https://github.com/bangadam/ohmyanime/issues/25) · **Priority:** P1

**Depends on:** None.

**Scope:** Add rotating local logs for packaged desktop failures, configurable retention and safe defaults. Share a central redaction policy for headers, query tokens and local paths.

**Acceptance:**
- [ ] Rotation caps disk usage.
- [ ] credentials and signed URLs are redacted in failure paths.
- [ ] unwritable log directory does not crash playback.
- [ ] shutdown flushes buffered events.

### 5.2 Export and restore the personal library safely

**Issue:** [#26](https://github.com/bangadam/ohmyanime/issues/26) · **Priority:** P2

**Depends on:** None.

**Scope:** Use a consistent SQLite backup mechanism rather than copying a live database file. Validate schema/version, preview replacement consequences and create a pre-restore backup.

**Acceptance:**
- [ ] Restore recovers bookmarks and progress.
- [ ] active writes cannot corrupt export.
- [ ] incompatible or damaged imports are rejected before replacement.
- [ ] failed restore leaves original data usable.

### 5.3 Use cached catalogue data when metadata is offline

**Issue:** [#27](https://github.com/bangadam/ohmyanime/issues/27) · **Priority:** P2

**Depends on:** None.

**Scope:** Return validated last-known cached metadata on transient upstream failure; label stale/offline state and provide retry. Keep catalogue cache distinct from playback availability.

**Acceptance:**
- [ ] Cached titles remain browsable offline.
- [ ] empty cache shows an honest offline message.
- [ ] reconnect refreshes stale metadata.
- [ ] unavailable streams are not advertised as offline downloads.

### 5.4 Add frontend linting and behavior-focused regression coverage

**Issue:** [#28](https://github.com/bangadam/ohmyanime/issues/28) · **Priority:** P1

**Depends on:** None.

**Scope:** Choose one lint/format tool aligned with the codebase; add component/logic and browser smoke coverage. Integrate with current CI without network-dependent fixtures.

**Acceptance:**
- [ ] CI catches guest/local browse-to-play regressions, movie/series presentation, provider filtering and reader progress once added.
- [ ] tests are deterministic.
- [ ] supported native-host limits are reported separately from browser tests.

## Definition of done

Exercise the implemented path in the actual app, preserve existing local data, cover plausible regressions with deterministic tests, and record platform/provider limits. New providers need parser fixtures and one authorized live smoke; successful homepage fetch alone is not playback verification. Do not publish secrets, private reading/watch history, tokens or signed media URLs.
