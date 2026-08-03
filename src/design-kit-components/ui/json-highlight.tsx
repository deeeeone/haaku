import { useMemo } from 'react'
import { cn } from '../../lib/utils'

/** Token colors follow the sf accent palette: keys cyan, strings green, numbers purple, null pink. */
const jsonTokenClasses: Record<string, string> = {
  key: 'text-sf-accent-2', // cyan
  string: 'text-sf-accent-4', // green
  number: 'text-sf-accent', // purple
  null: 'text-sf-accent-3', // pink, muted for null
  punctuation: 'text-sf-ink-hint',
}

function tokenize(json: string): { type: string; value: string }[] {
  const tokens: { type: string; value: string }[] = []
  let i = 0
  while (i < json.length) {
    const rest = json.slice(i)
    const keyMatch = rest.match(/^"([^"\\]*(?:\\.[^"\\]*)*)"\s*:/)
    const stringMatch = rest.match(/^"([^"\\]*(?:\\.[^"\\]*)*)"/)
    const numberMatch = rest.match(/^-?\d+/)
    const nullMatch = rest.match(/^null\b/)
    const punctMatch = rest.match(/^[{}[\],:\s\n]+/)

    if (keyMatch) {
      tokens.push({ type: 'key', value: keyMatch[0] })
      i += keyMatch[0].length
    } else if (punctMatch) {
      tokens.push({ type: 'punctuation', value: punctMatch[0] })
      i += punctMatch[0].length
    } else if (numberMatch) {
      tokens.push({ type: 'number', value: numberMatch[0] })
      i += numberMatch[0].length
    } else if (nullMatch) {
      tokens.push({ type: 'null', value: nullMatch[0] })
      i += nullMatch[0].length
    } else if (stringMatch) {
      tokens.push({ type: 'string', value: stringMatch[0] })
      i += stringMatch[0].length
    } else {
      tokens.push({ type: 'punctuation', value: rest[0] ?? '' })
      i += 1
    }
  }
  return tokens
}

export interface JsonHighlightProps {
  json: string
}

export function JsonHighlight({ json }: JsonHighlightProps) {
  const tokens = useMemo(() => tokenize(json), [json])

  return (
    <>
      {tokens.map((t, idx) => (
        <span
          key={idx}
          className={cn(
            jsonTokenClasses[t.type] ?? 'text-sf-ink',
            'overflow-x-auto wrap-anywhere whitespace-break-spaces'
          )}
        >
          {t.value}
        </span>
      ))}
    </>
  )
}
