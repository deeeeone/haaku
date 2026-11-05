# P4 — Lists UI (T6.4)

> **API tags:** Lists (25 routes — 23 🔒, 2 public)
> **Components:** poster-card, card, mini-info-card, input, select, dialog, dropdown, button, filter-pill, chip, glass-pill, badge, pagination-controls, section-head, rail, shimmer-text, tooltip, genre-strip, detail-stat, fact-row

---

## 1. My Lists Page (`/lists`)

### 1.1 API Endpoints
- `GET /v1/users/{id}/lists` — User's lists
- `POST /v1/users/me/lists` — Create list
- `GET /v1/users/me/lists/collaborations` — Lists I can edit

### 1.2 UI
```
ListsPage
├── SectionHead "My Lists" + "New List" Button
├── FilterPill[] (owned | collaborated | all)
├── Grid of ListCard[]
│   ├── List name + description
│   ├── Chip (privacy: public/link_only/private)
│   ├── Badge (item count)
│   ├── StatCard (likes, comments)
│   └── Actions: edit, delete, share
├── SectionHead "Collaborations"
│   └── ListCard[] (from collaborations endpoint)
└── "Create List" Dialog
    ├── Input (name)
    ├── Input (description, optional)
    ├── Select (privacy: public, link_only, private)
    ├── Toggle (allow comments, is ranked)
    └── Button (create)
```

### 1.3 Create List Dialog
```tsx
<Dialog>
  <form onSubmit={handleCreate}>
    <Input label="Name" required />
    <Input label="Description" />
    <Select label="Privacy" options={['public', 'link_only', 'private']} />
    <Toggle label="Allow comments" />
    <Toggle label="Ranked list" />
  </form>
</Dialog>
```

---

## 2. List Detail Page (`/lists/$id`)

### 2.1 API Endpoints
- `GET /v1/lists/{id}` — List detail
- `GET /v1/lists/{id}/items?page=&limit=` — List items
- `POST /v1/lists/{id}/items` — Add items
- `POST /v1/lists/{id}/items/remove` — Remove items
- `PUT /v1/lists/{id}/items/reorder` — Reorder items
- `PUT /v1/lists/{id}` — Update list
- `DELETE /v1/lists/{id}` — Delete list
- `POST /v1/lists/{id}/like` / `DELETE` — Like/unlike
- `GET /v1/lists/{id}/likes` — Who liked
- `GET /v1/lists/{id}/comments` — Comments
- `GET /v1/lists/{id}/collaborators` — Collaborators
- `POST /v1/lists/{id}/collaborators` — Invite
- `DELETE /v1/lists/{id}/collaborators/{user_id}` — Remove collab

### 2.2 UI
```
ListDetailPage
├── ListHero (name, description, cover image)
│   ├── Chip (privacy)
│   ├── Badge (item count)
│   ├── LikeButton + count
│   ├── ShareButton (copy link)
│   └── SettingsButton (edit, delete — owner only)
├── ActionBar (owner)
│   ├── "Add Items" Button → search modal
│   ├── "Invite Collaborator" Button
│   └── "Edit List" Button
├── ListItems (grid or rank list)
│   ├── PosterCard[] (draggable if is_ranked)
│   │   ├── Rank number
│   │   ├── Remove button
│   │   └── Notes indicator
│   └── PaginationControls
├── Collaborators section
│   └── MiniInfoCard[] (avatar, username, role)
├── Comments section
│   └── CommentThread
└── Likes popup
```

### 2.3 Add Items Modal
```
Dialog: "Add to List"
├── SearchBar (search movies/shows)
├── SearchResults → PosterCard[]
│   └── "Add" button on each
├── Or: "Add from watchlist/history"
│   └── Quick-add from recent activity
└── Close
```

---

## 3. Smart Lists (`/lists` → Smart Lists tab)

### 3.1 API Endpoints
- `GET /v1/users/me/smart-lists` — User's smart lists
- `POST /v1/users/me/smart-lists` — Create smart list
- `GET /v1/smart-lists/{id}` — Smart list detail
- `PUT /v1/smart-lists/{id}` — Update smart list
- `DELETE /v1/smart-lists/{id}` — Delete
- `GET /v1/smart-lists/{id}/items?page=&limit=` — Resolved items

### 3.2 Smart List Editor (Modal)
```
Dialog: "Create Smart List"
├── Input (name)
├── Filter Builder (visual DSL)
│   ├── Select "Media type" (movies | shows)
│   ├── GenreStrip → multi-select genres
│   ├── Input (year min, year max)
│   ├── Input (rating min)
│   ├── Input (votes min)
│   ├── Select "Status" (returning, ended, canceled — shows only)
│   ├── Toggle "Only watched"
│   └── Toggle "Only collected"
├── Button "Preview" → shows first 5 results
└── Button "Save"
```

### 3.3 Filter DSL Visual Editor
Map the smart-list filter fields to a visual builder:
```json
{
  "type": "shows",
  "genres": ["Action", "Drama"],
  "year_min": 2010,
  "year_max": 2024,
  "rating_min": 7.5,
  "votes_min": 100,
  "status": "returning",
  "watched": false,
  "collected": false
}
```

---

## 4. Community Lists (`/lists/popular`, `/lists/trending`)

### 4.1 API Endpoints
- `GET /v1/lists/popular?period=weekly&page=&limit=` — Popular lists
- `GET /v1/lists/trending?page=&limit=` — Trending lists

### 4.2 UI
```
CommunityListsPage
├── Period selector (weekly | monthly | all)
├── Grid of ListCard[]
│   ├── Name + creator
│   ├── StatCard (like count, comment count)
│   ├── Preview (first 4 posters as grid)
│   └── Click → navigate to list detail
└── PaginationControls
```

---

## 5. Inline "Add to List" Action

### 5.1 Where It Appears
- Media detail pages
- PosterCard hover menu
- Search results
- Watchlist/history item actions

### 5.2 UI
```
Dropdown: "Add to List"
├── "Create new list..." → opens Create List dialog
├── Divider
├── My lists (checkboxes)
│   ├── ☑ "Best Sci-Fi Movies"
│   ├── ☐ "To Watch This Weekend"
│   └── ☑ "Favorites"
└── Close
```

---

## 6. Verification Checklist

- [ ] Create list with privacy options
- [ ] Add items via search modal
- [ ] Remove items from list
- [ ] Reorder items (drag and drop)
- [ ] Like/unlike list
- [ ] Comment on list
- [ ] Invite collaborator
- [ ] Collaborator can add items
- [ ] Smart list editor: build filter, preview, save
- [ ] Smart list resolves items correctly
- [ ] Community lists page shows popular/trending
- [ ] Inline "Add to List" works from detail pages
- [ ] Delete list with confirmation dialog
- [ ] Private lists hidden from non-owners
- [ ] Link-only lists require URL to access