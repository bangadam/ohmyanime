# OhMyAnime

A self-hosted anime streaming app: browse a catalogue, track what you watch, and
play episodes in an immersive player. Go backend, React frontend, and a native
desktop build that ships as a single binary.

No account or sign-in is needed. Playback, saved list, history, and preferences
are available immediately and use one shared local database per instance.

![Home](assets/home.webp)

## Features

- **Catalogue** — popular anime, seasonal browsing with genre and year filters,
  search, and a weekly release schedule in your local timezone.
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

## Access model

The app opens directly without login. Library, history, progress, and preferences
are shared by everyone who can reach the same instance. Existing local data is
preserved; removing sign-in does not reset it.

Web mutations require a matching Origin and a CSRF token from `/api/v1/capabilities`.
The desktop shell supplies its private bootstrap bearer token directly to its
loopback API. These request protections are not a web account access restriction.

## Deployment

Use a TLS reverse proxy and set:

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
- The binary is unsigned. Distributing it requires your own signing and
  notarization credentials.
- Review provider terms and content rights before exposing an instance publicly.

## License

MIT — see [LICENSE](LICENSE).
