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

You can also route traffic through a working proxy:

```bash
PLANWAT_PROXY=http://user:pass@host:port go run cmd/watplan/main.go
```

`PLANWAT_PROXY_BACKUP` takes a second proxy in the same format. The server switches
to it when the main proxy rejects the connection (`402 Payment Required`, `407`,
no connection), times out, or when WAT blocks the main proxy's IP (`403`/Incapsula).
It goes back to the main proxy after `PLANWAT_PROXY_BLOCK_BACKOFF` (default `30m`)
and pauses fetching only when neither proxy can get through. If both proxies are
blocked by WAT, the domain is paused for `PLANWAT_BLOCK_BACKOFF` (default `1h`).

Do not restart the app in a loop while the block is active. Wait for it to expire or
change the public IP; repeated requests only make the block last longer.

## Hosting on Render (or another cloud)

The server does the fetching, so the phone's network does not matter: WAT sees the
hosting provider's egress IP. Fetching ~580 plans in the background from a shared
cloud IP is a good way to get that IP blocked.

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
- set `PLANWAT_PROXY` to a proxy with a clean IP, or
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
Without these variables the server works on local files only.

Note that Render's filesystem is ephemeral: plans fetched at runtime disappear on
the next deploy or restart. Turso is the permanent store; `data/` is only a local
cache and is not committed. To get the plans locally, run once with
`PLANWAT_DB_URL` and `PLANWAT_DB_TOKEN` set: the server restores them into `data/`.


## Tests

```bash
go test ./...
node --test e2e/*.test.mjs
```

The end-to-end tests build the server, run it offline on the plans in
`e2e/fixtures`, and drive a headless Chrome over the DevTools protocol with the
clock frozen at 19 November 2026. They need Node 22+ and Chrome; set
`CHROME_PATH` if Chrome is not in the default location. No npm packages are used.

## Disclaimer

WATplan is an independent student project, not affiliated with the Military University of Technology. The official schedule is always the source of truth.

## License

MIT
