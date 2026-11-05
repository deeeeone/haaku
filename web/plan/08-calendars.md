# P7 — Calendar & Progress (T6.7)

> **API tags:** Calendars (5 routes), Scrobble (3 🔒), Sync/Playback (partial)
> **Components:** mini-info-card, card, poster-card, section-head, select, filter-pill, chip, badge, rail, pagination-controls, shimmer-text, stat-card

---

## 1. Calendar Page (`/calendar`)

### 1.1 API Endpoints
- `GET /v1/calendars/my/shows/{start_date}/{days}` — My shows (🔒)
- `GET /v1/calendars/all/shows/{start_date}/{days}` — All shows
- `GET /v1/calendars/friends/shows/{start_date}/{days}` — Friends' shows (🔒)
- `GET /v1/calendars/favorites/shows/{start_date}/{days}` — Favorited shows (🔒)
- `GET /v1/calendars/watched/shows/{start_date}/{days}` — Watched shows (🔒)

URL params: `?type=new|premieres|finale` (optional)

### 1.2 UI
```
CalendarPage
├── Header: Date range navigator
│   ├── < Prev week | "Aug 3 – Aug 9, 2026" | Next week >
│   └── "Today" button
├── Target selector
│   └── TabBar: [My Shows | All Shows | Friends | Favorites | Watched]
├── Type filter
│   └── FilterPill[]: [All | New Shows | Season Premieres | Season Finales]
├── CalendarGrid (by day)
│   ├── Monday, Aug 3
│   │   ├── CalendarEntry
│   │   │   ├── Show poster thumbnail
│   │   │   ├── Show title
│   │   │   ├── Episode: "S03E05: Episode Title"
│   │   │   ├── Chip: "New Show" / "Premiere" / "Finale"
│   │   │   └── Click → episode detail
│   │   └── CalendarEntry...
│   ├── Tuesday, Aug 4
│   │   └── ...
│   └── ...
└── On mobile: list view instead of grid
```

### 1.3 CalendarEntry Component
```tsx
<CalendarEntry
  episode={...}
  show={...}
  season={...}
  type="new"  // new | premiere | finale | undefined
/>
```

---

## 2. Scrobble UI

### 2.1 API Endpoints
- `POST /v1/scrobble/start` — Start watching
- `POST /v1/scrobble/pause` — Pause watching
- `POST /v1/scrobble/stop` — Stop (scrobbles if ≥80%)

### 2.2 Scrobble Controls (on media detail page)
```
ScrobbleBar (floating, bottom-fixed on media page)
├── Episode/Movie title
├── Progress bar (estimated based on time since start)
├── Pause button → POST /v1/scrobble/pause
├── Stop button → POST /v1/scrobble/stop
│   └── If <80%: "Continue watching later?"
│   └── If ≥80%: "Scrobbled!" toast
└── Timer ("12:34 elapsed")
```

### 2.3 Checkin Button
```
CheckinSection (on media detail page)
├── "Watching now" indicator (if checkin active)
├── Button "Check In" → POST /v1/checkin
├── Button "Cancel Checkin" → DELETE /v1/checkin
└── Expires in: countdown timer
```

---

## 3. Continue Watching / Progress

### 3.1 API Endpoints
- `GET /v1/sync/playback/movies` — Movie playback positions (🔒)
- `GET /v1/sync/playback/episodes` — Episode playback positions (🔒)
- `GET /v1/sync/playback` — All playback positions
- `DELETE /v1/sync/playback/{media_type}/{media_id}` — Remove position

### 3.2 Home Page Rail
```
Rail "Continue Watching"
├── MiniInfoCard[] (with progress bar overlay)
│   ├── Movie poster + title
│   ├── Progress: [████████░░] 78%
│   ├── "Resume" button → navigate to detail + start scrobble
│   └── "Remove" button → DELETE playback position
└── Max 10 items, horizontal scroll
```

### 3.3 Continue Watching Page (`/progress`)
```
ProgressPage
├── SectionHead "Continue Watching"
│   └── Grid of MiniInfoCard[] (with progress bar)
├── FilterPill[] (movies | shows | all)
└── Empty state: "No content in progress. Start watching something!"
```

---

## 4. Hidden Items Management

### 4.1 API Endpoints
- `GET /v1/users/hidden/{section}?page=&limit=` — List hidden items (🔒)
- `POST /v1/users/hidden/{section}` — Hide item
- `DELETE /v1/users/hidden/{section}` — Unhide

### 4.2 Sections
- `calendar` — Hidden from calendar
- `progress_watched` — Hidden from continue watching
- `recommendations` — Hidden from recommendations

### 4.3 UI
- Three-dot menu on media → "Hide from..." dropdown
- Settings → Hidden Items tab: list + restore per section

---

## 5. "Up Next" / Anticipated

### 5.1 API Endpoints
- `GET /v1/media/anticipated?type=&page=&limit=` — Anticipated media (🔒)

### 5.2 Home Page Rail
```
Rail "Coming Soon"
├── MoodTile[] or PosterCard[]
│   ├── Show poster
│   ├── Title
│   ├── Release date / next episode date
│   └── Watchlist count
└── Horizontal scroll, max 20
```

---

## 6. Verification Checklist

- [ ] Calendar: navigate weeks, switch targets (my/all/friends/favorites/watched)
- [ ] Calendar: filter by episode type (new/premieres/finale)
- [ ] Calendar: entries click through to episode detail
- [ ] Scrobble: start/pause/stop from media detail page
- [ ] Scrobble: progress bar updates during playback
- [ ] Scrobble: stop at ≥80% triggers scrobble + history entry
- [ ] Checkin: create, see active status, cancel
- [ ] Continue watching: rail on home page with progress bars
- [ ] Remove from continue watching
- [ ] Hide items from calendar/recommendations
- [ ] Restore hidden items
- [ ] Anticipated: upcoming shows/movies rail