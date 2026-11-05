// Re-export all design-kit components from local copies
// These live in src/design-kit-components/, adapted from the design-kit workspace

export { ActivityItem } from '@/components/social/ActivityItem'
export { CalendarEntry } from '@/components/social/CalendarEntry'
// Social
export { CommentThread } from '@/components/social/CommentThread'
export { FollowButton } from '@/components/social/FollowButton'
export { NotificationItem } from '@/components/social/NotificationItem'
export { ProgressBar } from '@/components/social/ProgressBar'
export { ReactionPicker } from '@/components/social/ReactionPicker'
export { SpoilerOverlay } from '@/components/social/SpoilerOverlay'
export type { FavoriteButtonProps } from '@/components/sync/FavoriteButton'
export { FavoriteButton } from '@/components/sync/FavoriteButton'
export type { RatingStarsProps } from '@/components/sync/RatingStars'
// Sync
export { RatingStars } from '@/components/sync/RatingStars'
export type { WatchlistButtonProps } from '@/components/sync/WatchlistButton'
export { WatchlistButton } from '@/components/sync/WatchlistButton'
// Shared
export { EmptyState } from '@/design-kit-components/shared/empty-state'
export { ProfilePill } from '@/design-kit-components/shared/profile-pill'
export { WatchProgress } from '@/design-kit-components/shared/watch-progress'
// Display
export {
  Badge,
  badgeVariants,
  IconBadge,
} from '@/design-kit-components/ui/badge'
// Feedback
export { Banner } from '@/design-kit-components/ui/banner'
// Inputs
export { Button, buttonVariants } from '@/design-kit-components/ui/button'
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
export { Chip, chipVariants } from '@/design-kit-components/ui/chip'
export { DetailStat } from '@/design-kit-components/ui/detail-stat'
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
export { Eyebrow } from '@/design-kit-components/ui/eyebrow'
export { FactRow } from '@/design-kit-components/ui/fact-row'
export { FilterPill } from '@/design-kit-components/ui/filter-pill'
export { GenreStrip } from '@/design-kit-components/ui/genre-strip'
export { GlassPill } from '@/design-kit-components/ui/glass-pill'
export { Hero } from '@/design-kit-components/ui/hero'
export { InfoPanel } from '@/design-kit-components/ui/info-panel'
export { Input } from '@/design-kit-components/ui/input'

// Dev
export { JsonHighlight } from '@/design-kit-components/ui/json-highlight'
export { Label } from '@/design-kit-components/ui/label'
export { MiniInfoCard } from '@/design-kit-components/ui/mini-info-card'
export { MobileNav } from '@/design-kit-components/ui/mobile-nav'
export { MoodTile } from '@/design-kit-components/ui/mood-tile'
export { Number } from '@/design-kit-components/ui/number'
// Navigation
export { Pagination } from '@/design-kit-components/ui/pagination'
export { PaginationControls } from '@/design-kit-components/ui/pagination-controls'
// Media
export { PosterCard } from '@/design-kit-components/ui/poster-card'
export { Rail } from '@/design-kit-components/ui/rail'
export { SearchBar } from '@/design-kit-components/ui/search-bar'
export { SectionHead } from '@/design-kit-components/ui/section-head'
export {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/design-kit-components/ui/select'
// Loading
export { ShimmerText } from '@/design-kit-components/ui/shimmer-text'
export { StatCard } from '@/design-kit-components/ui/stat-card'
export { Toaster, toast } from '@/design-kit-components/ui/toast'
export {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/design-kit-components/ui/tooltip'
// Layout
export { Topbar } from '@/design-kit-components/ui/topbar'
