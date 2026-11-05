# P1 — App Shell (T6.1)

> **API routes:** Health, OAuth, Users (auth endpoints)
> **Components needed:** topbar, mobile-nav, rail, button, input, dialog, toast, shimmer-text
> **Auth:** Clerk — login, register, verify email, password reset
> **API client:** Generated from `../trakt-api/openapi.yaml`

---

## 1. Project Bootstrap

### 1.1 Scaffold with Vite 8
```bash
pnpm create vite tkiller-web --template react-ts
```

### 1.2 Install Dependencies
```bash
pnpm add \
  @clerk/clerk-react \
  @tanstack/react-query @tanstack/react-router \
  openapi-fetch openapi-typescript \
  react-hook-form zod @hookform/resolvers \
  framer-motion lucide-react sonner \
  clsx tailwind-merge cva \
  vite-plugin-pwa

pnpm add -D \
  @biomejs/biome \
  @vitejs/plugin-react \
  tailwindcss @tailwindcss/vite \
  playwright @playwright/test \
  vitest @testing-library/react
```

### 1.3 Configure Design Kit
```bash
# Symlink or workspace reference to design-kit
# Import components from @/components/ui/
# Copy motion.css, index.css tokens (--sf-*)
```

### 1.4 Generate API Client
```bash
# From trakt-api/openapi.yaml:
npx openapi-typescript ../trakt-api/openapi.yaml -o src/api/generated/schema.ts
```
Write `src/api/client.ts` — a typed wrapper around `openapi-fetch` with:
- Base URL from env (`VITE_API_URL=http://localhost:8080`)
- Clerk `getToken()` interceptor for Authorization header
- Error normalization (unwraps `{error: {code, message}}`)
- `onResponse` hook for global error toast

---

## 2. Clerk Auth Integration

### 2.1 Clerk Provider
```tsx
// src/main.tsx
import { ClerkProvider } from '@clerk/clerk-react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'

const clerkPubKey = import.meta.env.VITE_CLERK_PUBLISHABLE_KEY
const queryClient = new QueryClient()

function App() {
  return (
    <ClerkProvider publishableKey={clerkPubKey}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </ClerkProvider>
  )
}
```

### 2.2 Route Protection
```tsx
// src/routes/__root.tsx - check auth state
// Redirect to /login if unauthenticated for protected routes
// Clerk components: <SignIn>, <SignUp>, <UserButton>
```

### 2.3 Auth Hooks
```tsx
// src/hooks/useAuth.ts
// Wraps Clerk's useAuth(), provides:
// - isAuthenticated, isLoading, user
// - getToken() for API calls
// - signOut(), redirectToSignIn()
```

---

## 3. TanStack Router Setup

### 3.1 Route Tree
```
/                          → Home feed (index.tsx)
/login                     → Clerk SignIn
/register                  → Clerk SignUp
/search?q=                 → Search page
/movies/$id                → Movie detail
/shows/$id                 → Show detail
/shows/$id/seasons/$season/episodes/$ep → Episode detail
/people/$id                → Person detail
/watchlist                 → My watchlist
/history                   → My history
/collection                → My collection
/ratings                   → My ratings
/favorites                 → My favorites
/lists                     → My lists
/lists/$id                 → List detail
/lists/popular             → Community popular lists
/lists/trending            → Community trending lists
/calendar                  → My calendar
/calendar/all              → All shows calendar
/activity                  → My activity feed
/notifications             → Notification inbox
/profile/$id               → User profile
/me/settings               → Account settings
/me/settings/api-keys      → API key management
/me/settings/webhooks      → Webhook management
/me/settings/integrations  → Integration tokens
```

### 3.2 Route Guards
- **Public routes:** `/`, `/login`, `/register`, `/search`, `/movies/*`, `/shows/*`, `/people/*`, `/lists/popular`, `/lists/trending`, `/calendar/all`, `/profile/*`
- **Auth routes:** everything else (`/watchlist`, `/history`, `/lists/*`, `/me/*`, `/calendar`, `/notifications`, etc.)

---

## 4. TanStack Query Hooks

### 4.1 Hook Organization
```
src/api/hooks/
├── useMedia.ts       # Movies, Shows, Episodes, Seasons, People
├── useDiscovery.ts   # Trending, popular, recommendations
├── useSearch.ts      # Search queries
├── useSync.ts        # History, watchlist, collection, ratings, favorites
├── useLists.ts       # Lists, smart lists, items
├── useSocial.ts      # Follows, friends, blocks
├── useComments.ts    # Comments, reactions
├── useActivity.ts    # Activity feed
├── useCalendar.ts    # Calendar endpoints
├── useUsers.ts       # Profile, settings, stats
├── useScrobble.ts    # Scrobble, checkin, playback
└── useNotifications.ts
```

### 4.2 Pattern
```tsx
// src/api/hooks/useMedia.ts
import { useQuery } from '@tanstack/react-query'
import { apiClient } from '@/api/client'
import type { paths } from '@/api/generated/schema'

export function useMovie(id: string) {
  return useQuery({
    queryKey: ['movie', id],
    queryFn: () => apiClient.GET('/v1/movies/{id}', { params: { path: { id } } }),
    staleTime: 5 * 60 * 1000,
  })
}
```

---

## 5. Theme + Layout

### 5.1 Design Kit Tokens
Copy from `design-kit/`:
- `index.css` — Tailwind v4 entry, `@theme`, `--sf-*` tokens
- `motion.css` — animations (`.reveal*`, `.sf-*`, `.t-*`)
- `src/components/ui/*` — all 35 components

### 5.2 Root Layout
```tsx
// src/routes/__root.tsx
<Topbar>
  <SearchBar />      // Global search
  <UserMenu />       // Avatar, settings, logout
</Topbar>
<Sidebar>
  <Rail>
    // Home, Discover, Calendar, Watchlist, Lists, Activity
  </Rail>
</Sidebar>
<main>
  <Outlet />
</main>
<MobileNav>
  // Bottom nav for mobile/PWA
</MobileNav>
```

### 5.3 Responsive Breakpoints
- Mobile: < 768px — full-width, mobile nav, simplified layouts
- Tablet: 768–1024px — compact rail, 2-col grids
- Desktop: > 1024px — full rail, multi-column, side panels

---

## 6. Verification Checklist

- [ ] `pnpm dev` starts on port 5173
- [ ] Clerk sign-in flow works
- [ ] API client fetches `/healthz` successfully
- [ ] TanStack Router renders routes with type-safe params
- [ ] Design kit components render with `--sf-*` tokens
- [ ] Mobile nav appears on < 768px viewport
- [ ] Route guards redirect unauthenticated users to `/login`
- [ ] `pnpm build` passes (typecheck + production build)
- [ ] Biome lint passes