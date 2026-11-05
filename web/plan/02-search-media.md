# P2 — Search & Media Detail (T6.2)

> **API tags:** Movies (9), Shows (15), Episodes (5), Seasons (3), People (3), Search (3), Discovery (6), Reference (5), Updates (3)
> **Total routes:** 49 public, 0 auth required
> **Components:** poster-card, hero, mini-info-card, genre-strip, detail-stat, stat-card, fact-row, info-panel, search-bar, filter-pill, section-head, pagination-controls, pagination, rail, chip, glass-pill, input, select, tooltip, shimmer-text

---

## 1. Search Page (`/search?q=`)

### 1.1 API Endpoints
- `GET /v1/search?q=&type=&page=&limit=` — Full-text search across movies, shows, episodes, people, lists
- `GET /v1/search/exact?q=` — Exact title match
- `GET /v1/search/lookup?id_type=imdb&id=tt1234` — Lookup by external ID

### 1.2 UI Components
```
SearchBar → SearchPage
├── FilterPill[] (type: all | movies | shows | episodes | people)
├── SearchResults
│   ├── PosterCard[] (movies/shows)
│   ├── MiniInfoCard[] (episodes/people)
│   └── ListCard[] (lists)
└── PaginationControls
```

### 1.3 Behavior
- Debounced input (300ms) → query `GET /v1/search`
- URL search params: `?q=breaking+bad&type=shows&page=1`
- Loading state: ShimmerText skeleton cards
- Empty state: "No results for 'xyz'" with suggested genres
- Error state: retry button

---

## 2. Home / Discover Page (`/`)

### 2.1 API Endpoints
- `GET /v1/movies/trending?page=&limit=` — Trending movies
- `GET /v1/movies/popular?page=&limit=` — Popular movies
- `GET /v1/shows/trending?page=&limit=` — Trending shows
- `GET /v1/shows/popular?page=&limit=` — Popular shows
- `GET /v1/recommendations/movies?page=&limit=` — Personalized recs (🔒)
- `GET /v1/recommendations/shows?page=&limit=` — Personalized recs (🔒)
- `GET /v1/lists/popular` — Popular community lists
- `GET /v1/lists/trending` — Trending community lists

### 2.2 UI Components
```
HomePage
├── Hero section (featured show/movie)
├── Rail "Trending Movies" → PosterCard[]
├── Rail "Trending Shows" → PosterCard[]
├── Rail "Popular Movies" → PosterCard[]
├── Rail "Popular Shows" → PosterCard[]
├── Rail "Recommended for You" → PosterCard[] (🔒, only if authenticated)
└── Rail "Popular Lists" → ListCard[]
```

### 2.3 Behavior
- Each rail is a horizontal scrollable section (or "View all" → grid)
- PosterCard: click → navigate to detail page
- Loading: shimmer cards in rail
- Public page — no auth required, but personalized rails hidden

---

## 3. Movie Detail Page (`/movies/$id`)

### 3.1 API Endpoints
- `GET /v1/movies/{id}` — Movie data (title, overview, year, genres, runtime, rating, poster, backdrop)
- `GET /v1/movies/{id}/people` — Cast and crew
- `GET /v1/movies/{id}/stats` — Watchers, plays, comments count
- `GET /v1/movies/{id}/ratings` — Rating distribution
- `GET /v1/movies/{id}/watch` — Watch providers by country
- `GET /v1/movies/{id}/lists` — Lists containing this movie
- `GET /v1/movies/{id}/watching` — Users currently watching
- `GET /v1/movies/{id}/aliases` — Alternative titles
- `GET /v1/movies/{id}/translations/{lang}` — Translations
- `GET /v1/movies/recommendations/{id}` — Similar movies

### 3.2 UI Components
```
MoviePage
├── Hero (backdrop + poster + title + year + runtime + genres)
│   ├── GenreStrip
│   ├── DetailStat[] (rating, runtime, year, language)
│   └── Action buttons: WatchlistButton, FavoriteButton, RateButton (🔒)
├── InfoPanel (overview, watch providers, aliases)
├── SectionHead "Cast"
│   └── MiniInfoCard[] (top 10 cast, "View all")
├── SectionHead "Stats"
│   └── StatCard[]
├── SectionHead "Similar Movies"
│   └── Rail → PosterCard[]
├── SectionHead "Lists"
│   └── ListCard[]
└── SectionHead "Comments" (🔒)
    └── CommentThread
```

