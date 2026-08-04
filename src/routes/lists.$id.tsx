import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import {
  Heart,
  Share2,
  Edit,
  Trash2,
  UserPlus,
  Plus,
  X,
  ArrowUpDown,
  User,
} from 'lucide-react'
import { Page, Section, PosterGrid } from '@/components/layout'
import {
  Button,
  Card,
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  Input,
  Label,
  Chip,
  Badge,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Banner,
  ShimmerText,
  PosterCard,
  Dropdown,
  MiniInfoCard,
  DetailStat,
} from '@/components/ui'
import { useApi } from '@/api/client'
import {
  useListDetail,
  useListItems,
  useAddToList,
  useRemoveFromList,
  useLikeList,
  useUnlikeList,
  useListComments,
  useListCollaborators,
  useInviteCollaborator,
  useRemoveCollaborator,
  useUpdateList,
  useDeleteList,
  useSearchForList,
} from '@/api/hooks/useLists'
import type {
  ListPrivacy,
  ListItem,
  SearchResult,
} from '@/api/types/lists'

export const Route = createFileRoute('/lists/$id')({
  component: ListDetailPage,
})

function ListDetailPage() {
  const { id } = Route.useParams()
  const navigate = Route.useNavigate()
  const api = useApi()

  const { data: list, isLoading: listLoading } = useListDetail(api, id)
  const { data: items, isLoading: itemsLoading } = useListItems(api, id)
  const { data: comments } = useListComments(api, id)
  const { data: collaborators } = useListCollaborators(api, id)

  const likeList = useLikeList(api)
  const unlikeList = useUnlikeList(api)
  const updateList = useUpdateList(api)
  const deleteList = useDeleteList(api)
  const addItems = useAddToList(api)
  const removeItems = useRemoveFromList(api)
  const inviteCollab = useInviteCollaborator(api)
  const removeCollab = useRemoveCollaborator(api)

  const [isLiked, setIsLiked] = useState(false)
  const [addOpen, setAddOpen] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchEnabled, setSearchEnabled] = useState(false)
  const { data: searchResults } = useSearchForList(api, searchQuery, searchEnabled)
  const [inviteOpen, setInviteOpen] = useState(false)
  const [inviteUser, setInviteUser] = useState('')
  const [editOpen, setEditOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [editName, setEditName] = useState('')
  const [editDesc, setEditDesc] = useState('')
  const [editPrivacy, setEditPrivacy] = useState<ListPrivacy>('private')

  const [sortBy, setSortBy] = useState<'rank' | 'title' | 'added'>('rank')

  if (listLoading) {
    return (
      <Page>
        <ShimmerText className="h-8 w-64" />
        <div className="mt-6 grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
          {Array.from({ length: 10 }).map((_, i) => (
            <div key={i} className="glass aspect-[2/3] animate-pulse rounded-sf" />
          ))}
        </div>
      </Page>
    )
  }

  if (!list) {
    return (
      <Page>
        <Banner id="banner-db5d4cce" type="error">
          <p>List not found.</p>
        </Banner>
      </Page>
    )
  }

  const sortedItems = [...(items ?? [])].sort((a, b) => {
    if (sortBy === 'rank') return a.rank - b.rank
    if (sortBy === 'title') {
      const titleA = a.movie?.title ?? a.show?.title ?? ''
      const titleB = b.movie?.title ?? b.show?.title ?? ''
      return titleA.localeCompare(titleB)
    }
    return new Date(b.listed_at).getTime() - new Date(a.listed_at).getTime()
  })

  const handleLike = () => {
    if (isLiked) {
      unlikeList.mutate(id, { onSuccess: () => setIsLiked(false) })
    } else {
      likeList.mutate(id, { onSuccess: () => setIsLiked(true) })
    }
  }

  const handleShare = async () => {
    const url = window.location.href
    try {
      await navigator.clipboard.writeText(url)
    } catch {}
  }

  const handleAddItem = (result: SearchResult) => {
    const traktId = result.movie?.ids.trakt ?? result.show?.ids.trakt
    if (!traktId) return
    const payload =
      result.type === 'movie'
        ? { movies: [{ ids: { trakt: traktId, slug: "" } }] }
        : { shows: [{ ids: { trakt: traktId, slug: "" } }] }
    addItems.mutate(
      { id, payload },
      { onSuccess: () => setAddOpen(false) }
    )
  }

  const handleRemoveItem = (item: ListItem) => {
    const traktId = item.movie?.ids.trakt ?? item.show?.ids.trakt
    if (!traktId) return
    removeItems.mutate({
      id,
      payload: {
        ...(item.type === 'movie' ? { movies: [{ ids: { trakt: traktId } }] } : {}),
        ...(item.type === 'show' ? { shows: [{ ids: { trakt: traktId } }] } : {}),
      },
    })
  }

  const handleInvite = () => {
    if (!inviteUser.trim()) return
    inviteCollab.mutate(
      { id, payload: { username: inviteUser.trim() } },
      { onSuccess: () => { setInviteUser(''); setInviteOpen(false) } }
    )
  }

  const handleEdit = () => {
    if (!editName.trim()) return
    updateList.mutate(
      { id, payload: { name: editName.trim(), description: editDesc.trim() || undefined, privacy: editPrivacy } },
      { onSuccess: () => setEditOpen(false) }
    )
  }

  const handleDelete = () => {
    deleteList.mutate(id, {
      onSuccess: () => navigate({ to: '/lists' }),
    })
  }

  const privacyLabel = {
    public: 'Public',
    private: 'Private',
    link_only: 'Link Only',
  }[list.privacy] ?? 'Private'

  const privacyVariant = {
    public: 'green' as const,
    private: 'red' as const,
    link_only: 'glass' as const,
  }[list.privacy] ?? 'glass' as const

  return (
    <Page>

      {/* Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <Link
            to="/lists"
            className="text-sm text-sf-ink-muted hover:text-sf-ink mb-1 block"
          >
            ← Back to Lists
          </Link>
          <h1 className="font-display text-2xl font-bold text-sf-ink">
            {list.name}
          </h1>
          {list.description && (
            <p className="mt-1 text-sf-ink-muted">{list.description}</p>
          )}
          <div className="mt-2 flex flex-wrap gap-2">
            <Chip variant={privacyVariant}>{privacyLabel}</Chip>
            <Badge>{list.item_count} items</Badge>
          </div>
        </div>

        <div className="flex flex-wrap gap-2">
          <Button
            variant={isLiked ? 'primary' : 'ghost'}
            size="sm"
            onClick={handleLike}
          >
            <Heart className={`size-4 mr-1 ${isLiked ? 'fill-white' : ''}`} />
            {list.likes}
          </Button>
          <Button variant="ghost" size="sm" onClick={handleShare}>
            <Share2 className="size-4 mr-1" />
            Share
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setAddOpen(true)}>
            <Plus className="size-4 mr-1" />
            Add Items
          </Button>
          <Dropdown
            trigger={
              <Button variant="ghost" size="sm">
                <Edit className="size-4 mr-1" />
                Manage
              </Button>
            }
            items={[
              {
                label: 'Edit List',
                icon: <Edit className="size-4" />,
                onSelect: () => {
                  setEditName(list.name)
                  setEditDesc(list.description)
                  setEditPrivacy(list.privacy)
                  setEditOpen(true)
                },
              },
              {
                label: 'Invite Collaborator',
                icon: <UserPlus className="size-4" />,
                onSelect: () => setInviteOpen(true),
              },
              {
                label: 'Delete List',
                icon: <Trash2 className="size-4" />,
                danger: true,
                onSelect: () => setDeleteOpen(true),
              },
            ]}
          />
        </div>
      </div>

      {/* Stats row */}
      <div className="mt-6 grid grid-cols-2 gap-3 sm:grid-cols-3">
        <DetailStat label="Items" value={list.item_count} />
        <DetailStat label="Likes" value={list.likes} />
        <DetailStat label="Comments" value={list.comment_count} />
      </div>

      {/* Items Section */}
      <Section title="Items">
        <div className="flex items-center justify-between mb-4">
          <Select
            value={sortBy}
            onValueChange={(v) => setSortBy(v as typeof sortBy)}
          >
            <SelectTrigger>
              <ArrowUpDown className="size-3.5 mr-1.5" />
              <SelectValue placeholder="Sort by" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="rank">Rank</SelectItem>
              <SelectItem value="title">Title</SelectItem>
              <SelectItem value="added">Recently Added</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {itemsLoading ? (
          <PosterGrid>
            {Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="glass aspect-[2/3] animate-pulse rounded-sf" />
            ))}
          </PosterGrid>
        ) : sortedItems.length === 0 ? (
          <Banner id="banner-4c4aa06f" type="info">
            <p className="text-sf-ink-muted">
              No items in this list yet. Click "Add Items" to get started.
            </p>
          </Banner>
        ) : (
          <PosterGrid>
            {sortedItems.map((item) => {
              const title = item.movie?.title ?? item.show?.title ?? 'Unknown'
              const year = item.movie?.year ?? item.show?.year
              return (
                <div key={item.id} className="relative group">
                  <PosterCard
                    title={title}
                    meta={year ? String(year) : undefined}
                    type={item.type as 'movie' | 'series'}
                  />
                  <button
                    type="button"
                    onClick={() => handleRemoveItem(item)}
                    className="absolute top-2 right-2 size-7 flex items-center justify-center rounded-full bg-black/60 text-white opacity-0 group-hover:opacity-100 transition-opacity hover:bg-red-600"
                  >
                    <X className="size-3.5" />
                  </button>
                  <span className="absolute top-2 left-2 size-6 flex items-center justify-center rounded-full bg-sf-accent text-xs font-bold text-white">
                    {item.rank}
                  </span>
                </div>
              )
            })}
          </PosterGrid>
        )}
      </Section>

      {/* Collaborators Section */}
      {collaborators && collaborators.length > 0 && (
        <Section title="Collaborators">
          <div className="flex flex-wrap gap-3">
            {collaborators.map((c) => (
              <div key={c.user.username} className="flex items-center gap-2">
                <MiniInfoCard
                  icon={User} label={c.user.name || c.user.username}
                  value={c.role === 'owner' ? 'Owner' : 'Collaborator'}
                />
                {c.role !== 'owner' && (
                  <button
                    type="button"
                    onClick={() =>
                      removeCollab.mutate({
                        id,
                        userId: c.user.username,
                      })
                    }
                    className="text-xs text-sf-ink-faint hover:text-red-400"
                  >
                    Remove
                  </button>
                )}
              </div>
            ))}
          </div>
        </Section>
      )}

      {/* Comments Section */}
      {comments && comments.length > 0 && (
        <Section title="Comments">
          <div className="space-y-3">
            {comments.map((comment) => (
              <Card key={comment.id} className="!p-4">
                <div className="flex items-start gap-3">
                  <div className="flex-1">
                    <p className="text-sm font-medium text-sf-ink">
                      {comment.user.name || comment.user.username}
                    </p>
                    <p className="mt-1 text-sm text-sf-ink-muted">
                      {comment.comment}
                    </p>
                    <p className="mt-1 text-xs text-sf-ink-faint">
                      {new Date(comment.created_at).toLocaleDateString()}
                    </p>
                  </div>
                  <Badge variant="secondary">
                    <Heart className="size-3 mr-1" />
                    {comment.likes}
                  </Badge>
                </div>
              </Card>
            ))}
          </div>
        </Section>
      )}

      {/* Add Items Dialog */}
      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Add to "{list.name}"</DialogTitle>
            <DialogDescription>Search for movies and shows to add.</DialogDescription>
          </DialogHeader>
          <div className="mt-2 space-y-4">
            <div className="flex gap-2">
              <Input
                value={searchQuery}
                onChange={(e) => {
                  setSearchQuery(e.target.value)
                  setSearchEnabled(e.target.value.length >= 2)
                }}
                placeholder="Search movies & shows..."
                className="flex-1"
              />
            </div>
            {searchResults && searchResults.length > 0 && (
              <div className="max-h-96 overflow-y-auto space-y-2">
                {searchResults.map((r, i) => {
                  const title = r.movie?.title ?? r.show?.title ?? ''
                  const year = r.movie?.year ?? r.show?.year ?? 0
                  return (
                    <div
                      key={i}
                      className="flex items-center justify-between rounded-xl p-2 hover:bg-sf-panel-2 transition-colors"
                    >
                      <div>
                        <p className="text-sm font-medium text-sf-ink">{title}</p>
                        <p className="text-xs text-sf-ink-muted">
                          {r.type} {year > 0 && `· ${year}`}
                        </p>
                      </div>
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => handleAddItem(r)}
                        disabled={addItems.isPending}
                      >
                        <Plus className="size-3.5 mr-1" />
                        Add
                      </Button>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        </DialogContent>
      </Dialog>

      {/* Invite Collaborator Dialog */}
      <Dialog open={inviteOpen} onOpenChange={setInviteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Invite Collaborator</DialogTitle>
            <DialogDescription>
              Enter the username of the person you want to invite.
            </DialogDescription>
          </DialogHeader>
          <form
            onSubmit={(e) => { e.preventDefault(); handleInvite() }}
            className="mt-2 space-y-4"
          >
            <div>
              <Label className="mb-1.5 block text-xs font-semibold text-sf-ink-muted uppercase tracking-wider">
                Username
              </Label>
              <Input
                value={inviteUser}
                onChange={(e) => setInviteUser(e.target.value)}
                placeholder="username"
              />
            </div>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setInviteOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={!inviteUser.trim() || inviteCollab.isPending}>
                {inviteCollab.isPending ? 'Inviting...' : 'Invite'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Edit List Dialog */}
      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit List</DialogTitle>
            <DialogDescription>Update your list details.</DialogDescription>
          </DialogHeader>
          <form
            onSubmit={(e) => { e.preventDefault(); handleEdit() }}
            className="mt-2 space-y-4"
          >
            <div>
              <Label className="mb-1.5 block text-xs font-semibold text-sf-ink-muted uppercase tracking-wider">
                Name
              </Label>
              <Input
                value={editName}
                onChange={(e) => setEditName(e.target.value)}
              />
            </div>
            <div>
              <Label className="mb-1.5 block text-xs font-semibold text-sf-ink-muted uppercase tracking-wider">
                Description
              </Label>
              <Input
                value={editDesc}
                onChange={(e) => setEditDesc(e.target.value)}
              />
            </div>
            <div>
              <Label className="mb-1.5 block text-xs font-semibold text-sf-ink-muted uppercase tracking-wider">
                Privacy
              </Label>
              <Select
                value={editPrivacy}
                onValueChange={(v) => setEditPrivacy(v as ListPrivacy)}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="public">Public</SelectItem>
                  <SelectItem value="link_only">Link Only</SelectItem>
                  <SelectItem value="private">Private</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setEditOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={updateList.isPending}>
                {updateList.isPending ? 'Saving...' : 'Save'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Delete Confirmation */}
      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete List</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete "{list.name}"? This cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleteOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="ghost"
              className="text-red-400 hover:text-red-300"
              onClick={handleDelete}
              disabled={deleteList.isPending}
            >
              {deleteList.isPending ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Page>
  )
}