import { useQuery } from '@tanstack/react-query'
import { q } from '#/lib/queries'
import { ErrorBanner, Panel } from '#/components/kit'
import { Ago } from '#/components/time'

const stateText: Record<string, string> = {
  reachable: 'Reached from this probe',
  not_reached: 'Not reached from this probe',
  unknown: 'Unknown',
}

export function ReachabilityPanel({ serverId }: { serverId: number }) {
  const result = useQuery(q.reachability(serverId))
  return (
    <Panel title="Reachability evidence" className="xl:col-span-2">
      <p className="text-sm text-muted-foreground">
        These are TCP observations from probes you run. They do not describe
        reachability from the whole internet. Evidence is stale after 15
        minutes.
      </p>
      <ErrorBanner error={result.error} />
      {result.isLoading ? (
        <p className="mt-3 text-sm">Loading probe evidence.</p>
      ) : !result.data?.items.length ? (
        <p className="mt-3 text-sm">
          No probe evidence. Reachability is unknown. Run{' '}
          <code>esa servers probe</code> from a machine whose network you want
          to check.
        </p>
      ) : (
        <ul className="mt-4 divide-y divide-line">
          {result.data.items.map((p) => (
            <li key={`${p.source}-${p.port}`} className="py-3 text-sm">
              <div className="flex flex-wrap justify-between gap-2">
                <span>
                  {p.source} · {p.host} · TCP {p.port}
                </span>
                <span>{p.stale ? 'Unknown, stale' : stateText[p.state]}</span>
              </div>
              <p className="mt-1 text-xs text-muted-foreground">
                Last attempt <Ago iso={p.attemptedAt} /> ·{' '}
                {p.reason.replaceAll('_', ' ')}
              </p>
              {p.lastKnownAt && (
                <p className="mt-1 text-xs text-muted-foreground">
                  Last complete observation:{' '}
                  {stateText[p.lastKnownState ?? 'unknown']} ·{' '}
                  <Ago iso={p.lastKnownAt} />
                </p>
              )}
            </li>
          ))}
        </ul>
      )}
    </Panel>
  )
}
