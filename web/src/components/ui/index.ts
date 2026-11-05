// Re-export all design-kit components from local copies
// These live in src/design-kit-components/, adapted from the design-kit workspace

// Layout
export { Topbar } from '@/design-kit-components/ui/topbar'
export { Rail } from '@/design-kit-components/ui/rail'
export { MobileNav } from '@/design-kit-components/ui/mobile-nav'
export { SectionHead } from '@/design-kit-components/ui/section-head'
export { InfoPanel } from '@/design-kit-components/ui/info-panel'

// Inputs
export { Button, buttonVariants } from '@/design-kit-components/ui/button'
export { Input } from '@/design-kit-components/ui/input'
export {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/design-kit-components/ui/select'
export { SearchBar } from '@/design-kit-components/ui/search-bar'
export { FilterPill } from '@/design-kit-components/ui/filter-pill'

// Display
export { Badge, badgeVariants, IconBadge } from '@/design-kit-components/ui/badge'
export { Chip, chipVariants } from '@/design-kit-components/ui/chip'
export { GlassPill } from '@/design-kit-components/ui/glass-pill'
export { Label } from '@/design-kit-components/ui/label'
export { Eyebrow } from '@/design-kit-components/ui/eyebrow'
export { Number } from '@/design-kit-components/ui/number'

// Containers
export {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/design-kit-components/ui/card'
export { MoodTile } from '@/design-kit-components/ui/mood-tile'

// Overlays
export {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/design-kit-components/ui/dialog'
export { Dropdown } from '@/design-kit-components/ui/dropdown'
export {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/design-kit-components/ui/tooltip'

// Feedback
export { Banner } from '@/design-kit-components/ui/banner'
export { Toaster, toast } from '@/design-kit-components/ui/toast'

// Media
export { PosterCard } from '@/design-kit-components/ui/poster-card'
export { Hero } from '@/design-kit-components/ui/hero'
export { MiniInfoCard } from '@/design-kit-components/ui/mini-info-card'
export { GenreStrip } from '@/design-kit-components/ui/genre-strip'
export { DetailStat } from '@/design-kit-components/ui/detail-stat'
export { StatCard } from '@/design-kit-components/ui/stat-card'
export { FactRow } from '@/design-kit-components/ui/fact-row'

// Navigation
export { Pagination } from '@/design-kit-components/ui/pagination'
export { PaginationControls } from '@/design-kit-components/ui/pagination-controls'

// Loading
export { ShimmerText } from '@/design-kit-components/ui/shimmer-text'

// Dev
export { JsonHighlight } from '@/design-kit-components/ui/json-highlight'

// Shared
export { EmptyState } from '@/design-kit-components/shared/empty-state'
export { ProfilePill } from '@/design-kit-components/shared/profile-pill'
export { WatchProgress } from '@/design-kit-components/shared/watch-progress'

// Sync
export { RatingStars } from '@/components/sync/RatingStars'
export type { RatingStarsProps } from '@/components/sync/RatingStars'
export { WatchlistButton } from '@/components/sync/WatchlistButton'
export type { WatchlistButtonProps } from '@/components/sync/WatchlistButton'
export { FavoriteButton } from '@/components/sync/FavoriteButton'
export type { FavoriteButtonProps } from '@/components/sync/FavoriteButton'

// Social
export { CommentThread } from '@/components/social/CommentThread'
export { ReactionPicker } from '@/components/social/ReactionPicker'
export { ActivityItem } from '@/components/social/ActivityItem'
export { CalendarEntry } from '@/components/social/CalendarEntry'
export { NotificationItem } from '@/components/social/NotificationItem'
export { FollowButton } from '@/components/social/FollowButton'
export { ProgressBar } from '@/components/social/ProgressBar'
export { SpoilerOverlay } from '@/components/social/SpoilerOverlay'