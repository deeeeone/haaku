# TKiller Web — Architecture & Project Plan

> **Status:** Planning phase · **Last updated:** 2026-08-03
> **API:** [trakt-api](../../trakt-api/openapi.yaml) — 195 endpoints, 29 tags
> **Design system:** [design-kit](../../design-kit/) — 35 components, Tailwind v4 + Radix UI

---

## Stack

| Layer | Technology |
|---|---|
| Framework | React 19 + TypeScript (strict) |
| Build | Vite 8 |
| Routing | TanStack Router (type-safe file-based) |
| Data fetching | TanStack Query v5 |
| Auth | Clerk (`@clerk/clerk-react`) |
| API client | Generated from OpenAPI spec (typed fetcher) |
| Styling | Tailwind CSS v4 + design-kit tokens (`--sf-*`) |
| Components | Design-kit UI primitives (Radix + CVA + framer-motion) |
| Forms | React Hook Form + Zod validation |
| PWA | vite-plugin-pwa (offline shell + mutation queue) |
| Testing | Playwright (E2E journeys), Vitest (unit) |
| Linting | Biome (same config as design-kit) |

---

## Project Structure

```
tkiller-web/
├── plan/                    # Planning documents (this dir)
├── src/
│   ├── main.tsx             # Entry point, providers
│   ├── App.tsx              # Router + layout
│   ├── api/                 # Generated API client + TanStack Query hooks
│   │   ├── client.ts        # Base fetcher (auth header, error handling)
│   │   ├── generated/       # Auto-generated from openapi.yaml
│   │   └── hooks/           # TanStack Query wrappers per domain
│   ├── routes/              # TanStack Router file-based routes
│   │   ├── __root.tsx       # Root layout (topbar, nav, auth guard)
│   │   ├── index.tsx        # Home feed
│   │   ├── search.tsx       # Search page
│   │   ├── movies/
│   │   ├── shows/
│   │   ├── watchlist.tsx
│   │   ├── history.tsx
│   │   ├── lists/
│   │   ├── calendar.tsx
│   │   ├── profile/
│   │   └── settings/
│   ├── components/          # App-specific components (composed from design-kit)
│   │   ├── media/           # PosterCard, MediaHero, EpisodeList, etc.
│   │   ├── layout/          # Page shell, sidebar, topbar overrides
│   │   ├── lists/           # ListCard, ListEditor, SmartListBuilder
│   │   ├── social/          # FollowButton, ActivityFeed, CommentThread
│   │   ├── sync/            # RatingStars, WatchlistButton, HistoryRow
│   │   └── ui/              # Thin wrappers re-exporting design-kit
│   ├── lib/                 # Utils: cn(), formatDate, slugify, media helpers
│   ├── hooks/               # Shared hooks: useAuth, useMedia, useDebounce
│   └── stores/              # Zustand (if needed beyond TanStack Query cache)
├── public/                  # Static assets, PWA manifest, icons
├── e2e/                     # Playwright test specs
└── package.json
```

---

## API Integration Strategy

**Client generation:** Run `openapi-typescript` on `openapi.yaml` → typed fetch client.
**TanStack Query hooks:** One hook file per API domain tag (e.g., `hooks/useSync.ts`, `hooks/useLists.ts`).
**Auth interceptor:** Clerk `getToken()` injected into every request automatically.
**Error handling:** Global error boundary + per-query retry (3x exponential backoff for 5xx).

### API Surface by Feature

| Feature | API Tags | Routes | Auth |
|---|---|---|---|
| Discovery | Movies, Shows, Discovery, Search, Recommendations | 33 | Mixed |
| Media Detail | Movies, Shows, Episodes, Seasons, People, Reference | 35 | Public |
| Sync | Sync, Syncs | 25 | 🔒 All |
| Scrobble/Checkin | Scrobble, Checkin | 6 | 🔒 All |
| Lists | Lists | 25 | 🔒 23, Public 2 |
| Social | Social, Comments, Notifications | 28 | 🔒 All |
| Calendars | Calendars, Media | 6 | 🔒 4, Public 2 |
| Profile/Settings | Users, API Keys, Filters, Notes, Reports, Webhooks, Integrations | 32 | 🔒 All |
| OAuth | OAuth | 6 | Mixed |

