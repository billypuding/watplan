# WATplan

**[watplan.me](https://watplan.me)**

Darmowy plan zajęć dla wszystkich wydziałów Wojskowej Akademii Technicznej (WAT). Bez logowania, bez reklam. Wpisz kod grupy, np. `WCY26IY1S1`, i od razu widzisz swój plan.

A free, open-source class timetable for students of the Military University of Technology (WAT) in Warsaw. It covers WCY, WEL, WIG, WIM, WLO, WML, WTC and IOE, about 790 groups in total.

WAT publishes class schedules as static plansoft HTML pages, which are hard to read on a phone. WATplan turns them into a clean, phone-friendly timetable.

## Features

- Day, week and month views; move between days, weeks and months with arrows, the keyboard or a swipe
- Each class shows its type, room and teacher, with a colour for each subject and breaks of 30 minutes or more marked
- A progress bar on the class that is happening now, with the time left; finished classes are dimmed (both can be turned off in settings)
- Tap a class to see every session of that subject: which one it is (e.g. 6/15), how many are left and when the last one is
- Search across all groups, plus favourites for switching between groups quickly
- Export the timetable to your calendar as an `.ics` file
- Remembers your group and opens it straight away; recently opened plans are shown instantly from the phone's storage and stay visible if the server cannot be reached
- Light, dark and automatic theme
- Checks WAT for schedule changes in the background, with a manual refresh button and a notice when the plan is older than a day
- No login, no ads, free and open source

## How it works

1. Fetches the schedule page for a given group from the WAT plansoft server
2. Parses the HTML into JSON
3. A Go server renders the weekly view
4. A background check detects changes in the source schedule and updates the data

Static files are embedded in the binary. At startup the server replaces every
`?v=...` in links to local files (HTML, CSS, JS, `manifest.json`, `sw.js`) with a hash
of that file's content, and names the service worker cache after a hash of all
files. Write new links as `/file.js?v=0`; there is no version to bump by hand.

With `PLANWAT_CANONICAL_HOST=watplan.me`, every other host name (for example
`www.watplan.me` or `*.onrender.com`) gets a `301` to `https://watplan.me` with the
same path and query. `/api/` and `/sw.js` are left alone for older installs, and so
are `localhost` and bare IP addresses (health checks).

Every response carries `X-Content-Type-Options: nosniff`,
`Referrer-Policy: strict-origin-when-cross-origin`, a `Permissions-Policy` that turns off
unused features (camera, microphone, geolocation, payment and so on),
`Strict-Transport-Security: max-age=86400` and a `Content-Security-Policy` that allows only
the site itself, umami (`cloud.umami.is`, `gateway.umami.is`) and no framing
(`frame-ancestors 'none'`). Inline scripts are allowed by their `sha256` hashes, which
`assets.go` computes at startup from the served HTML, so there is no `'unsafe-inline'`.

Cache headers are set so that a CDN in front of the server (e.g. Cloudflare) only
keeps what is safe to share:

- versioned files (`?v=`): `public, max-age=31536000, immutable`,
- HTML (`/`, `/app`, `/o-watplan`): `public, max-age=0, must-revalidate` with an `ETag`,
- `sw.js`: `no-cache`,
- everything under `/api/`: `private, no-store`, or `private, no-cache` with an `ETag`
  for `/api/plan` and `/api/plans`; compressed responses carry `Vary: Accept-Encoding`.

## Getting started

```bash
git clone https://github.com/billypuding/watplan.git
cd watplan
go run cmd/watplan/main.go
```

Then open `http://127.0.0.1:8080` in your browser.

## When WAT blocks your IP

WAT protects its schedule servers with Incapsula. Too many requests can trigger an
`Access denied` / `Error 17` page for the whole IP. The application never tries to
bypass that block; it stops fetching and keeps using the plans already saved in
`data/`.

To run entirely from the cache (no requests to WAT at all):

```bash
go run cmd/watplan/main.go -offline
# or: PLANWAT_OFFLINE=1 go run cmd/watplan/main.go
```

If you need a plan that is not in the cache, open the plan page in a browser on a
different network (e.g. a phone hotspot), save the HTML file, and import it:

```bash
go run cmd/watplan/main.go -offline -import WEL24EX1S0=~/Downloads/WEL24EX1S0.htm
```

`-import` can be repeated. The full ID must already exist in `plans.json`.

Do not restart the app in a loop while the block is active. Wait for it to expire or
change the public IP; repeated requests only make the block last longer.

### Scraping API and browser-fingerprint proxy

If WAT blocks the hosting IP and proxies, WATplan can fetch through a scraping API.
The API does the outbound request and returns the plan HTML:

```bash
PLANWAT_SCRAPER_PROVIDER=scrapingant
PLANWAT_SCRAPER_KEY=...
PLANWAT_SCRAPER_PROXY_TYPE=datacenter
PLANWAT_SCRAPER_COUNTRY=PL
PLANWAT_SCRAPER_BROWSER=false
```

Supported providers: `scrapingant`, `scrapingbee`, `scraperapi`. Background
refreshes use the scraping API.

Manual refreshes can use a browser-fingerprint Go client through an HTTP proxy:

```bash
PLANWAT_PROXY_HOST=pr.oxylabs.io:7777
PLANWAT_PROXY_USER=customer-...
PLANWAT_PROXY_PASS=...
```

The client uses a Chrome profile from `tls-client`, one shared cookie jar, and a
sticky proxy session. On `403`, `407`, or timeout it generates a new session ID and
retries once. If the proxy still fails, the refresh falls back to the scraping API.
The proxy login is built as
`customer-<user>-cc-PL-sessid-<id>-sesstime-1440` (24 hours, Oxylabs' maximum).

Opening a plan always returns the cached copy right away. The server then checks
it in the background through the scraping API if the last check (`checked_at`,
updated on `200` and `304`) is older than an interval that depends on when the
content last changed (`modified_at`):

- changed in the last 24 hours: every 10 minutes,
- unchanged for 1 to 7 days: every hour,
- unchanged for more than 7 days: every 4 hours.

Each plan is checked at most once per interval and never while another fetch of
that plan is running.
The response then carries `X-Plan-Refresh: background` (otherwise `none`), and the
app polls quietly after 2, 5, 10 and 20 seconds, swapping the plan in place and
showing "Plan zaktualizowany" only if the classes changed.

A manual refresh also returns the cached plan immediately, with `X-Plan-Refresh`:

- `fresh`: checked in the last 15 minutes, nothing is fetched (`X-Plan-Checked-At` gives the time),
- `pending`: a fetch of this plan is already running, the proxy is not used,
- `cooldown`: refreshed less than a minute ago,
- `background`: fetched through the proxy, falling back to the scraping API.

The intervals are `CheckIntervals` in `internal/model/check.go` (also used for plans watched through push notifications) and the 15 minute window is `manualFreshFor` in
`internal/server/server.go`.

ScrapingAnt's free plan includes 10,000 credits a month; a plain datacenter request
costs 1 credit, while residential costs 25. Use `PLANWAT_ON_DEMAND=1` and
`PLANWAT_NO_BACKGROUND=1` to avoid spending credits on bulk refreshes.

For tests, `PLANWAT_SCRAPER_API_URL` overrides the provider endpoint.

## Hosting on Render (or another cloud)

The server does the fetching, so the phone's network does not matter: WAT sees the
hosting provider's egress IP. Fetching ~580 plans in the background from a shared
cloud IP is a good way to get that IP blocked.

Use native Go on Render. Set the proxy variables above and keep the scraping API
for background refreshes. No credentials belong in the repository.

For a hosted instance, use on-demand mode. It serves the plans restored from Turso
(see below) and fetches only the plan a user actually opens:

```bash
PLANWAT_ON_DEMAND=1 PLANWAT_WORKERS=1 PLANWAT_FETCH_INTERVAL=2s go run cmd/watplan/main.go
```

In on-demand mode a slow background loop also runs. Once a day it reads each
faculty's group list from `sources.json`, adds new groups and reports a moved list
(404) in `/api/status`. Every 30 minutes it fetches plans that have no data yet,
one every 90 seconds, skipping hosts that are blocked. Refreshes send `ETag` and
`If-Modified-Since`, so an unchanged page costs a `304` with no body. Set
`PLANWAT_NO_BACKGROUND=1` to turn the loop off.

On Render, set those as environment variables. If the Render IP is already blocked,
on-demand mode alone will not unblock it. In that case:

- create the service in a different Render region to get a new egress IP (and keep
  on-demand mode enabled), or
- set `PLANWAT_PROXY_HOST`, `PLANWAT_PROXY_USER`, and `PLANWAT_PROXY_PASS` to a
  proxy with a clean IP, or
- move fetching to a machine on a network that WAT allows; with `PLANWAT_DB_URL`
  set there too, the plans it fetches land in Turso and the hosted server picks
  them up on its next start.

### Push notifications about plan changes

When the server notices a change in a plan (moved, cancelled or new classes, a
new room), it saves it to `data/<ID>.history.json` (shown in the app under
"Zmiany w planie") and sends a Web Push notification to everyone who turned
notifications on for that group. Plans with subscribers are rechecked every
2 hours in the background loop.

Generate the keys once and set them as environment variables:

```bash
go run cmd/watplan/main.go -vapid-keys
```

- `PLANWAT_VAPID_PUBLIC`, `PLANWAT_VAPID_PRIVATE`: the generated keys. Without
  them notifications are off and the switch is hidden in the app.
- `PLANWAT_VAPID_SUBJECT`: contact for push services (default `mailto:awalczak@duck.com`).
- `PLANWAT_PUSH_FILE`: where subscriptions are stored (default `data/push.json`).

The app re-sends its subscription every time it opens, so a lost
`push.json` heals itself, but to keep subscriptions and change history across
deploys put `PLANWAT_PUSH_FILE` and `PLANWAT_DATA` on a persistent disk.

### Alerts about broken fetching

Set `PLANWAT_ALERT_WEBHOOK` to a Discord channel webhook URL (Server settings →
Integrations → Webhooks → New Webhook → Copy URL). The server posts a message
when something breaks and another when it recovers: the proxy rejecting
connections (for example `402 Payment Required`), WAT's firewall blocking a
faculty host, a faculty's group list failing or disappearing, and writes to
Turso failing. An unchanged problem is repeated at most every 6 hours. Summer
lists that are not published yet do not alert.

### Keeping data across deploys (Turso)

Render wipes the disk on every deploy. To keep plans, change history and push
subscriptions, create a free Turso database and set:

- `PLANWAT_DB_URL`: the database URL, e.g. `libsql://watplan-<name>.turso.io`
- `PLANWAT_DB_TOKEN`: a database token

On start the server restores everything from the database (newer plans win),
uploads local plans the database does not have yet, and from then on mirrors
every write in the background. `/api/status` shows the state under `db`.

Daily budgets cap outgoing requests over a rolling 24 hours:
`PLANWAT_BUDGET_SCRAPER_DAILY` (default 400) and `PLANWAT_BUDGET_PROXY_DAILY` (default 300),
`0` turns a budget off. When a budget is used up the server stops fetching that way, keeps
serving cached plans and sends one alert; `/api/refresh` answers `X-Plan-Refresh: budget`.

Per-IP limits (per hour) protect the budgets: `PLANWAT_LIMIT_REFRESH_PER_HOUR` (default 10)
for `/api/refresh` and `PLANWAT_LIMIT_CHECKS_PER_HOUR` (default 30) for checks started by
opening a plan; `-1` turns a limit off. Over the limit, refresh answers `X-Plan-Refresh: limited`
and opening a plan just shows the cached copy. The client IP is taken from `CF-Connecting-IP`
only when the request really came from Cloudflare: either the peer is in Cloudflare's IP
ranges, or the peer is a private address and `PLANWAT_TRUST_PROXY=1` (Render, where all
traffic goes through Cloudflare). Otherwise the TCP peer address is used.

`GET /healthz` answers `200` or `503` with `{"ok": ..., "checks": {"data", "db", "fetch", "budget"}}`
and nothing sensitive:

- `data`: plans restored from the database and present on disk,
- `db`: Turso reachable (no pending write error; required when `PLANWAT_DB_URL` is set),
- `fetch`: fetching from WAT is not failing: it only turns false when fetches keep failing and
  the last success is older than `PLANWAT_HEALTH_FETCH_MAX_AGE` (default `3h`), so a quiet
  night without any fetches is still healthy,
- `budget`: the daily fetch budget is not used up.

With ScrapingAnt, the server reads the remaining credits from its free `/v2/usage` endpoint
every `PLANWAT_CREDITS_CHECK_EVERY` (default `4h`) and sends one alert per threshold from
`PLANWAT_CREDITS_ALERT` (percent of the plan, default `30,10`) to the alert webhook. After a
top-up the alerts are marked as resolved and the thresholds work again.

`/api/status` returns only `mode` and `now` unless the request carries
`Authorization: Bearer <PLANWAT_STATUS_TOKEN>`; without the variable nobody gets the details.
The detailed view adds the proxy, blocked hosts, database state, usage, budgets, credits and
the number of push subscriptions:

```bash
curl -s -H "Authorization: Bearer $PLANWAT_STATUS_TOKEN" https://watplan.me/api/status
```

Every request to the scraping API and to the proxy is logged (`zużycie: ...`) and counted.
With `Authorization: Bearer $PLANWAT_STATUS_TOKEN`, `/api/status` shows the counts under `usage`, per provider, for the last 24 hours and
30 days: requests, ScrapingAnt credits (from the `Ant-Credits-Cost` header) and proxy
bytes. The counters are stored in Turso, so they survive restarts.
Without these variables the server works on local files only.

Note that Render's filesystem is ephemeral: plans fetched at runtime disappear on
the next deploy or restart. Turso is the permanent store; `data/` is only a local
cache and is not committed. To get the plans locally, run once with
`PLANWAT_DB_URL` and `PLANWAT_DB_TOKEN` set: the server restores them into `data/`.


### Cloudflare in front of Render

The `watplan.me` zone is on Cloudflare, in front of Render:

- DNS: both the apex `watplan.me` and `www` are `CNAME` records pointing to
  `watplan.onrender.com`, **Proxied**. With an `A` record or "DNS only" the traffic goes
  straight to Render and the zone (cache rules, settings) is skipped.
- SSL/TLS: **Full (strict)**. Flexible causes a redirect loop, because Render forces HTTPS.
- Cache Rule "cache static": requests whose path ends in `js`, `css`, `woff2`, `png`,
  `svg` or `ico` are *Eligible for cache*, with Edge TTL taken from the origin's
  `Cache-Control`. Only `?v=` files are `immutable`; HTML and `/api/` stay uncached
  because of their own headers (see above).
- Rocket Loader and Email Obfuscation are off: both rewrite the HTML, which breaks the
  inline script hashes in the CSP.

Check: `curl -sI "https://watplan.me$(curl -s https://watplan.me/app | grep -o '/app.js?v=[0-9a-f]*')" | grep -i cf-cache-status`
should show `HIT` on the second request.

## Tests

```bash
go test ./...
node --test e2e/*.test.mjs
```

The end-to-end tests build the server, run it offline on the plans in
`e2e/fixtures`, and drive a headless Chrome over the DevTools protocol with the
clock frozen at 19 November 2026. They need Node 22+ and Chrome; set
`CHROME_PATH` if Chrome is not in the default location. No npm packages are used.
`E2E_CHROME_FLAGS` passes extra Chrome flags, e.g.
`E2E_CHROME_FLAGS="--disable-features=WebBluetooth"` to mimic Chrome on the Linux CI
runner. CSP violations fail the tests; "Unrecognized feature" messages about
`Permissions-Policy` depend on the Chrome build and are only reported.

## Disclaimer

WATplan is an independent student project, not affiliated with the Military University of Technology. The official schedule is always the source of truth.

## License

MIT
