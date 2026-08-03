import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import * as React from 'react'

const buttonVariants = cva(
  [
    'inline-flex h-full cursor-pointer items-center justify-center gap-2 whitespace-nowrap font-semibold',
    'sf-btn-press outline-none shrink-0',
    'focus-visible:ring-2 focus-visible:ring-sf-accent/70 focus-visible:ring-offset-2 focus-visible:ring-offset-sf-bg',
    'disabled:cursor-not-allowed disabled:pointer-events-none disabled:opacity-50',
    '[&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*="size-"])]:size-4',
  ].join(' '),
  {
    variants: {
      variant: {
        primary: [
          'sf-sheen text-white',
          'bg-linear-to-r from-sf-accent to-sf-accent-2',
          'shadow-[0_8px_30px_rgba(140,107,255,0.32)]',
          'hover:shadow-[0_12px_44px_rgba(140,107,255,0.5)]',
        ],
        glass: [
          'glass text-sf-ink-muted',
          'hover:text-sf-ink hover:bg-sf-panel-2 hover:border-sf-line-2',
        ],
        ghost: 'text-sf-ink-muted hover:text-sf-ink hover:bg-sf-panel',
        danger: 'bg-sf-brand-red text-white hover:bg-[#ff2d2d]',
      },
      size: {
        default: 'rounded-full px-5 py-2.5 text-sm',
        sm: 'rounded-full px-3.5 py-1.5 text-xs',
        lg: 'rounded-full px-7 py-3.5 text-base',
        icon: 'aspect-square rounded-full p-2',
      },
    },
    defaultVariants: {
      variant: 'primary',
      size: 'default',
    },
  }
)

type ButtonVariants = VariantProps<typeof buttonVariants>

export type ButtonProps = Omit<
  React.ComponentPropsWithoutRef<'button'>,
  keyof ButtonVariants
> &
  ButtonVariants & {
    asChild?: boolean
    /** Toggle between two icons using t-icon-swap cross-fade */
    iconA?: React.ReactNode
    iconB?: React.ReactNode
    toggled?: boolean
    /** Swap button text with blur animation (t-text-swap) */
    swapText?: string
    /** Show a spinning loader in place of the label */
    loading?: boolean
    /** Show success check after action */
    success?: boolean
  }

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  (props, ref) => {
    const {
      className,
      variant,
      size,
      asChild = false,
      iconA,
      iconB,
      toggled = false,
      swapText,
      loading = false,
      success = false,
      children,
      ...rest
    } = props
    const Comp = asChild ? Slot : 'button'
    const textRef = React.useRef<HTMLSpanElement>(null)

    React.useEffect(() => {
      if (swapText && textRef.current) {
        textRef.current.classList.add('is-exit')
        const dur = 150
        const t = setTimeout(() => {
          if (textRef.current) {
            textRef.current.textContent = swapText
            textRef.current.classList.remove('is-exit')
            textRef.current.classList.add('is-enter-start')
            void textRef.current.offsetHeight
            textRef.current.classList.remove('is-enter-start')
          }
        }, dur)
        return () => clearTimeout(t)
      }
    }, [swapText])

    return (
      <Comp
        ref={ref}
        className={buttonVariants({ variant, size, className })}
        {...rest}
      >
        {success ? (
          <span className="t-success-check" data-state="in" aria-hidden="true">
            <svg
              viewBox="0 0 24 24"
              fill="none"
              width={16}
              height={16}
              aria-hidden="true"
            >
              <path
                d="M5 13l4 4L19 7"
                stroke="currentColor"
                strokeWidth="2.5"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
          </span>
        ) : loading ? (
          <span className="inline-flex items-center gap-2">
            <svg
              viewBox="0 0 24 24"
              fill="none"
              width={16}
              height={16}
              className="animate-spin"
              aria-hidden="true"
            >
              <circle
                cx="12"
                cy="12"
                r="9"
                stroke="currentColor"
                strokeWidth="2.5"
                opacity="0.25"
              />
              <path
                d="M21 12a9 9 0 0 0-9-9"
                stroke="currentColor"
                strokeWidth="2.5"
                strokeLinecap="round"
              />
            </svg>
            {children}
          </span>
        ) : iconA && iconB ? (
          <span className="t-icon-swap" data-state={toggled ? 'b' : 'a'}>
            <span className="t-icon" data-icon="a">
              {iconA}
            </span>
            <span className="t-icon" data-icon="b">
              {iconB}
            </span>
          </span>
        ) : swapText ? (
          <span ref={textRef} className="t-text-swap">
            {children}
          </span>
        ) : (
          children
        )}
      </Comp>
    )
  }
)
Button.displayName = 'Button'

export { Button, buttonVariants }
