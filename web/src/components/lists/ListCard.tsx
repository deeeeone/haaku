import { Link } from '@tanstack/react-router'
import {
  List,
  Lock,
  Globe,
  Link2,
  Edit,
  Trash2,
  Share2,
  Heart,
  MessageCircle,
} from 'lucide-react'
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter, CardAction } from '@/components/ui'
import { Badge, Chip } from '@/components/ui'
import { Button } from '@/components/ui'
import { StatCard } from '@/components/ui'
import { Dropdown } from '@/components/ui'
import type { DropdownItem } from '@/design-kit-components/ui/dropdown'
import { cn } from '@/lib/utils'
import type { TraktList } from '@/api/types/lists'

export interface ListCardProps {
  list: TraktList
  onEdit?: (list: TraktList) => void
  onDelete?: (list: TraktList) => void
  onShare?: (list: TraktList) => void
  showActions?: boolean
  className?: string
}

const privacyConfig = {
  public: { label: 'Public', icon: Globe, variant: 'green' as const },
  private: { label: 'Private', icon: Lock, variant: 'red' as const },
  link_only: { label: 'Link Only', icon: Link2, variant: 'glass' as const },
}

const COLORS = ['purple', 'blue', 'green', 'orange', 'pink'] as const

export function ListCard({
  list,
  onEdit,
  onDelete,
  onShare,
  showActions = true,
  className,
}: ListCardProps) {
  const privacy = privacyConfig[list.privacy] ?? privacyConfig.private
  const colorIndex = list.ids.trakt % COLORS.length
  const color = COLORS[colorIndex]

  const actionItems: DropdownItem[] = []
  if (onEdit)
    actionItems.push({
      label: 'Edit',
      icon: <Edit className="size-4" />,
      onSelect: () => onEdit(list),
    })
  if (onShare)
    actionItems.push({
      label: 'Share',
      icon: <Share2 className="size-4" />,
      onSelect: () => onShare(list),
    })
  if (onDelete)
    actionItems.push({
      label: 'Delete',
      icon: <Trash2 className="size-4" />,
      danger: true,
      onSelect: () => onDelete(list),
    })

  return (
    <Link
      to="/lists/$id"
      params={{ id: String(list.ids.trakt) }}
      className="block group"
    >
      <Card
        color={color}
        hoverScale
        className={cn(
          'h-full cursor-pointer transition-shadow hover:shadow-lg',
          className
        )}
      >
        <CardHeader>
          <div className="flex items-start justify-between">
            <div className="flex-1 min-w-0">
              <CardTitle className="text-base font-bold truncate">
                {list.name}
              </CardTitle>
              {list.description && (
                <CardDescription className="mt-1 line-clamp-2">
                  {list.description}
                </CardDescription>
              )}
            </div>
            {showActions && actionItems.length > 0 && (
              <CardAction>
                <div onClick={(e) => e.preventDefault()}>
                  <Dropdown
                    trigger={
                      <Button
                        variant="ghost"
                        size="icon"
                        className="text-sf-ink-muted hover:text-sf-ink"
                      >
                        <List className="size-4" />
                      </Button>
                    }
                    items={actionItems}
                  />
                </div>
              </CardAction>
            )}
          </div>
        </CardHeader>

        <CardContent>
          <div className="flex flex-wrap gap-2">
            <Badge count={list.item_count}>
              {list.item_count} items
            </Badge>
            <Chip variant={privacy.variant}>{privacy.label}</Chip>
          </div>
        </CardContent>

        <CardFooter>
          <div className="flex w-full gap-3">
            <StatCard
              label="Likes"
              value={list.likes}
              icon={Heart}
              badgeColor={color}
              className="flex-1 p-3"
            />
            <StatCard
              label="Comments"
              value={list.comment_count}
              icon={MessageCircle}
              badgeColor={color}
              className="flex-1 p-3"
            />
          </div>
        </CardFooter>
      </Card>
    </Link>
  )
}
