import { createFileRoute } from '@tanstack/react-router'
import { CalendarDays, ChevronLeft, ChevronRight } from 'lucide-react'
import { useState } from 'react'
import { type CalendarTarget, useCalendar } from '@/api/hooks/useCalendar'
import { Page, Section } from '@/components/layout'
import { CalendarEntry } from '@/components/social/CalendarEntry'
import {
  Button,
  FilterPill,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui'
import { cn } from '@/lib/utils'

type EpisodeType = 'all' | 'new' | 'premieres' | 'finale'

const TARGETS: { value: CalendarTarget; label: string }[] = [
  { value: 'my', label: 'My Shows' },
  { value: 'all', label: 'All Shows' },
  { value: 'friends', label: 'Friends' },
  { value: 'favorites', label: 'Favorites' },
  { value: 'watched', label: 'Watched' },
]

function getMonday(d: Date): Date {
  const date = new Date(d)
  const day = date.getDay()
  const diff = date.getDate() - day + (day === 0 ? -6 : 1)
  date.setDate(diff)
  return date
}

function formatDateStr(d: Date): string {
  return d.toISOString().split('T')[0]
}

function formatWeekRange(start: Date): string {
  const end = new Date(start)
  end.setDate(start.getDate() + 6)

  const startMonth = start.toLocaleString('default', { month: 'short' })
  const endMonth = end.toLocaleString('default', { month: 'short' })
  const startDay = start.getDate()
  const endDay = end.getDate()
  const year = end.getFullYear()

  if (startMonth === endMonth) {
    return `${startMonth} ${startDay} – ${endDay}, ${year}`
  }
  return `${startMonth} ${startDay} – ${endMonth} ${endDay}, ${year}`
}

const DAY_NAMES = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

export const Route = createFileRoute('/calendar')({
  component: CalendarPage,
})

function CalendarPage() {
  const today = new Date()
  const [weekStart, setWeekStart] = useState<Date>(getMonday(today))
  const [target, setTarget] = useState<CalendarTarget>('my')
  const [type, setType] = useState<EpisodeType>('all')
  const [days, setDays] = useState(7)

  const startDate = formatDateStr(weekStart)

  const { data: episodes = [], isLoading } = useCalendar({
    target,
    startDate,
    days,
    type: type === 'all' ? undefined : type,
  })

  // Group episodes by day
  const groupedByDay: Record<string, typeof episodes> = {}
  for (const ep of episodes) {
    const day = ep.episode.first_aired?.split('T')[0] ?? ''
    if (!groupedByDay[day]) groupedByDay[day] = []
    groupedByDay[day].push(ep)
  }

  const goToPrevWeek = () => {
    const prev = new Date(weekStart)
    prev.setDate(prev.getDate() - 7)
    setWeekStart(prev)
  }

  const goToNextWeek = () => {
    const next = new Date(weekStart)
    next.setDate(next.getDate() + 7)
    setWeekStart(next)
  }

  const goToToday = () => {
    setWeekStart(getMonday(today))
  }

  // Build day headers
  const dayHeaders: Date[] = []
  for (let i = 0; i < days; i++) {
    const d = new Date(weekStart)
    d.setDate(d.getDate() + i)
    dayHeaders.push(d)
  }

  return (
    <Page>
      <Section>
        {/* Header with navigation */}
        <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h1 className="font-display text-2xl font-bold text-sf-ink">
              Calendar
            </h1>
            <p className="mt-1 text-sm text-sf-ink-muted">
              {formatWeekRange(weekStart)}
            </p>
          </div>

          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" onClick={goToToday}>
              Today
            </Button>
            <div className="flex rounded-full border border-white/10">
              <button
                type="button"
                onClick={goToPrevWeek}
                className="rounded-l-full p-1.5 text-sf-ink-muted transition-colors hover:bg-white/10 hover:text-sf-ink"
              >
                <ChevronLeft className="size-4" />
              </button>
              <button
                type="button"
                onClick={goToNextWeek}
                className="rounded-r-full p-1.5 text-sf-ink-muted transition-colors hover:bg-white/10 hover:text-sf-ink"
              >
                <ChevronRight className="size-4" />
              </button>
            </div>
          </div>
        </div>

        {/* Target selector */}
        <div className="mb-4 flex gap-1 flex-wrap">
          {TARGETS.map((t) => (
            <FilterPill
              key={t.value}
              active={target === t.value}
              onClick={() => setTarget(t.value)}
            >
              {t.label}
            </FilterPill>
          ))}
        </div>

        {/* Type filter */}
        <div className="mb-6 flex items-center gap-3">
          <div className="flex gap-1 flex-wrap">
            {(['all', 'new', 'premieres', 'finale'] as EpisodeType[]).map(
              (f) => (
                <FilterPill
                  key={f}
                  active={type === f}
                  onClick={() => setType(f)}
                >
                  {f === 'all'
                    ? 'All'
                    : f === 'new'
                      ? 'New Shows'
                      : f === 'premieres'
                        ? 'Season Premieres'
                        : 'Season Finales'}
                </FilterPill>
              )
            )}
          </div>

          <Select
            value={String(days)}
            onValueChange={(v) => setDays(Number(v))}
          >
            <SelectTrigger className="w-24">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="7">Week</SelectItem>
              <SelectItem value="14">2 Weeks</SelectItem>
              <SelectItem value="30">Month</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {/* Calendar grid */}
        {isLoading ? (
          <div className="space-y-4">
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="animate-pulse rounded-[--sf-radius] p-3">
                <div className="mb-2 h-5 w-24 rounded bg-white/10" />
                <div className="flex gap-3">
                  <div className="h-14 w-10 rounded bg-white/5" />
                  <div className="flex-1 space-y-2">
                    <div className="h-4 w-2/3 rounded bg-white/10" />
                    <div className="h-3 w-1/2 rounded bg-white/5" />
                  </div>
                </div>
              </div>
            ))}
          </div>
        ) : episodes.length === 0 ? (
          <div className="glass flex flex-col items-center gap-4 rounded-[var(--sf-radius-lg)] px-6 py-14 text-center">
            <span className="grid size-14 place-items-center rounded-full bg-sf-accent/12 text-sf-accent-2">
              <CalendarDays className="size-6" />
            </span>
            <h3 className="font-display text-xl font-bold text-sf-ink">
              No episodes this week
            </h3>
            <p className="text-sm text-sf-ink-muted">
              Nothing airing this period. Try a different week or filter.
            </p>
          </div>
        ) : (
          <div className="space-y-6">
            {dayHeaders.map((day) => {
              const dayKey = formatDateStr(day)
              const dayEpisodes = groupedByDay[dayKey] ?? []
              const isToday = formatDateStr(today) === dayKey

              return (
                <div key={dayKey}>
                  {/* Day header */}
                  <div
                    className={cn(
                      'mb-2 flex items-center gap-2',
                      isToday && 'text-sf-accent-2'
                    )}
                  >
                    <span className="font-display text-sm font-semibold">
                      {DAY_NAMES[day.getDay() === 0 ? 6 : day.getDay() - 1]}
                    </span>
                    <span
                      className={cn(
                        'text-sm',
                        isToday ? 'text-sf-accent-2' : 'text-sf-ink-muted'
                      )}
                    >
                      {day.toLocaleDateString('default', {
                        month: 'short',
                        day: 'numeric',
                      })}
                    </span>
                    {isToday && (
                      <span className="rounded-full bg-sf-accent/20 px-2 py-0.5 text-[10px] font-bold text-sf-accent-2">
                        TODAY
                      </span>
                    )}
                  </div>

                  {/* Episodes */}
                  {dayEpisodes.length === 0 ? (
                    <p className="pl-1 text-xs text-sf-ink-muted">
                      No episodes
                    </p>
                  ) : (
                    <div className="grid gap-1 sm:grid-cols-2 lg:grid-cols-3">
                      {dayEpisodes.map((ep, i) => (
                        <CalendarEntry
                          key={`${ep.episode.ids.trakt}-${i}`}
                          item={ep}
                        />
                      ))}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </Section>
    </Page>
  )
}
