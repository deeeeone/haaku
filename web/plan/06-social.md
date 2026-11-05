# P6 — Social Features (T6.x: follows, friends, comments, activity)

> **API tags:** Social (8 🔒), Comments (17 🔒), Notifications (3 🔒)
> **Components:** mini-info-card, card, button, input, dropdown, dialog, badge, chip, section-head, pagination-controls, shimmer-text, tooltip, toast

---

## 1. Follow System

### 1.1 API Endpoints
- `POST /v1/users/{id}/follow` / `DELETE` — Follow/unfollow
- `GET /v1/users/{id}/followers?page=&limit=` — Followers list
- `GET /v1/users/{id}/following?page=&limit=` — Following list
- `GET /v1/users/{id}/friends?page=&limit=` — Mutual follows
- `GET /v1/users/me/follow_requests?page=&limit=` — Pending requests
- `POST /v1/users/me/follow_requests/{follower_id}/approve` — Approve
- `DELETE /v1/users/me/follow_requests/{follower_id}` — Deny

### 1.2 Followers/Following Page
```
SocialPage (/profile/$id/followers)
├── TabBar: [Followers | Following | Friends]
├── Grid of MiniInfoCard[]
│   ├── Avatar + username + display name
│   ├── FollowButton / FollowingButton
│   └── "Mutual" chip (if friends)
└── PaginationControls
```

### 1.3 Follow Requests Page
```
FollowRequestsPage (/me/follow_requests)
├── List of MiniInfoCard[]
│   ├── Avatar + username
│   ├── Button "Approve"
│   └── Button "Deny"
└── Empty state: "No pending follow requests"
```

---

## 2. Comments System

### 2.1 API Endpoints
- `POST /v1/comments` — Create comment
- `GET /v1/comments/{id}` — Single comment
- `PUT /v1/comments/{id}` — Edit comment
- `DELETE /v1/comments/{id}` — Delete comment
- `GET /v1/comments/{id}/replies?page=&limit=` — Replies
- `POST /v1/comments/{id}/replies` — Reply
- `POST /v1/comments/{id}/like` / `DELETE` — Like/unlike
- `GET /v1/comments/{id}/likes` — Who liked
- `POST /v1/comments/{id}/reactions` — Add reaction (👍🔥😂😮😢😡)
- `DELETE /v1/comments/{id}/reactions` — Remove reaction
- `GET /v1/comments/{id}/reactions` — Reaction totals
- `POST /v1/comments/{id}/report` — Report comment
- `GET /v1/comments/trending` — Trending comments
- `GET /v1/comments/recent` — Recent comments
- `GET /v1/comments/updates` — Recently edited

### 2.2 CommentThread Component
```tsx
<CommentThread mediaType="movie" mediaId={123} />
```
```
CommentThread
├── CommentInput (textarea + submit button)
│   └── Optional: spoiler toggle
├── CommentList
│   └── Comment[]
│       ├── Avatar + username + timestamp
│       ├── Body (markdown-like, truncated with "Show more")
│       ├── Spoiler overlay (if spoiler flag)
│       ├── ReactionBar
│       │   ├── ReactionButton[] (👍🔥😂😮😢😡) + counts
│       │   └── LikeButton + count
│       ├── Reply button → ReplyForm (inline)
│       ├── Actions (edit, delete, report — owner only)
│       └── Nested Replies (1 level deep)
│           └── Comment[] (same structure)
└── PaginationControls (for top-level, per-page for replies)
```

### 2.3 Reaction Picker
```
ReactionPicker (floating)
├── 👍 Like
├── 🔥 Fire
├── 😂 Laugh
├── 😮 Wow
├── 😢 Sad
└── 😡 Angry
```

---

## 3. Activity Feed

### 3.1 API Endpoints
- `GET /v1/users/me/activity?type=friends&page=&limit=&start_at=` — Activity feed

### 3.2 ActivityFeed Component
```
ActivityFeed
├── FilterPill[] (friends | followers | following | all)
├── ActivityItem[]
│   ├── Icon (based on event type)
│   │   ├── 📺 = history.added
│   │   ├── ⭐ = rating.added
│   │   ├── 🔖 = watchlist.added
│   │   ├── 💬 = comment.created
│   │   ├── ❤️ = favorite.added
│   │   └── 👤 = follow.created
│   ├── Text: "{User} watched {media}" (media name linked)
│   ├── Timestamp ("2 hours ago")
│   ├── Media thumbnail (optional)
│   └── Actions (like, reply — if comment-related)
└── PaginationControls (infinite scroll)
```

---

## 4. Blocking

### 4.1 API Endpoints
- `POST /v1/users/{id}/block` — Block user
- `DELETE /v1/users/{id}/block` — Unblock user

### 4.2 UI
- Three-dot menu on profile → "Block user"
- Blocked users list in Settings → Privacy tab
- Blocked users' content hidden from feed/comments

---

## 5. Community Comments Feed (`/comments`)

### 5.1 API Endpoints
- `GET /v1/comments/trending?type=&page=&limit=` — Trending (7 days)
- `GET /v1/comments/recent?type=&page=&limit=` — Recent
- `GET /v1/comments/updates?type=&page=&limit=` — Recently edited

### 5.2 UI
```
CommunityCommentsPage
├── TabBar: [Trending | Recent | Updates]
├── FilterPill[] (all | movies | shows | seasons | episodes | lists)
├── CommentList
│   └── CommentCard[]
│       ├── Media reference ("on Breaking Bad S03E05")
│       ├── Comment body (with spoiler handling)
│       ├── Like + reply count
│       └── Click → navigate to comment context
└── PaginationControls
```

---

## 6. Verification Checklist

- [ ] Follow/unfollow user from profile page
- [ ] Private profile → follow creates pending request
- [ ] Approve/deny follow requests
- [ ] Friends page shows mutual follows
- [ ] Post comment on media item
- [ ] Reply to comment (1 level deep)
- [ ] Like/unlike comment
- [ ] Add/remove reactions (6 emojis)
- [ ] Edit/delete own comment
- [ ] Spoiler toggle on comment
- [ ] Report comment
- [ ] Activity feed shows friend actions
- [ ] Block user hides their content
- [ ] Community comments page shows trending/recent