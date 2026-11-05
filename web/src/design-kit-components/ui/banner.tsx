import { AlertCircle, Info, X } from 'lucide-react'
import { useEffect, useState } from 'react'

import { cn } from '../../lib/utils'

interface BannerProps {
  children: React.ReactNode
  type?: 'error' | 'info'
  /** Unique id used for the localStorage dismiss key. */
  id: string
}

const styles = {
  error:
    'border-sf-brand-red/30 bg-sf-brand-red/10 text-[#ff7a82] [&_svg]:text-[#ff7a82]',
  info: 'border-sf-accent/30 bg-sf-accent/10 text-[#b9a6ff] [&_svg]:text-sf-accent-2',
}

const icons = {
  error: AlertCircle,
  info: Info,
}

/**
 * Self-contained dismissible banner. Dismissal persists in localStorage
 * under `hideBanner-<id>`.
 */
export function Banner({ children, type = 'info', id }: BannerProps) {
  const [hidden, setHidden] = useState(
    () =>
      typeof localStorage !== 'undefined' &&
      !!localStorage.getItem(`hideBanner-${id}`)
  )

  const dismiss = () => {
    setHidden(true)
    localStorage.setItem(`hideBanner-${id}`, 'true')
  }

  useEffect(() => {
    queueMicrotask(() => {
      if (
        typeof localStorage !== 'undefined' &&
        localStorage.getItem(`hideBanner-${id}`)
      ) {
        setHidden(true)
      }
    })
  }, [id])

  if (hidden) return null

  const Icon = icons[type]

  return (
    <div
      className={cn(
        styles[type],
        'relative flex items-center justify-center gap-3 rounded-full border px-5 py-2.5 backdrop-blur-md'
      )}
      role={type === 'error' ? 'alert' : 'status'}
    >
      <div className="flex items-center gap-3">
        <Icon className="h-4 w-4" />
        <div className="text-sm font-medium">{children}</div>
      </div>
      <button
        type="button"
        className="ml-2 flex size-5 cursor-pointer items-center justify-center rounded-full opacity-60 transition-opacity hover:opacity-100"
        onClick={dismiss}
        aria-label="Dismiss"
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  )
}
