import { Link, createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { PlusIcon } from 'lucide-react'
import { q } from '#/lib/queries'
import { Until } from '#/components/time'
import { Empty, PageHeader } from '#/components/kit'
import { Badge } from '#/components/ui/badge'
import { Button } from '#/components/ui/button'
import { useRole } from '../route'

export const Route = createFileRoute('/app/groups/')({
  loader: ({ context }) => context.queryClient.ensureQueryData(q.groups()),
  head: () => ({ meta: [{ title: 'Groups · easy-sys-admin' }] }),
  component: Groups,
})

function Groups() {
  const { data } = useSuspenseQuery(q.groups())
  const { canAct } = useRole()
  const add = canAct && (
    <Button asChild>
      <Link to="/app/groups/new">
        <PlusIcon /> New group
      </Link>
    </Button>
  )
  return (
    <div>
      <PageHeader kicker="Policy" title="Groups">
        {add}
      </PageHeader>
      {data.items.length === 0 ? (
        <Empty title="No groups yet" action={add}>
          A group is a set of servers that share when they are cleaned and patched. Make one, then move servers into it.
        </Empty>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {data.items.map((g, i) => (
            <Link
              key={g.id}
              to="/app/groups/$groupId"
              params={{ groupId: g.id }}
              className="panel group block animate-rise p-5 transition-colors hover:border-amber/60 active:bg-panel-2"
              style={{ animationDelay: `${i * 50}ms` }}
            >
              <div className="flex items-baseline justify-between gap-3">
                <h2 className="min-w-0 truncate font-display text-3xl font-bold uppercase group-hover:text-amber">{g.name}</h2>
                <span className="shrink-0 font-mono text-xs text-muted-foreground">{g.serverCount} servers</span>
              </div>
              <dl className="mt-4 space-y-2.5 font-mono text-xs">
                <Line label="Clean" on={g.clean.enabled} detail={`${g.clean.days.join(' ')} ${g.clean.time}`} next={g.nextClean} />
                <Line
                  label={g.updates.mode === 'all' ? 'Updates (all)' : 'Security updates'}
                  on={g.updates.enabled}
                  detail={`${g.updates.days.join(' ')} ${g.updates.time}`}
                  next={g.nextUpdate}
                />
              </dl>
              <div className="mt-4 font-mono text-[10px] text-faint">{g.timezone}</div>
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}

function Line({ label, on, detail, next }: { label: string; on: boolean; detail: string; next?: string | null }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <dt className="text-ink-2">{label}</dt>
      <dd className="min-w-0 truncate text-right">
        {on ? (
          <span className="text-ink">
            {detail} · <Until iso={next} />
          </span>
        ) : (
          <Badge variant="outline">off</Badge>
        )}
      </dd>
    </div>
  )
}
