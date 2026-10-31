import { useEffect, useState } from 'react'
import { ago, dateTime, until } from '#/lib/format'

/**
 * The current time, refreshed every 30 seconds. Server and browser render
 * times at different moments and often in different time zones, so time
 * text opts out of hydration checks and settles on the client.
 */
function useNow(): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    setNow(Date.now())
    const t = setInterval(() => setNow(Date.now()), 30_000)
    return () => clearInterval(t)
  }, [])
  return now
}

/** "34s ago", "3h ago", "never". */
export function Ago({ iso }: { iso: string | null | undefined }) {
  const now = useNow()
  return (
    <time dateTime={iso ?? undefined} title={iso ?? undefined} suppressHydrationWarning>
      {ago(iso, now)}
    </time>
  )
}

/** "in 3h 20m". */
export function Until({ iso }: { iso: string | null | undefined }) {
  const now = useNow()
  return (
    <time dateTime={iso ?? undefined} title={iso ?? undefined} suppressHydrationWarning>
      {until(iso, now)}
    </time>
  )
}

/** A date and time in the viewer's locale and time zone. */
export function When({ iso }: { iso: string | null | undefined }) {
  return (
    <time dateTime={iso ?? undefined} suppressHydrationWarning>
      {dateTime(iso)}
    </time>
  )
}
