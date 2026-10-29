# OhMyAnime

A personal desktop streaming app: browse a catalogue, track what you watch, and
play episodes in an immersive player. Go backend, React frontend, and a native
desktop build that ships as a single binary.

No account or sign-in is needed. Playback, saved list, history, and preferences
are available immediately and use one shared local database per instance.

![Home](assets/home.webp)

## Features

- **Catalogue** — popular anime with filters for airing status, type, multiple
  genres and tags, and sorting by A-Z, newest, recently updated, or popularity;
  seasonal browsing with year filters, search, a weekly release schedule in your
  local timezone, and genre and tag chips on every title page.
- **Title pages** — real metadata, episode inventory with actual episode titles,
  and per-episode provider availability.
- **Player** — custom overlay on hls.js with quality, speed, audio and subtitle
  tracks, episode queue, skip intro/outro, and WebVTT import. Native HLS is used
  where the browser supports it.
- **Library** — saved list, watch history with resume, and progress that survives
  restarts.
- **Appearance** — Dark, Light, or System, following the OS while in System mode.
- **Desktop** — one binary, private loopback backend, native window.

![Title details](assets/details.webp)

## How it works

```
Browser / desktop webview
        │  REST + HLS
        ▼
Go server ── metadata + episodes ──► public catalogue API
   │
   ├── provider adapters ──► episode sources
   ├── media proxy (capability-scoped HLS)
   └── SQLite (library, history, settings)
```

The server never hands the browser an upstream URL. A playback session mints
capability-scoped local paths, and the proxy streams bytes with range support.

## Requirements

