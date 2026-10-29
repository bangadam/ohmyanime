// Package provider implements metadata discovery and unofficial stream resolvers.
//
// Metadata is obtained from AniList, not fabricated from stream availability.
// Application anime IDs are anilist:<numeric ID>. Episode IDs append :episode:
// and the unpadded base64url encoding of the original provider episode label.
// Labels are not rounded, renumbered, or matched by slice position. Provider
// slugs remain internal, with unique exact normalized full-title/alias matching;
// normalization retains all season, part, cour and numeral information.
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
