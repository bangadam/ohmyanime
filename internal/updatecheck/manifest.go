package updatecheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// MaxManifestSize bounds an accepted update manifest, mirroring mygo's
// updater limit.
const MaxManifestSize = 1 << 20

// Manifest is the update metadata an update feed serves: the version, the
// archive it downloads, and the Ed25519 signature mygo's updater checks
// the downloaded archive against. VerifyManifest enforces the acceptance
// policy; the signature itself is verified against the pinned public key
// when the archive is downloaded, by mygo.Updater.
type Manifest struct {
	Version   string `json:"version"`
	URL       string `json:"url"`
	Signature string `json:"signature"`
	Size      int64  `json:"size"`
	Notes     string `json:"notes,omitempty"`
	Date      string `json:"date,omitempty"`
	Sha256    string `json:"sha256,omitempty"`
}

// VerifyManifest applies the update-metadata acceptance policy to a
// manifest body: it must parse as JSON within MaxManifestSize, carry a
// well-formed version, an HTTPS URL, a non-empty signature, and, when the
// current version is given, describe a strictly newer version. Tampered,
// incomplete, downgrade, or replayed (equal-version) metadata is rejected.
func VerifyManifest(body []byte, current string) error {
	if len(body) == 0 {
		return errors.New("updatecheck: empty manifest")
	}
	if len(body) > MaxManifestSize {
		return fmt.Errorf("updatecheck: manifest is %d bytes, over the %d limit", len(body), MaxManifestSize)
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return fmt.Errorf("updatecheck: manifest is not valid JSON: %w", err)
	}
	if strings.TrimSpace(m.Version) == "" {
		return errors.New("updatecheck: manifest has no version")
	}
	if _, _, err := parse(m.Version); err != nil {
		return err
	}
	if strings.TrimSpace(m.Signature) == "" {
		return errors.New("updatecheck: manifest has no signature")
	}
	u, err := url.Parse(m.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("updatecheck: manifest URL %q is not a valid HTTPS URL", m.URL)
	}
	if m.Size < 0 {
		return fmt.Errorf("updatecheck: manifest size %d is negative", m.Size)
	}
	if current != "" {
		if c := Compare(m.Version, current); c <= 0 {
			return fmt.Errorf("updatecheck: manifest version %s is not newer than %s", m.Version, current)
		}
	}
	return nil
}
