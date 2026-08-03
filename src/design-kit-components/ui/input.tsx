import * as React from 'react'

import { cn } from '../../lib/utils'

export type InputProps = Omit<
  React.InputHTMLAttributes<HTMLInputElement>,
  'size'
> & {
  error?: boolean
  errorMessage?: string
  size?: 'md' | 'sm'
}

const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ className, type, error, errorMessage, size = 'md', ...props }, ref) => {
    const shakeRef = React.useRef<HTMLDivElement>(null)

    React.useEffect(() => {
      const el = shakeRef.current
      if (!el) return
      if (error) {
        el.classList.add('is-error')
        el.classList.remove('is-shaking')
        void el.offsetWidth
        el.classList.add('is-shaking')
        const total = 80 * 2 + 60 * 2
        const t = setTimeout(
          () => el.classList.remove('is-shaking'),
          total + 20
        )
        return () => clearTimeout(t)
      }
      el.classList.remove('is-error')
      el.classList.remove('is-shaking')
    }, [error])

    return (
      <div
        className={cn(
          't-input-wrap',
          size === 'sm' ? 'w-auto' : 'w-full',
          className
        )}
      >
        <div
          ref={shakeRef}
          className={cn(
            't-input glass flex items-center rounded-full px-4 transition-colors focus-within:border-sf-line-2',
            size === 'sm'
              ? 'h-7 rounded-full px-2.5'
              : 'h-11 rounded-full px-4',
            error && 'border-sf-brand-red/70'
          )}
        >
          <input
            type={type}
            ref={ref}
            className={cn(
              'h-full w-full bg-transparent outline-none placeholder:text-sf-ink-faint disabled:cursor-not-allowed disabled:opacity-50',
              size === 'sm' ? 'text-xs' : 'text-sm text-sf-ink',
              className
            )}
            {...props}
          />
        </div>
        {errorMessage && (
          <p className="t-error-msg mt-2 pl-4 text-xs text-[#ff7a82]">
            {errorMessage}
          </p>
        )}
      </div>
    )
  }
)
Input.displayName = 'Input'

export { Input }
