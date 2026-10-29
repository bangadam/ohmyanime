package desktop

import (
	"context"
	"errors"
	"sync"

	"github.com/egoist/mygo"

	"myanime/internal/updatecheck"
)

// UpdateState is the state of a desktop update flow, reported to the UI.
type UpdateState string

const (
	// UpdateStateUnavailable means updates are not enabled for this build
	// (development builds, or builds without update signing configured).
	UpdateStateUnavailable UpdateState = "unavailable"
	// UpdateStateUpToDate means the check succeeded and found no newer version.
	UpdateStateUpToDate UpdateState = "up-to-date"
	// UpdateStateAvailable means a newer signed version is ready to install.
	UpdateStateAvailable UpdateState = "available"
)

// UpdateStatus is the result of a manual update check.
type UpdateStatus struct {
	State   UpdateState `json:"state"`
	Version string      `json:"version,omitempty"`
	Notes   string      `json:"notes,omitempty"`
	Current string      `json:"current"`
}

// UpdateProgress is one download progress report.
type UpdateProgress struct {
	Downloaded int64 `json:"downloaded"`
	Total      int64 `json:"total"`
}

// Updater is the desktop update service bound to the frontend. It wraps
// mygo's Ed25519-signed updater behind a manual, consent-driven flow:
// the app never checks, downloads, or restarts on its own.
type Updater struct {
	mu      sync.Mutex
	pending *mygo.Update
}

// Status reports whether updates are enabled for this build and, when a
// previous check found one, the pending update.
func (u *Updater) Status() UpdateStatus {
	current := mygo.App.Version()
	if !mygo.Updater.Enabled() {
		return UpdateStatus{State: UpdateStateUnavailable, Current: current}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.pending != nil {
		return UpdateStatus{State: UpdateStateAvailable, Version: u.pending.Version, Notes: u.pending.Notes, Current: current}
	}
	return UpdateStatus{State: UpdateStateUpToDate, Current: current}
}

// CheckForUpdates asks the configured update feed for a newer signed
// version. It returns the found update or an up-to-date state; transport
// failures are returned as errors so the UI can offer a retry.
func (u *Updater) CheckForUpdates() (UpdateStatus, error) {
	if !mygo.Updater.Enabled() {
		return UpdateStatus{State: UpdateStateUnavailable, Current: mygo.App.Version()}, nil
	}
	update, err := mygo.Updater.Check(context.Background())
	if err != nil {
		if errors.Is(err, mygo.ErrUpdatesDisabled) {
			return UpdateStatus{State: UpdateStateUnavailable, Current: mygo.App.Version()}, nil
		}
		return UpdateStatus{Current: mygo.App.Version()}, err
	}
	u.mu.Lock()
	u.pending = update
	u.mu.Unlock()
	if update == nil {
		return UpdateStatus{State: UpdateStateUpToDate, Current: mygo.App.Version()}, nil
	}
	return UpdateStatus{State: UpdateStateAvailable, Version: update.Version, Notes: update.Notes, Current: mygo.App.Version()}, nil
}

// Install downloads and installs the pending update, streaming download
// progress on the channel. The installed version keeps running until
// Relaunch is called with the user's consent.
func (u *Updater) Install(progress *mygo.Channel[UpdateProgress]) error {
	u.mu.Lock()
	pending := u.pending
	u.mu.Unlock()
	if pending == nil {
		return errors.New("no update has been found; check for updates first")
	}
	return pending.Install(context.Background(), func(downloaded, total int64) {
		if progress != nil {
			_ = progress.Send(UpdateProgress{Downloaded: downloaded, Total: total})
		}
	})
}

// Relaunch quits the app and starts the installed version. Call it only
// after Install succeeded and the user consented to the restart.
func (u *Updater) Relaunch() {
	u.mu.Lock()
	u.pending = nil
	u.mu.Unlock()
	mygo.App.Relaunch()
}

// VerifyUpdateFeed validates signed update-manifest metadata for the
// running version without installing anything; it backs deterministic
// tests of the manifest policy (incomplete and tampered metadata must be
// rejected). The archive signature itself is checked by the updater
// against the pinned public key during download.
func VerifyUpdateFeed(body []byte, current string) error {
	return updatecheck.VerifyManifest(body, current)
}
