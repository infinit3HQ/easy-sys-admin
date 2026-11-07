import type { MouseEvent } from 'react'

/** A row whose action is the link inside it. */
export const clickableRowClass =
  'cursor-pointer hover:bg-panel-2/70 focus-within:bg-panel-2/70'

/**
 * Follow the link inside a row when the click lands on the row itself.
 * A click on a link or a button keeps that control's own behavior.
 */
export function followRowLink(event: MouseEvent<HTMLElement>) {
  if (event.button !== 0 && event.button !== 1) return
  const target = event.target
  if (!(target instanceof Element)) return
  if (target.closest('a, button, input, textarea, select, label')) return
  const selection = window.getSelection()
  if (
    selection &&
    selection.toString() &&
    event.currentTarget.contains(selection.anchorNode)
  ) {
    return
  }
  const link = event.currentTarget.querySelector('a[href]')
  if (!(link instanceof HTMLAnchorElement)) return
  if (
    event.metaKey ||
    event.ctrlKey ||
    event.shiftKey ||
    event.altKey ||
    event.button === 1
  ) {
    event.preventDefault()
    window.open(link.href, '_blank', 'noopener,noreferrer')
    return
  }
  link.click()
}
