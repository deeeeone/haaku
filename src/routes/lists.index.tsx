import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { Plus, ListPlus } from 'lucide-react'
import { Page, Section } from '@/components/layout'
import { SectionHead, Button, FilterPill, Input, Label } from '@/components/ui'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui'
import { Banner } from '@/components/ui'
import { ListCard } from '@/components/lists/ListCard'
import { useApi } from '@/api/client'
import {
  useMyLists,
  useCollaborations,
  useCreateList,
  useUpdateList,
  useDeleteList,
} from '@/api/hooks/useLists'
import type { TraktList, ListPrivacy } from '@/api/types/lists'

export const Route = createFileRoute('/lists/')({
  component: MyListsPage,
})

function MyListsPage() {
  const api = useApi()
  const { data: myLists, isLoading: myLoading } = useMyLists(api)
  const { data: collaborations, isLoading: collabLoading } = useCollaborations(api)
  const createList = useCreateList(api)
  const updateList = useUpdateList(api)
  const deleteList = useDeleteList(api)

  const [filter, setFilter] = useState<'all' | 'owned' | 'collaborated'>('all')
  const [createOpen, setCreateOpen] = useState(false)
  const [editList, setEditList] = useState<TraktList | null>(null)
  const [deleteConfirm, setDeleteConfirm] = useState<TraktList | null>(null)

  // Create list form state
  const [formName, setFormName] = useState('')
  const [formDesc, setFormDesc] = useState('')
  const [formPrivacy, setFormPrivacy] = useState<ListPrivacy>('private')

  const myListArray = myLists ?? []
  const collabArray = collaborations ?? []

  const displayedLists = (() => {
    if (filter === 'owned') return myListArray
    if (filter === 'collaborated') return collabArray
    return [...myListArray, ...collabArray]
  })()

  const handleCreate = () => {
    if (!formName.trim()) return
    createList.mutate(
      { name: formName.trim(), description: formDesc.trim() || undefined, privacy: formPrivacy },
      {
        onSuccess: () => {
          setCreateOpen(false)
          setFormName('')
          setFormDesc('')
          setFormPrivacy('private')
        },
      }
    )
  }

  const handleEdit = () => {
    if (!editList || !formName.trim()) return
    updateList.mutate(
      { id: editList.ids.trakt, payload: { name: formName.trim(), description: formDesc.trim() || undefined, privacy: formPrivacy } },
      {
        onSuccess: () => {
          setEditList(null)
          setFormName('')
          setFormDesc('')
          setFormPrivacy('private')
        },
      }
    )
  }

  const handleDelete = () => {
    if (!deleteConfirm) return
    deleteList.mutate(deleteConfirm.ids.trakt, {
      onSuccess: () => setDeleteConfirm(null),
    })
  }

  const openEdit = (list: TraktList) => {
    setEditList(list)
    setFormName(list.name)
    setFormDesc(list.description)
    setFormPrivacy(list.privacy)
  }

  const handleShare = async (list: TraktList) => {
    const url = `${window.location.origin}/lists/${list.ids.trakt}`
    try {
      await navigator.clipboard.writeText(url)
    } catch {
      // Fallback
    }
  }

  return (
    <Page>

      <div className="flex items-center justify-between">
        <SectionHead title="My Lists" />
        <div className="flex gap-2">
          <Link to="/lists/smart/new">
            <Button variant="ghost" size="sm">
              <ListPlus className="size-4 mr-1.5" />
              Smart List
            </Button>
          </Link>
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            <Plus className="size-4 mr-1.5" />
            New List
          </Button>
        </div>
      </div>

      {/* Filter pills */}
      <div className="flex gap-2 mb-6">
        <FilterPill
          active={filter === 'all'}
          onClick={() => setFilter('all')}
          count={myListArray.length + collabArray.length}
        >
          All
        </FilterPill>
        <FilterPill
          active={filter === 'owned'}
          onClick={() => setFilter('owned')}
          count={myListArray.length}
        >
          My Lists
        </FilterPill>
        <FilterPill
          active={filter === 'collaborated'}
          onClick={() => setFilter('collaborated')}
          count={collabArray.length}
        >
          Collaborations
        </FilterPill>
      </div>

      {/* Lists Grid */}
      <Section>
        {myLoading || collabLoading ? (
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="glass h-48 animate-pulse rounded-sf-lg" />
            ))}
          </div>
        ) : displayedLists.length === 0 ? (
          <Banner id="banner-197413ac" type="info">
            <p className="text-sf-ink-muted">
              {filter === 'owned'
                ? "You haven't created any lists yet."
                : filter === 'collaborated'
                  ? "You're not collaborating on any lists."
                  : "No lists yet. Create one to get started."}
            </p>
          </Banner>
        ) : (
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {displayedLists.map((list) => (
              <ListCard
                key={list.ids.trakt}
                list={list}
                onEdit={openEdit}
                onDelete={setDeleteConfirm}
                onShare={handleShare}
              />
            ))}
          </div>
        )}
      </Section>

      {/* Create List Dialog */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create List</DialogTitle>
            <DialogDescription>
              Create a new list to organize your favorite movies and shows.
            </DialogDescription>
          </DialogHeader>
          <form
            onSubmit={(e) => { e.preventDefault(); handleCreate() }}
            className="space-y-4 mt-2"
          >
            <div>
              <Label className="mb-1.5 block text-xs font-semibold text-sf-ink-muted uppercase tracking-wider">
                Name
              </Label>
              <Input
                value={formName}
                onChange={(e) => setFormName(e.target.value)}
                placeholder="My Awesome List"
                required
              />
            </div>
            <div>
              <Label className="mb-1.5 block text-xs font-semibold text-sf-ink-muted uppercase tracking-wider">
                Description (optional)
              </Label>
              <Input
                value={formDesc}
                onChange={(e) => setFormDesc(e.target.value)}
                placeholder="A short description..."
              />
            </div>
            <div>
              <Label className="mb-1.5 block text-xs font-semibold text-sf-ink-muted uppercase tracking-wider">
                Privacy
              </Label>
              <Select
                value={formPrivacy}
                onValueChange={(v) => setFormPrivacy(v as ListPrivacy)}
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
              <Button variant="ghost" type="button" onClick={() => setCreateOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={!formName.trim() || createList.isPending}>
                {createList.isPending ? 'Creating...' : 'Create List'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Edit List Dialog */}
      <Dialog open={!!editList} onOpenChange={() => setEditList(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit List</DialogTitle>
            <DialogDescription>Update your list details.</DialogDescription>
          </DialogHeader>
          <form
            onSubmit={(e) => { e.preventDefault(); handleEdit() }}
            className="space-y-4 mt-2"
          >
            <div>
              <Label className="mb-1.5 block text-xs font-semibold text-sf-ink-muted uppercase tracking-wider">
                Name
              </Label>
              <Input
                value={formName}
                onChange={(e) => setFormName(e.target.value)}
                required
              />
            </div>
            <div>
              <Label className="mb-1.5 block text-xs font-semibold text-sf-ink-muted uppercase tracking-wider">
                Description
              </Label>
              <Input
                value={formDesc}
                onChange={(e) => setFormDesc(e.target.value)}
              />
            </div>
            <div>
              <Label className="mb-1.5 block text-xs font-semibold text-sf-ink-muted uppercase tracking-wider">
                Privacy
              </Label>
              <Select
                value={formPrivacy}
                onValueChange={(v) => setFormPrivacy(v as ListPrivacy)}
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
              <Button variant="ghost" type="button" onClick={() => setEditList(null)}>
                Cancel
              </Button>
              <Button type="submit" disabled={!formName.trim() || updateList.isPending}>
                {updateList.isPending ? 'Saving...' : 'Save'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Delete Confirmation Dialog */}
      <Dialog open={!!deleteConfirm} onOpenChange={() => setDeleteConfirm(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete List</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete "{deleteConfirm?.name}"? This cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleteConfirm(null)}>
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