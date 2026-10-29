package updatecheck

import "testing"

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.2.0", "1.10.0", -1}, // numeric, not lexicographic
		{"1.10.0", "1.9.9", 1},  //
		{"2.0.0", "1.99.99", 1}, //
		{"1.0", "1.0.0", 0},     // missing segments are zero
		{"1.0.0", "1.0", 0},     //
		{"1.0.0-beta.1", "1.0.0", -1},
		{"1.0.0", "1.0.0-beta.1", 1},
		{"1.0.0-beta.1", "1.0.0-beta.2", -1},
		{"1.0.0-beta.2", "1.0.0-beta.10", -1},
		{"1.0.0-beta.1", "1.0.0-rc.1", -1}, // alphanumeric after equal prefix
		{"1.0.0-rc.1", "1.0.0-beta.1", 1},  //
		{"1.0.0-beta", "1.0.0-beta.1", -1}, // longer prerelease is greater
		// Invalid candidates are conservative: treated as older, never offered.
		{"", "1.0.0", -1},
		{"not-a-version", "1.0.0", -1},
		{"1.0.0-", "1.0.0", -1},
		{"1.0.x", "1.0.0", -1},
		{"1..0", "1.0.0", -1},
		{"1.0.0.0", "1.0.0", -1},
		{"1.0.0", "", 1},
		{"1.0.0", "garbage", 1},
		{"garbage", "garbage", 0},
	}
	for _, tc := range cases {
		if got := Compare(tc.a, tc.b); got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestVerifyManifest(t *testing.T) {
	valid := `{"version":"1.1.0","url":"https://downloads.example.com/ohmyanime-1.1.0-darwin-arm64.tar.gz","signature":"c2ln","size":123}`
	if err := VerifyManifest([]byte(valid), "1.0.0"); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	bad := []struct {
		name    string
		body    string
		current string
	}{
		{"empty", "", "1.0.0"},
		{"not json", "}{", "1.0.0"},
		{"no version", `{"url":"https://x/y","signature":"s"}`, "1.0.0"},
		{"bad version", `{"version":"1.x","url":"https://x/y","signature":"s"}`, "1.0.0"},
		{"no signature", `{"version":"1.1.0","url":"https://x/y"}`, "1.0.0"},
		{"no url", `{"version":"1.1.0","signature":"s"}`, "1.0.0"},
		{"http url", `{"version":"1.1.0","url":"http://x/y","signature":"s"}`, "1.0.0"},
		{"url no host", `{"version":"1.1.0","url":"https:///y","signature":"s"}`, "1.0.0"},
		{"negative size", `{"version":"1.1.0","url":"https://x/y","signature":"s","size":-1}`, "1.0.0"},
		{"downgrade", valid, "1.2.0"},
		{"replay equal version", valid, "1.1.0"},
	}
	for _, tc := range bad {
		if err := VerifyManifest([]byte(tc.body), tc.current); err == nil {
			t.Errorf("%s: manifest accepted, want rejection", tc.name)
		}
	}
}

func TestVerifyManifestSizeLimit(t *testing.T) {
	body := []byte(`{"version":"1.1.0","url":"https://x/y","signature":"` + string(make([]byte, MaxManifestSize)) + `"}`)
	if err := VerifyManifest(body, "1.0.0"); err == nil {
		t.Error("oversized manifest accepted, want rejection")
	}
}
