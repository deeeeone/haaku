# P5 — Profile & Settings (T6.6)

> **API tags:** Users (14 🔒), API Keys (3 🔒), Webhooks (7 🔒, 1 public), Integrations (3 🔒, 1 public), Notifications (3 🔒), Filters (3 🔒), Notes (5 🔒), Reports (4 🔒)
> **Components:** button, input, select, dialog, dropdown, card, fact-row, detail-stat, stat-card, section-head, banner, toast, tooltip, badge, chip, glass-pill, pagination-controls, shimmer-text, json-highlight

---

## 1. User Profile Page (`/profile/$id`)

### 1.1 API Endpoints
- `GET /v1/users/{id}` — Public profile
- `GET /v1/users/{id}/stats` — User stats
- `GET /v1/users/{id}/lists` — User's public lists
- `GET /v1/users/{id}/followers` — Followers
- `GET /v1/users/{id}/following` — Following
- `POST /v1/users/{id}/follow` / `DELETE` — Follow/unfollow
- `POST /v1/users/{id}/block` / `DELETE` — Block/unblock

### 1.2 UI
```
ProfilePage
├── Hero (avatar, username, display name, join date)
│   ├── StatCard[] (plays, minutes, shows, movies, episodes watched)
│   ├── FollowButton / FollowingButton
│   ├── BlockButton (dropdown: "Block")
│   └── EditButton (only if own profile → /me/settings)
├── SectionHead "Stats"
│   └── StatCard[] (detailed stats from /users/{id}/stats)
├── SectionHead "Public Lists"
│   └── ListCard[]
├── SectionHead "Followers"
│   └── MiniInfoCard[] (avatar + username + follow toggle)
├── SectionHead "Following"
│   └── MiniInfoCard[]
└── If private profile + not following: "This profile is private"
```

---

## 2. Settings Page (`/me/settings`)

### 2.1 API Endpoints
- `GET /v1/users/me` — Current user profile
- `PUT /v1/users/me` — Update profile
- `POST /v1/users/me/avatar` — Upload avatar
- `GET /v1/users/me/settings` — User settings
- `PUT /v1/users/me/settings` — Update settings
- `DELETE /v1/users/me` — Delete account

### 2.2 UI
```
SettingsPage
├── Sidebar
│   ├── "Profile"
│   ├── "Privacy"
│   ├── "Notifications"
│   ├── "API Keys"
│   ├── "Webhooks"
│   ├── "Integrations"
│   ├── "Connected Apps"
│   └── "Danger Zone"
└── Content (tab panel)
    ├── [Profile tab]
    │   ├── Avatar upload (click to upload, preview)
    │   ├── Input (display name)
    │   ├── Input (bio/description)
    │   └── Button "Save"
    ├── [Privacy tab]
    │   ├── Toggle (private profile)
    │   └── Blocked users list
    ├── [Notifications tab]
    │   ├── GET /v1/notifications/settings
    │   └── Toggle[] (per notification type)
    ├── [API Keys tab]
    │   ├── List of keys (masked)
    │   ├── Button "Generate New Key" → shows key ONCE
    │   └── Revoke button (with confirmation)
    ├── [Webhooks tab]
    │   ├── List of webhook endpoints
    │   ├── CreateWebhookDialog
    │   ├── Per endpoint: url, events, active toggle
    │   ├── Test ping button
    │   ├── Rotate secret button
    │   └── Delivery log table
    ├── [Integrations tab]
    │   ├── List of tokens
    │   ├── CreateTokenDialog
    │   └── Revoke button
    └── [Danger Zone]
        ├── Button "Delete Account"
        └── Confirmation dialog (type username to confirm)
```

---

## 3. Webhook Management UI

### 3.1 API Endpoints
- `GET /v1/webhooks` — List endpoints
- `POST /v1/webhooks` — Create (url, events[], secret?)
- `PUT /v1/webhooks/{id}` — Update (url, events, active)
- `DELETE /v1/webhooks/{id}` — Delete
- `POST /v1/webhooks/{id}/rotate-secret` — Rotate secret
- `POST /v1/webhooks/{id}/test` — Send test ping
- `GET /v1/webhooks/{id}/deliveries` — Delivery log

