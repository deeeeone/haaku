import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { Page, Section } from '@/components/layout'
import { Button, Banner, Badge } from '@/components/ui'
import { useApi } from '@/api/client'
import { useCreateSmartList, useMySmartLists } from '@/api/hooks/useLists'
import { SmartListEditor } from '@/components/lists/SmartListEditor'
import type { SmartListFilter, SmartList } from '@/api/types/lists'

export const Route = createFileRoute('/lists/smart/new')({
  component: SmartListNewPage,
})

function SmartListNewPage() {
  const api = useApi()
  const createSmartList = useCreateSmartList(api)
  const { data: existingSmartLists, isLoading: smartLoading } =
    useMySmartLists(api)

  const [editorOpen, setEditorOpen] = useState(false)

  const handleSave = (name: string, filter: SmartListFilter) => {
    createSmartList.mutate(
      { name, filter },
      {
        onSuccess: () => {
          setEditorOpen(false)
        },
      }
    )
  }

  return (
    <Page>

      <Section>
        <div className="mb-6">
          <h1 className="font-display text-2xl font-bold text-sf-ink">
            Smart Lists
          </h1>
          <p className="mt-1 text-sm text-sf-ink-muted">
            Dynamic lists that auto-populate based on filters.
          </p>
        </div>

        <div className="mb-6 flex gap-2">
          <Button onClick={() => setEditorOpen(true)}>Create Smart List</Button>
        </div>

        {/* Existing Smart Lists */}
        {smartLoading ? (
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <div
                key={i}
                className="glass h-32 animate-pulse rounded-sf-lg"
              />
            ))}
          </div>
        ) : existingSmartLists && existingSmartLists.length > 0 ? (
          <div className="space-y-3">
            {existingSmartLists.map((sl: SmartList) => (
              <div
                key={sl.ids.trakt}
                className="glass rounded-2xl p-4 flex items-center justify-between"
              >
                <div>
                  <p className="font-medium text-sf-ink">{sl.name}</p>
                  <p className="text-sm text-sf-ink-muted">
                    {sl.item_count} items · Type: {sl.filter.type}
                  </p>
                  <div className="mt-2 flex flex-wrap gap-1">
                    {(sl.filter.genres ?? []).slice(0, 3).map((g) => (
                      <Badge key={g} variant="secondary">
                        {g}
                      </Badge>
                    ))}
                    {(sl.filter.genres ?? []).length > 3 && (
                      <Badge variant="secondary">
                        +{(sl.filter.genres ?? []).length - 3} more
                      </Badge>
                    )}
                  </div>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    // Navigate to smart list detail if we had that route
                  }}
                >
                  View
                </Button>
              </div>
            ))}
          </div>
        ) : (
          <Banner id="banner-b9c03995" type="info">
            <p className="text-sf-ink-muted">
              No smart lists yet. Create one to automatically find content
              matching your filters.
            </p>
          </Banner>
        )}
      </Section>

      {/* Smart List Editor Dialog */}
      <SmartListEditor
        open={editorOpen}
        onOpenChange={setEditorOpen}
        onSave={handleSave}
      />
    </Page>
  )
}