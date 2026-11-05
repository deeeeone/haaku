import { Search, X } from 'lucide-react'
import * as React from 'react'

import { cn } from '../../lib/utils'

export type SearchBarProps = React.ComponentPropsWithoutRef<'input'> & {
  onClear?: () => void
  inputClassName?: string
}

export const SearchBar = React.forwardRef<HTMLInputElement, SearchBarProps>(
  ({ className, onClear, inputClassName, ...rest }, ref) => {
    const [value, setValue] = React.useState(
      (rest.defaultValue as string) ?? ''
    )

    return (
      <div
        className={cn(
          'glass group flex h-12 items-center gap-3 rounded-full pl-4 pr-2 transition-colors',
          'focus-within:border-sf-line-2',
          className
        )}
      >
        <Search className="size-4 shrink-0 text-sf-ink-hint transition-colors group-focus-within:text-sf-accent-2" />
        <input
          ref={ref}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          className={cn(
            'h-full w-full bg-transparent text-sm text-sf-ink outline-none',
            'placeholder:text-sf-ink-faint',
            inputClassName
          )}
          {...rest}
        />
        {value && (
          <button
            type="button"
            aria-label="Clear search"
            onClick={() => {
              setValue('')
              onClear?.()
            }}
            className="flex size-7 shrink-0 cursor-pointer items-center justify-center rounded-full text-sf-ink-faint transition-colors hover:bg-sf-panel-2 hover:text-sf-ink"
          >
            <X className="size-3.5" />
          </button>
        )}
      </div>
    )
  }
)
SearchBar.displayName = 'SearchBar'
