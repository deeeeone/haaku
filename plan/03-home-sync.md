# P3 — Home Feed & Sync UI (T6.3)

> **API tags:** Sync (21), Scrobble (3), Checkin (3), Syncs (4), Recommendations (4)
> **Total routes:** 35, all 🔒 auth required
> **Components:** poster-card, mini-info-card, stat-card, section-head, rail, button, rating-stars (new), chip, pagination-controls, dropdown, dialog, toast, shimmer-text

---

## 1. Home Feed (`/` — Authenticated View)

### 1.1 API Endpoints
- `GET /v1/users/me/activity?type=friends&page=&limit=` — Activity feed
- `GET /v1/recommendations/movies` — Personalized movie recs
- `GET /v1/recommendations/shows` — Personalized show recs
- `GET /v1/social_recommendations/movies` — Friend-based movie recs
- `GET /v1/social_recommendations/shows` — Friend-based show recs
- `GET /v1/sync/playback/movies` — Resume movie playback
- `GET /v1/sync/playback/episodes` — Resume episode playback

### 1.2 UI Components
```
HomePage (authenticated)
├── SectionHead "Continue Watching"
│   └── Rail → MiniInfoCard[] (with progress bar)
├── SectionHead "Recommended for You"
│   └── Rail → PosterCard[] (from personalized recs)
├── SectionHead "Friends Are Watching"
│   └── Rail → PosterCard[] (from social recs)
├── SectionHead "Activity Feed"
│   └── ActivityFeed (paginated scroll)
│       ├── "You watched X" → media link
│       ├── "Friend rated Y 8/10"
│       └── "Friend added Z to watchlist"
└── Discovery rails (trending, popular — same as public view)
```

### 1.3 Continue Watching
- Queries `GET /v1/sync/playback/movies` and `GET /v1/sync/playback/episodes`
- Shows media with `progress > 0` and `paused_at` recent
- Progress bar: filled percentage
- Click → navigate to detail page with scrobble context

---

## 2. Watchlist Page (`/watchlist`)

### 2.1 API Endpoints
- `GET /v1/sync/watchlist?page=&limit=&sort=` — User's watchlist
- `POST /v1/sync/watchlist` — Add to watchlist (inline action)
- `POST /v1/sync/watchlist/remove` — Remove from watchlist
- `PUT /v1/sync/watchlist/reorder` — Drag-and-drop reorder

### 2.2 UI
```
WatchlistPage
├── FilterPill[] (movies | shows | all)
├── Sort selector (rank, title, added_at, released)
├── Grid/List toggle
├── PosterCard[] (draggable for reorder)
│   └── Each card: remove button, "watched" quick-action
├── PaginationControls
└── Empty state: "Your watchlist is empty. Browse trending →"
```

### 2.3 Drag-and-Drop Reorder
- Use `@dnd-kit/core` for drag-and-drop
- On drop → `PUT /v1/sync/watchlist/reorder` with new rank array
- Optimistic update in TanStack Query cache

---

## 3. History Page (`/history`)

### 3.1 API Endpoints
- `GET /v1/sync/history?page=&limit=&type=&start_at=&end_at=` — Watch history
- `POST /v1/sync/history` — Add to history
- `POST /v1/sync/history/remove` — Remove history items
- `GET /v1/sync/history/{media_type}/{media_id}` — History for single item

### 3.2 UI
```
HistoryPage
├── FilterPill[] (movies | shows | episodes | all)
├── Date range picker (start_at, end_at)
├── HistoryList
│   ├── DateGroup header ("Today", "Yesterday", "This Week", "Aug 2026")
│   └── HistoryRow[]
│       ├── Poster thumbnail
│       ├── Title + episode info + watched_at time
│       ├── Action: add to list, rate, favorite
│       └── Action: remove from history
└── PaginationControls (infinite scroll preferred)
```

### 3.3 Behavior
- Infinite scroll preferred over pagination (load more on scroll)
- Grouped by date (today, yesterday, this week, older)
- Swipe-to-remove on mobile

---

## 4. Collection Page (`/collection`)

### 4.1 API Endpoints
- `GET /v1/sync/collection?page=&limit=&type=` — User's collection
- `POST /v1/sync/collection` — Add to collection
- `POST /v1/sync/collection/remove` — Remove from collection

### 4.2 UI
```
CollectionPage
├── FilterPill[] (movies | shows | all)
├── Grid of PosterCard[]
│   └── Each: owned indicator, remove button
└── PaginationControls
```

---

## 5. Ratings Page (`/ratings`)

### 5.1 API Endpoints
- `GET /v1/sync/ratings?page=&limit=&type=` — User's ratings
- `POST /v1/sync/ratings` — Add/update rating
- `POST /v1/sync/ratings/remove` — Remove rating

### 5.2 UI
```
RatingsPage
├── FilterPill[] (movies | shows | episodes | seasons | all)
├── Grid of PosterCard[]
│   └── Each: rating stars overlay, remove button
└── PaginationControls
```

---

## 6. Favorites Page (`/favorites`)

### 6.1 API Endpoints
- `GET /v1/sync/favorites?page=&limit=&type=` — User's favorites
- `POST /v1/sync/favorites` — Add favorite
- `POST /v1/sync/favorites/remove` — Remove favorite

---

## 7. Rating Stars Component (new)

### 7.1 Design
```
[★][★][★][★][★][★][★][★][★][★]  (1-10 scale)
                    ↑ hover position
```

### 7.2 States
- **Empty:** all stars outline (gray)
- **Hover:** fill up to hover position (accent color)
- **Selected:** fill up to user's rating
- **Disabled:** when loading (shimmer)
- **Read-only:** on public media pages (show average)

### 7.3 API
```tsx
<RatingStars
  value={7}           // current rating (1-10)
  onChange={(v) => {  // POST /v1/sync/ratings
    mutate({ media_type: 'movie', media_id: 123, rating: v })
  }}
  readOnly={false}
/>
```

---

## 8. Common Sync Components

### 8.1 WatchlistButton
- Toggle button (bookmark icon)
- Active state: filled bookmark
- Inactive state: outline bookmark
- Used on: media detail pages, PosterCard hover, search results

### 8.2 FavoriteButton
- Toggle button (heart icon)
- Active: filled heart (red)
- Inactive: outline heart
- Used on: media detail pages, PosterCard hover

### 8.3 RateButton
- Trigger for RatingStars popup/dropdown
- Shows current rating if exists
- Used on: media detail pages, list items

---

## 9. Verification Checklist

- [ ] Home feed shows activity + continue watching (authenticated)
- [ ] Watchlist page: add, remove, reorder works
- [ ] History page: grouped by date, infinite scroll
- [ ] Collection page: add, remove, filter by type
- [ ] Ratings page: star picker, add/update/remove
- [ ] Favorites page: toggle, filter by type
- [ ] Inline actions on detail pages update sync state immediately
- [ ] Optimistic updates roll back on API error
- [ ] Empty states for all pages
- [ ] Loading skeletons match final layout