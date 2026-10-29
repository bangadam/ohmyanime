// Package provider implements metadata discovery and unofficial stream resolvers.
//
// Metadata is obtained from AniList, not fabricated from stream availability.
// Application anime IDs are anilist:<numeric ID>. Episode IDs append :episode:
// and the unpadded base64url encoding of the original provider episode label.
// Labels are not rounded, renumbered, or matched by slice position. Provider
// slugs remain internal, with unique exact normalized full-title/alias matching;
// normalization retains all season, part, cour and numeral information.
//
// Sources implement the AnimeProvider or MangaProvider contracts, which share
// one provider identity (id, name, kind), and register in-process in a
// registry: adding an adapter is a registration call, never an edit to a
// resolver branch. Caching, cancellation, enablement and explicit provider
// selection stay in Service orchestration.

// Provider-level capabilities (media kinds, audio modes, and reader formats
// for manga) are typed declarations on the adapter contracts, strictly
// separate from episode/chapter availability and from the subtitle/audio
// tracks a playback session discovers. An undeclared capability is never
// inferred or offered, and a declared dub mode never promises any specific
// title has one.
//
// Episode titles come from one AniZip mappings request per title, keyed by
// AniList ID and matched to provider labels by exact number (en, then x-jat,
// then ja). Provider anchor text that only restates the number is discarded so
// the interface never renders "Episode 5" twice. Missing metadata is not an
// error: labels alone remain a valid list.
//
// Resolve accepts only sub/dub. An explicit provider never changes provider or
// language. Auto attempts enabled HiAnime, Otakudesu, then AniChan, with failures
// cached per provider+canonical episode+mode for twenty seconds, successes for
// two minutes, and explicit Refresh invalidation. AniChan exposes only its HLS
// servers and refreshes an expired/401 session at most once per resolve.
//
// Missing skip intervals are optionally enriched from AniSkip using AniList's
// MAL identity, never title guessing. The lookup has a shared 1.8 second deadline
// and cannot fail playback; decimal/special labels are not rounded for AniSkip.
// Catalog planned counts are not accepted as AniChan playback availability.
//
// Manga reading resolves through MangaProvider adapters. MangaDex serves its
// documented public JSON API without authentication: manga search and detail,
// an English-chapter feed, and at-home page URLs. Application manga IDs are
// mangadex:<uuid>; chapter identity uses domain.ChapterKey (volume, decimal
// number, special, language) so "12" and "12.5" never collide. Numberless
// releases carry a synthetic "oneshot" special. Chapters with no hosted pages
// (official external links) are dropped rather than zeroed. Covers are
// upstream CDN thumbnails; page URLs come from one at-home server response
// and are size/count bounded like every other upstream payload.
//
// Close releases idle HTTP connections during application shutdown.
// Subtitle language uses explicit label/code mappings, preferring recognized
// human labels over misleading CDN language fields. Unknown language is und;
// the default is Indonesian, then English, then the first subtitle track.
//
// Megaplay AES-CBC requires valid block lengths and PKCS#7 padding. Putarin and
// AniChan AES-GCM require valid authentication tags. Legacy XOR configuration is
// parsed as JSON, never evaluated as JavaScript. All manifests are size limited,
// signature checked and one variant is probed. TLS verification is enabled;
// redirects and DNS results are checked before connections to prevent private
// network access. Provider permission and current live endpoint availability
// remain release gates, not claims made by a successful fixture test.
package provider
