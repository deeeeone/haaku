# Component Map — API → Design Kit Components

> Maps every API feature area to the design-kit components available for building it.

---

## Component Availability

### Already in design-kit (35 components)

| Component | Used In |
|---|---|
| `badge` | Notification count, item count, list status |
| `banner` | Alerts, promo messages, empty states |
| `button` | Every interactive action |
| `card` | List items, content cards, settings cards |
| `chip` | Status indicators, media type labels |
| `detail-stat` | Movie/show stats (rating, year, runtime) |
| `dialog` | Modals: create list, report, confirm delete, create webhook |
| `dropdown` | Action menus: add to list, hide item, three-dot menus |
| `eyebrow` | Section overline text |
| `fact-row` | Key-value rows: detail pages, list info |
| `filter-pill` | Active filter chips: type, genre, period |
| `genre-strip` | Genre tags on media detail |
| `glass-pill` | Transparent labels, count indicators |
| `hero` | Media detail page headers |
| `info-panel` | Side panels, metadata blocks |
| `input` | Form fields everywhere |
| `json-highlight` | API response display (dev/debug) |
| `label` | Form labels, section labels |
| `mini-info-card` | Episode list, search results, follower cards |
| `mobile-nav` | Bottom navigation (PWA) |
| `mood-tile` | Category/collection tiles, genre browse |
| `number` | Animated stat transitions |
| `pagination-controls` | Paginated list navigation |
| `pagination` | Simple prev/next nav |
| `poster-card` | Movie/show grid items |
| `rail` | Horizontal scrollable sections, sidebar nav |
| `search-bar` | Global search |
| `section-head` | Page section headers |
| `select` | Dropdown selections |
| `shimmer-text` | Skeleton loading states |
| `stat-card` | User stats, dashboard numbers |
| `toast` | Sonner notifications, success/error messages |
| `tooltip` | Hover explanations |
| `topbar` | App header |

---

## New Components Needed (build from design-kit primitives)

| Component | Purpose | Built From |
|---|---|---|
| `rating-stars` | 1-10 star rating picker | button + chip + cn() |
| `watchlist-button` | Toggle bookmark button | button + lucide-react (Bookmark) |
| `favorite-button` | Toggle heart button | button + lucide-react (Heart) |
| `checkin-button` | Check in to media | button + dialog + timer |
| `scrobble-bar` | Playback controls + progress | card + button + progress bar |
| `comment-thread` | Nested comments + reactions | card + input + button + badge |
| `reaction-picker` | 6-reaction emoji picker | badge[] + tooltip |
| `activity-item` | Activity feed entry | mini-info-card + chip |
| `calendar-entry` | Calendar day entry | mini-info-card + chip + glas-pill |
| `list-card` | List summary card | card + poster-card[] + badge + chip |
| `smart-list-editor` | Visual filter DSL builder | dialog + select[] + genre-strip + input[] + toggle |
| `add-to-list-dropdown` | Quick-add to lists | dropdown + input + checkbox[] |
| `notification-item` | Notification inbox entry | mini-info-card + badge + chip |
| `follow-button` | Follow/unfollow with states | button (loading, active, inactive) |
| `progress-bar` | Playback progress overlay | div + cn() (linear gradient) |
| `spoiler-overlay` | Blur/hide spoiler content | div + cn() (backdrop-blur) |
| `empty-state` | "No content" illustrations | card + button + icon |
| `report-dialog` | Content reporting form | dialog + select + input + button |

---

## Page-to-Component Matrix