### 3.2 Create Webhook Dialog
```
Dialog: "Create Webhook"
├── Input (URL, https only, validated)
├── MultiSelect (events: history.added, watchlist.updated, scrobble, ...)
├── Input (secret — auto-generated if empty, shown once)
├── Note: "Secret will only be shown ONCE. Store it securely."
└── Button "Create"
```

### 3.3 Delivery Log Table
```
| Event Type | Status  | Attempts | Response Code | Duration | Time |
|------------|---------|----------|---------------|----------|------|
| scrobble   | success | 1        | 200           | 145ms    | ...  |
| history    | failed  | 5        | 502           | 8000ms   | ...  |
| ping       | success | 1        | 200           | 98ms     | ...  |
```

---

## 4. API Key Management

### 4.1 API Endpoints
- `GET /v1/users/me/api_keys` — List keys (masked)
- `POST /v1/users/me/api_keys` — Generate new key
- `DELETE /v1/users/me/api_keys/{key_id}` — Revoke key

### 4.2 UI
```
APIKeysSection
├── List of keys
│   ├── Name + masked key (tk_****abcd)
│   ├── Created date
│   └── Revoke button
├── Button "Generate New Key"
│   └── Dialog: name input → key shown ONCE with copy button
└── Warning: "Store your key securely. It cannot be retrieved later."
```

---

## 5. Personal Notes UI

### 5.1 API Endpoints
- `POST /v1/notes` — Create/upsert note
- `GET /v1/notes/{id}` — Get note
- `PUT /v1/notes/{id}` — Update note
- `DELETE /v1/notes/{id}` — Delete note
- `GET /v1/users/me/notes?page=&limit=&media_type=` — List notes

### 5.2 Where It Appears
- **Media detail pages:** "Add Note" button → inline textarea
- **My Notes page** (`/me/notes`): list of all notes with media context

```
NoteSection
├── "My Note" (textarea)
│   ├── Save button
│   ├── Delete button
│   └── Character count
└── Last edited timestamp
```

---

## 6. Saved Filters UI

### 6.1 API Endpoints
- `GET /v1/users/me/saved_filters/{section}` — List saved filters
- `POST /v1/users/me/saved_filters` — Save filter
- `DELETE /v1/users/me/saved_filters/{id}` — Delete filter

### 6.2 Where It Appears
- Smart list editor: "Save this filter"
- Discovery page: "Save search"
- Lists page: "Save filter"

---

## 7. Notification Inbox

### 7.1 API Endpoints
- `GET /v1/notifications?page=&limit=` — Inbox (unread-first)
- `POST /v1/notifications/read` — Mark read
- `GET /v1/notifications/settings` — Settings

### 7.2 UI
```
NotificationsPage
├── Header "Notifications" + "Mark all read" button
├── NotificationList
│   ├── Unread indicator (blue dot)
│   ├── "User started following you" → link to profile
│   ├── "User commented on your list" → link to comment
│   ├── "Your list was liked" → link to list
│   └── "User replied to your comment" → link to reply
└── PaginationControls
```

---

## 8. Content Reporting

### 8.1 API Endpoints
- `POST /v1/movies/{id}/report` — Report content
- `POST /v1/shows/{id}/report`
- `POST /v1/seasons/{id}/report`
- `POST /v1/episodes/{id}/report`

### 8.2 UI
```
ReportDialog
├── Select (reason: spam, spoiler, offensive, incorrect, duplicate, other)
├── Input (description, optional)
└── Button "Submit Report" → Toast "Report submitted"
```

### 8.3 Where It Appears
- Three-dot menu on media detail pages → "Report"
- Three-dot menu on comments → "Report"
- Three-dot menu on lists → "Report"

---

## 9. Verification Checklist

- [ ] Profile page shows stats, lists, followers
- [ ] Follow/unfollow/block works
- [ ] Settings: update profile, privacy toggle
- [ ] API key: generate (shown once), list (masked), revoke
- [ ] Webhook: create, test ping, view delivery log, rotate secret, delete
- [ ] Integration tokens: create, list, revoke
- [ ] Personal notes: add/edit on media detail page
- [ ] Saved filters: save from smart list editor, load on discovery
- [ ] Notification inbox: unread-first, mark read
- [ ] Report dialog: submit reason + description
- [ ] Account deletion: confirmation required
- [ ] Private profile hidden from non-followers