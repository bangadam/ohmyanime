package app

// manga_bookmarks_test.go covers issue #10's HTTP surface: manga bookmarks
// listed beside anime bookmarks without merging identities. All data is
// seeded straight into the store, so no provider is contacted.

import (
	"context"
	"testing"

	"myanime/internal/domain"
	"myanime/internal/store"
)

func TestHTTPMangaBookmarksContract(t *testing.T) {
	c := newContractClient(t)
	ctx := context.Background()
	if err := c.app.db.UpsertManga(ctx, domain.Manga{ID: "mangadex:m", Title: "Same Title", Availability: []string{"mangadex"}}); err != nil {
		t.Fatal(err)
	}
	// The anime namespacing keeps same-title rows distinct from the manga one.
	if err := c.app.db.UpsertAnime(ctx, domain.Anime{ID: "anilist:a", Title: "Same Title", Availability: []string{"sub"}}); err != nil {
		t.Fatal(err)
	}

	c.request("PUT", "/manga/mangadex:m/bookmark", "", 200)
	c.request("PUT", "/bookmarks/anilist:a", "", 200)

	// Each kind lists only its own bookmark; titles never merge identities.
	mangaList := contractData[store.MangaBookmarkPage](t, c.request("GET", "/manga/bookmarks", "", 200))
	if len(mangaList.Items) != 1 || mangaList.Items[0].ID != "mangadex:m" {
		t.Fatalf("manga bookmarks: %+v", mangaList.Items)
	}
	animeList := contractData[store.BookmarkPage](t, c.request("GET", "/bookmarks", "", 200))
	if len(animeList.Items) != 1 || animeList.Items[0].ID != "anilist:a" {
		t.Fatalf("anime bookmarks: %+v", animeList.Items)
	}

	// Removing the manga bookmark leaves the anime bookmark untouched.
	c.request("DELETE", "/manga/mangadex:m/bookmark", "", 204)
	mangaList = contractData[store.MangaBookmarkPage](t, c.request("GET", "/manga/bookmarks", "", 200))
	if len(mangaList.Items) != 0 {
		t.Fatalf("manga bookmark not removed: %+v", mangaList.Items)
	}
	animeList = contractData[store.BookmarkPage](t, c.request("GET", "/bookmarks", "", 200))
	if len(animeList.Items) != 1 {
		t.Fatalf("anime bookmark lost: %+v", animeList.Items)
	}

	// Bookmarking an unknown manga without provider reachability fails.
	c.request("PUT", "/manga/mangadex:missing/bookmark", "", 400)
}