---

## Design Kit Component Inventory

35 components available in `design-kit/src/components/ui/`:

| Component | Category | Used For |
|---|---|---|
| `poster-card` | Media | Movie/show poster grid items |
| `hero` | Media | Media detail page headers |
| `mini-info-card` | Media | Episode list items, search results |
| `genre-strip` | Media | Genre tags on detail pages |
| `detail-stat` | Media | Stats display (rating, year, runtime) |
| `stat-card` | Data | User stats, dashboard cards |
| `fact-row` | Data | Key-value rows in detail panels |
| `info-panel` | Layout | Side panels, metadata blocks |
| `section-head` | Layout | Page section headers |
| `topbar` | Layout | App header with search bar |
| `mobile-nav` | Layout | Bottom navigation for PWA |
| `rail` | Layout | Sidebar navigation |
| `search-bar` | Input | Global search |
| `button` | Input | Primary/secondary/destructive actions |
| `input` | Input | Form fields |
| `select` | Input | Dropdown selections |
| `filter-pill` | Input | Active filter chips |
| `chip` | Display | Status badges, count indicators |
| `badge` | Display | Notification count, labels |
| `glass-pill` | Display | Transparent label pills |
| `label` | Display | Form labels, section labels |
| `banner` | Feedback | Alerts, promo banners |
| `toast` | Feedback | Sonner-based notifications |
| `dialog` | Overlay | Modals, confirmations |
| `dropdown` | Overlay | Action menus |
| `tooltip` | Overlay | Hover explanations |
| `card` | Container | Content cards, list items |
| `mood-tile` | Container | Category/collection tiles |
| `shimmer-text` | Loading | Skeleton loading states |
| `pagination-controls` | Navigation | Page navigation |
| `pagination` | Navigation | Simple prev/next |
| `number` | Display | Animated number transitions |
| `json-highlight` | Dev | API response display |
| `eyebrow` | Typography | Section overlines |

---

## Development Phases (matching roadmap T6 tasks)

| Phase | Tasks | Depends On |
|---|---|---|
| **P1 — App Shell** | T6.1: Routing, auth flow, TanStack Query, API client gen, theme | API health |
| **P2 — Media Core** | T6.2: Search, movie/show/episode/season/person pages, inline actions | P1 + API Phase 1 |
| **P3 — Home + Sync** | T6.3: Activity feed, continue watching, watchlist, history | P1 + API Phase 3 |
| **P4 — Lists** | T6.4: List CRUD, smart-list editor, community browse | P1 + API Phase 4 |
| **P5 — Profile + Settings** | T6.6: Profile, settings, API keys, webhooks, notifications | P1 + API Phase 2 |
| **P6 — Calendar + Progress** | T6.7: Calendar views, playback progress, notifications inbox | P1 + API Phase 9b |
| **P7 — PWA** | T6.8: Offline shell, mutation queue | P1 (can run parallel) |

---

## Key Architectural Decisions

1. **TanStack Router over React Router** — type-safe route params, file-based routing, built-in loaders (IDE autocomplete on route params).
2. **TanStack Query over SWR** — mutations, optimistic updates, infinite queries for paginated lists.
3. **Generated API client** — `openapi-typescript` → typed fetcher, never hand-write `fetch()` calls.
4. **Design kit as dependency** — import from `@/components/ui/` (symlink or workspace package), never copy-paste components.
5. **Clerk for auth** — same as API; `useAuth()` hook, `getToken()` interceptor, syncs with API's JWKS verification.
6. **No global state library** — TanStack Query cache is the source of truth for server state. Zustand only if needed for local UI state (sidebar open, theme toggle).
7. **PWA from day one** — offline shell, installable, push notifications (Phase 2).
