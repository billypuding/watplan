# PlanWat

A free, open-source weekly timetable viewer for students of the Military University of Technology (WAT) in Warsaw.

WAT publishes class schedules as static plansoft HTML pages, which are hard to read, especially on a phone. PlanWat scrapes that page and shows it as a clean, colour-coded weekly calendar.

## Features

- Weekly grid with an hours column and a colour for each subject
- Switch between weeks without reloading the page
- Automatically refreshes when WAT changes the schedule
- Free and open source

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

Do not restart the app in a loop while the block is active. Wait for it to expire or
change the public IP; repeated requests only make the block last longer.

## Hosting on Render (or another cloud)

The server does the fetching, so the phone's network does not matter: WAT sees the
hosting provider's egress IP. Fetching ~580 plans in the background from a shared
cloud IP is a good way to get that IP blocked.

For a hosted instance, use on-demand mode. It serves the plans committed in `data/`
and fetches only the plan a user actually opens:

```bash
PLANWAT_ON_DEMAND=1 PLANWAT_WORKERS=1 PLANWAT_FETCH_INTERVAL=2s go run cmd/watplan/main.go
```

On Render, set those as environment variables. If the Render IP is already blocked,
on-demand mode alone will not unblock it. In that case:

- create the service in a different Render region to get a new egress IP (and keep
  on-demand mode enabled), or
- set `PLANWAT_PROXY` to a proxy with a clean IP, or
- move fetching to a machine on a network that WAT allows and publish the updated
  `data/` directory to the hosting provider.

Note that Render's filesystem is ephemeral: plans fetched at runtime disappear on
the next deploy or restart. The committed `data/` directory is the permanent cache.


## Disclaimer

PlanWat is an independent student project, not affiliated with the Military University of Technology. The official schedule is always the source of truth.

## License

MIT
