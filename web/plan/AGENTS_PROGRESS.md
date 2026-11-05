# AGENTS_PROGRESS.md

> **Last updated:** 2026-08-03
> **API:** trakt-api (195 endpoints, 358 tests, port 8080)

---

## Phase 1: App Shell ✅

**Commit:** `bea2594` — "Phase 1: App shell"

**What was built:**
- Vite 8 + React 19 + TypeScript strict scaffold
- Tailwind CSS v4 with `--sf-*` design tokens (Ultra purple/cyan glass theme)
- 35 design-kit components copied and adapted (UI primitives + shared helpers)
- TanStack Router file-based routing with 9 placeholder routes
- TanStack Query v5 with 30s stale time, 2x retry
- Clerk auth integration (provider, styled sign-in, auth guards on root route)
- API client generated from `trakt-api/openapi.yaml` (195 endpoints typed)
- `ApiClient` with Clerk token injection + error normalization + toast on failure
- App shell: Topbar + Sidebar + MobileNav + Page layout components
- Biome 2.5.6 config (matching design-kit)
- PostCSS + Vite config with `@/` and `@design-kit/` aliases

**Verification:** `pnpm build` passes — 2431 modules, 65KB CSS, 657KB JS

**Routes:**
| Route | Status |
|---|---|
| `/` | Placeholder |
| `/search` | Placeholder |
| `/watchlist` | Placeholder |
| `/history` | Placeholder |
| `/lists` | Placeholder |
| `/calendar` | Placeholder |
| `/social` | Placeholder |
| `/profile` | Placeholder |
| `/settings` | Placeholder |

---

## Phase 2: Media Core 🔄
**Delegated:** deleg_xxx
**Status:** In progress
**Goal:** Search, movie/show/episode/season/person pages, inline actions

---

## Phase 3: Home + Sync ⏳
**Status:** Pending
**Goal:** Activity feed, continue watching, watchlist, history, ratings, collection

---

## Phase 4: Lists ⏳
**Status:** Pending
**Goal:** List CRUD, smart-list editor, community browse

---

## Phase 5: Profile + Settings ⏳
**Status:** Pending
**Goal:** Profile, settings, API keys, webhooks, notifications

---

## Phase 6: Calendar + Social ⏳
**Status:** Pending
**Goal:** Calendar views, comments, activity feed, social features

---

## Phase 7: PWA + Polish ⏳
**Status:** Pending
**Goal:** Offline shell, mutation queue, E2E tests

---

## New Components Created
| Component | Phase |
|---|---|
| `Page`, `Section`, `PosterGrid`, `ScrollRow` | P1 — layout primitives |
| `MobileNav` | P1 — bottom nav |
| `Sidebar` | P1 — sidebar navigation |
| `AppShell` | P1 — layout wrapper |

## Design Kit Components Available (35)
Button, Input, Select, SearchBar, FilterPill, Badge, Chip, GlassPill, Label, Eyebrow, Number, Card, MoodTile, Dialog, Dropdown, Tooltip, Banner, Toaster, PosterCard, Hero, MiniInfoCard, GenreStrip, DetailStat, StatCard, FactRow, Pagination, PaginationControls, ShimmerText, JsonHighlight, Topbar, Rail, MobileNav, SectionHead, InfoPanel, ProfilePill

## API Integration
- Generated client: `src/api/generated/schema.ts` (from trakt-api/openapi.yaml)
- Typed fetcher with Clerk auth header injection
- 29 API tags, 195 endpoints available
