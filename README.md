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

## Disclaimer

PlanWat is an independent student project, not affiliated with the Military University of Technology. The official schedule is always the source of truth.

## License

MIT