| Page | Design-Kit Components | New Components |
|---|---|---|
| **Home Feed** | rail, section-head, poster-card, mini-info-card, shimmer-text, stat-card | activity-item, progress-bar, empty-state |
| **Search** | search-bar, filter-pill, poster-card, mini-info-card, pagination-controls, shimmer-text | empty-state |
| **Movie Detail** | hero, genre-strip, detail-stat, info-panel, stat-card, section-head, rail, poster-card, mini-info-card | watchlist-button, favorite-button, rating-stars, add-to-list-dropdown, scrobble-bar, checkin-button, comment-thread, report-dialog |
| **Show Detail** | hero, genre-strip, detail-stat, info-panel, section-head, rail, poster-card, select, mini-info-card | same as movie + calendar-entry |
| **Episode Detail** | hero, info-panel, stat-card, section-head, mini-info-card | watchlist-button, rating-stars, comment-thread |
| **Season Detail** | hero, info-panel, stat-card, section-head | rating-stars |
| **Person Detail** | hero, info-panel, section-head, rail, poster-card | — |
| **Watchlist** | section-head, filter-pill, select, poster-card, pagination-controls, shimmer-text | empty-state |
| **History** | section-head, filter-pill, mini-info-card, shimmer-text | empty-state, activity-item |
| **Ratings** | section-head, filter-pill, poster-card, pagination-controls | rating-stars, empty-state |
| **Collection** | section-head, filter-pill, poster-card, pagination-controls | empty-state |
| **Favorites** | section-head, filter-pill, poster-card, pagination-controls | empty-state |
| **My Lists** | section-head, card, button, dialog, input, select, chip, badge | list-card, empty-state |
| **List Detail** | hero (lite), card, poster-card, mini-info-card, section-head, dialog, dropdown, button, input, pagination-controls | comment-thread, empty-state |
| **Smart Lists** | dialog, select[], input[], genre-strip, filter-pill, section-head, poster-card, pagination-controls | smart-list-editor, empty-state |
| **Community Lists** | section-head, card, stat-card, pagination-controls | list-card, empty-state |
| **Calendar** | section-head, tab-bar, filter-pill, chip, mini-info-card, shimmer-text | calendar-entry, empty-state |
| **Social** | section-head, tab-bar, mini-info-card, button, card, input, badge, pagination-controls | follow-button, comment-thread, reaction-picker, activity-item, spoiler-overlay, report-dialog |
| **Activity Feed** | section-head, filter-pill, mini-info-card, shimmer-text | activity-item, empty-state |
| **Notifications** | section-head, mini-info-card, badge, pagination-controls | notification-item, empty-state |
| **Profile** | hero, stat-card, section-head, card, mini-info-card, poster-card, pagination-controls | follow-button, empty-state |
| **Settings** | section-head, card, input, select, button, dialog, dropdown, banner, toast | empty-state, report-dialog |
| **API Keys** | section-head, card, chip, button, dialog, dialog, input, toast | — |
| **Webhooks** | section-head, card, button, dialog, input, select[], chip, badge | — |
| **Comments Feed** | section-head, tab-bar, filter-pill, card, pagination-controls | comment-thread, reaction-picker |
| **Progress** | section-head, filter-pill, mini-info-card, shimmer-text | progress-bar, empty-state |
| **Continue Watching** | section-head, rail, mini-info-card | progress-bar |

---

## Component Build Priority

### Phase 1 (all pages need these)
1. `rating-stars` — used on every media detail + ratings page
2. `watchlist-button` — used on every media detail + poster hover
3. `favorite-button` — used on every media detail + poster hover
4. `follow-button` — used on profiles + social pages
5. `empty-state` — used on every list page

### Phase 2 (per-feature)
6. `comment-thread` + `reaction-picker` — comments + social
7. `calendar-entry` — calendar page
8. `scrobble-bar` + `progress-bar` — playback + progress
9. `list-card` — lists
10. `activity-item` — activity feed
11. `notification-item` — notifications
12. `smart-list-editor` — smart lists
13. `add-to-list-dropdown` — inline list actions

### Phase 3 (polish)
14. `spoiler-overlay` — comment spoilers
15. `report-dialog` — content reporting
16. `checkin-button` — checkin flow

---

## Design Token Usage

All components use the `--sf-*` token system from the design kit:
- `--sf-panel*` — Backgrounds, cards, surface colors
- `--sf-line*` — Borders, dividers, strokes
- `--sf-ink*` — Text colors, icons
- `--sf-accent*` — Primary/secondary/destructive actions, links, highlights

Never use un-namespaced tokens (`--background`, `--border`, `--foreground`).