- Go **1.27.3+**
- [Bun](https://bun.sh) for the frontend
- Desktop: macOS WKWebView, Windows WebView2, or Linux GTK3/WebKitGTK with media
  codecs

## Setup

### 1. Configure (optional)

```sh
cp .env.example .env
```

No account credentials are required. The defaults listen only on localhost.
Use `.env` to customize development origins or storage.

### 2. Run

```sh
# terminal 1
export $(grep -v '^#' .env | xargs)
MYANIME_DEV=1 MYANIME_DEV_ORIGIN=http://localhost:5173 go run ./cmd/server

# terminal 2
cd web
bun install
bun run dev
```

Open <http://localhost:5173>.

## Desktop build

```sh
cd web && bun run build
cd .. && go build -tags release -trimpath -o bin/myanime-desktop ./cmd/desktop
./bin/myanime-desktop
```

The `release` tag embeds `web/dist`, so the binary needs no asset directory and
no dev server.

### Download desktop apps

Download platform files from [GitHub Releases](https://github.com/bangadam/ohmyanime/releases/latest).

| Platform | Architecture | Package and requirements |
|---|---|---|
| macOS 12+ | Apple Silicon and Intel | Universal `.dmg`; drag the app to Applications |
| Windows 10/11 | x64 or ARM64 | Portable `.zip`; extract and run `OhMyAnime.exe`. Requires Microsoft Edge WebView2 Runtime (included with Windows 11) |
| Linux | x64 or ARM64 | `.deb` or `.tar.gz` with `install.sh`; requires GTK 3, WebKitGTK 4.1, and GStreamer media codecs |

On Debian/Ubuntu, install the matching `.deb` with `sudo apt install ./ohmyanime_1.0.0_amd64.deb`
(use the `arm64` file on ARM64). This also installs declared runtime dependencies.
For other distributions, install GTK 3, WebKitGTK 4.1, and GStreamer good/bad/libav
plugins with your package manager before using the archive's `install.sh`.

Release files include `SHA256SUMS` for integrity verification. macOS packages are
ad-hoc signed, not Apple-notarized; Windows binaries are unsigned. Your operating
system may require explicit approval before opening them.

### Package all supported platforms

After building the frontend above, run:

```sh
GOFLAGS=-tags=release go run github.com/egoist/mygo/cmd/mygo@v0.7.1 build \
  -platform darwin/universal,windows/amd64,windows/arm64,linux/amd64,linux/arm64
```

`mygo.json` pins the app identity, version, and runtime package requirements.
Packages are written to `bin/release`. Run on macOS to create and sign the DMG;
Windows installers additionally need NSIS. Without NSIS, distribute the generated
Windows executable and LICENSE together in a portable ZIP.

### App updates (manual checks)

The desktop app has a manual update check in **Preferences → Updates**.
Checking, downloading, and restarting are each explicit user actions; nothing
happens automatically, and a restart is only offered after consent (with an
extra warning while something is playing). Update archives must be signed
with the publisher's Ed25519 key and are verified before installation; a
failed or interrupted download leaves the running version untouched, and
library data lives outside the app bundle so it survives updates.

Signed update builds are only produced when `mygo build` runs with an update
key and a configured update feed (`updates` in `mygo.json`, see the
[mygo updates documentation](https://github.com/egoist/mygo/blob/main/docs/updates.md)).
**Not verified locally:** no release has been published with a real update
server yet, so the end-to-end check-download-restart path has only been
exercised through deterministic tests (`internal/updatecheck`, and the
Preferences update-flow vitest suite), not against a live feed or a packaged,
notarized build. Development builds report updates as unavailable by design.

### App icon

The app icon (`assets/icon.svg`, packaged as `resources/icon.png`, wired via
the `icon` field in `mygo.json`) is original geometric artwork owned by the
project author (bangadam), released under the repository's MIT license. It
contains no third-party anime artwork, characters, or trademarks. `mygo build`
derives the macOS `.icns`, the Windows executable icon, and the Linux desktop
entry icon from the square 1024×1024 source PNG.

## Access model

The app opens directly without login. Library, history, progress, and preferences
are shared by everyone who can reach the same instance. Existing local data is
preserved; removing sign-in does not reset it.

Web mutations require a matching Origin and a CSRF token from `/api/v1/capabilities`.
The desktop shell supplies its private bootstrap bearer token directly to its
loopback API. These request protections are not a web account access restriction.

## Serving it to another device

Optional. The app is built for local desktop use; if you also want it on your
phone or another machine, put it behind a TLS reverse proxy and set:

```sh
MYANIME_PUBLIC_ORIGIN=https://your-host
MYANIME_TRUSTED_PROXIES=10.0.0.0/8
MYANIME_DATA_DIR=/var/lib/myanime
```

Non-loopback listen additionally requires `MYANIME_BEHIND_TLS_PROXY=1`. Ensure the
proxy supports byte ranges and streaming without whole-response buffering — video
traffic passes through the server.

There is no built-in account access control. Keep the server local, or restrict
access at your reverse proxy before making it reachable by other people.

## Development

```sh
go test ./...          # backend
go vet ./...
cd web && bun run typecheck
```

The API contract is in [openapi.yaml](openapi.yaml).

## Configuration

| Variable | Purpose |
|---|---|
| `MYANIME_DATA_DIR` | SQLite and cache location |
| `MYANIME_PUBLIC_ORIGIN` | Exact HTTPS origin in production |
| `MYANIME_TRUSTED_PROXIES` | CIDRs allowed to set `X-Forwarded-Proto` |
| `MYANIME_BEHIND_TLS_PROXY` | Required for non-loopback listen |
| `MYANIME_DEV`, `MYANIME_DEV_ORIGIN` | Local Vite origin |

## Notes and limits

- Single local library per instance; no account separation or login sessions.
- Provider integrations are unofficial; upstream layouts and availability change
  without notice, and a title may exist in one provider but not another.
- Public releases are not Apple-notarized or Windows code-signed. Publishing
  trusted signed releases requires your own signing and notarization credentials.
- Review provider terms and content rights before exposing an instance publicly.

## Roadmap

Next steps — provider architecture, manga, desktop integration — are in
[ROADMAP.md](ROADMAP.md).

## License

MIT — see [LICENSE](LICENSE).
