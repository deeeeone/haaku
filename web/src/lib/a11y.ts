import type { KeyboardEvent } from 'react'

/**
 * Keyboard bridge for clickable elements that are not native buttons.
 *
 * Attach as `onKeyDown` next to `role="button"` and `tabIndex={0}`. Enter and
 * Space dispatch a real click, so the existing `onClick` logic runs unchanged
 * and the element is reachable without a mouse.
 */
export function activateOnKey(event: KeyboardEvent<HTMLElement>) {
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    event.currentTarget.click()
  }
}