### 3.3 Inline Actions (🔒 auth required)
- **WatchlistButton:** `POST /v1/sync/watchlist` / `DELETE`
- **FavoriteButton:** `POST /v1/sync/favorites` / `DELETE`
- **RateButton:** `POST /v1/sync/ratings` (star rating picker 1-10)
- **AddToListButton:** Dropdown → `POST /v1/lists/{id}/items`
- **CheckinButton:** `POST /v1/checkin`

---

## 4. Show Detail Page (`/shows/$id`)

### 4.1 API Endpoints
Same as movie detail, plus:
- `GET /v1/shows/{id}/seasons` — All seasons
- `GET /v1/shows/{id}/seasons/{season}` — Single season
- `GET /v1/shows/{id}/seasons/{season}/episodes` — Episodes in season
- `GET /v1/shows/{id}/seasons/{season}/episodes/{episode}` — Single episode

### 4.2 UI Components
```
ShowPage
├── Hero (same as movie)
├── InfoPanel
├── SectionHead "Seasons"
│   └── Select (season picker dropdown)
│       └── EpisodeList
│           ├── MiniInfoCard[] (title, number, aired, runtime)
│           └── "Mark as watched" button (🔒)
├── SectionHead "Cast"
├── SectionHead "Similar Shows"
└── Comments
```

### 4.3 Episode List
- Season selector dropdown
- Each episode: number, title, overview truncation, air date, watched indicator (🔒)
- Click → `/shows/$id/seasons/$season/episodes/$ep`

---

## 5. Episode Detail Page

### 5.1 API Endpoints
- `GET /v1/episodes/{id}` — Episode with show+season embedded
- `GET /v1/episodes/{id}/stats` — Watchers, plays, ratings
- `GET /v1/episodes/{id}/ratings` — Rating distribution
- `GET /v1/episodes/{id}/comments` — Comments on episode
- `GET /v1/episodes/{id}/lists` — Lists containing episode

### 5.2 UI
```
EpisodePage
├── Hero (show title + season+episode number + episode title)
├── InfoPanel (overview, air date, runtime)
├── Stats (watchers, plays, average rating)
├── Comments
└── Lists
```

---

## 6. Season Detail Page

### 6.1 API Endpoints
- `GET /v1/seasons/{id}` — Season with show embedded
- `GET /v1/seasons/{id}/stats` — Stats
- `GET /v1/seasons/{id}/ratings` — Ratings

---

## 7. Person Detail Page (`/people/$id`)

### 7.1 API Endpoints
- `GET /v1/people/{id}` — Person (name, bio, birthday, images, known_for)
- `GET /v1/people/{id}/movies` — Movie credits
- `GET /v1/people/{id}/shows` — Show credits

### 7.2 UI
```
PersonPage
├── Hero (photo + name + bio)
├── SectionHead "Movies"
│   └── PosterCard[]
└── SectionHead "TV Shows"
    └── PosterCard[]
```

---

## 8. Reference Data Pages

### 8.1 API Endpoints
- `GET /v1/genres/{type}` — Genre list for browse/discover
- `GET /v1/languages/{type}` — Language list
- `GET /v1/countries/{type}` — Country list
- `GET /v1/networks` — Network list
- `GET /v1/certifications/{type}` — Certification list

### 8.2 UI
- **Genre browse:** `/genres/movies` → grid of MoodTile components, click → filtered discovery
- **Networks page:** list of networks, click → shows on that network

---

## 9. Verification Checklist

- [ ] Search returns results from all types with correct filters
- [ ] Movie page shows all sections (hero, cast, stats, similar, lists)
- [ ] Show page shows seasons + episode list
- [ ] Episode/season detail pages render
- [ ] Person page shows filmography
- [ ] Inline actions (watchlist, favorite, rate) work when authenticated
- [ ] Unauthenticated users see public content, actions prompt login
- [ ] All loading states show shimmer skeletons
- [ ] All empty states show helpful messages
- [ ] Responsive: mobile, tablet, desktop