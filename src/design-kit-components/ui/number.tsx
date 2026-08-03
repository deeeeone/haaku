import { cn } from '../../lib/utils'

interface NumberProps {
  value: number
  /** @deprecated Animation duration is controlled by CSS var --digit-dur */
  duration?: number
  /** @deprecated Use CSS var --digit-stagger for timing */
  delay?: number
  decimals?: number
  formatter?: (value: number) => string
  suffix?: string
  className?: string
}

function formatValue(
  val: number,
  decimals: number,
  formatter?: (value: number) => string
): string {
  if (formatter) return formatter(val)
  if (decimals > 0) return val.toFixed(decimals)
  return Math.round(val).toLocaleString()
}

export function Number({
  value,
  decimals = 0,
  formatter,
  suffix,
  className,
}: NumberProps) {
  const formatted = formatValue(value, decimals, formatter)
  const chars = formatted.split('')

  return (
    <span className={cn('t-digit-group is-animating', className)}>
      {chars.map((ch, i) => (
        <span
          key={`${i}-${value}`}
          className="t-digit"
          data-stagger={
            i === chars.length - 2
              ? '1'
              : i === chars.length - 1
                ? '2'
                : undefined
          }
        >
          {ch}
        </span>
      ))}
      {suffix}
    </span>
  )
}
