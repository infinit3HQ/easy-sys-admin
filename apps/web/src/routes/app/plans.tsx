import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { q } from '#/lib/queries'
import { Empty, ErrorBanner, Loading, PageHeader } from '#/components/kit'
import { PlanCard } from '#/components/plan-card'
import type { PlanNames } from '#/components/plan-card'
import { useRole } from './route'

export const Route = createFileRoute('/app/plans')({
  loader: ({ context }) => context.queryClient.ensureQueryData(q.plans()),
  head: () => ({ meta: [{ title: 'Plans · easy-sys-admin' }] }),
  component: Plans,
})

function Plans() {
  const { canAct } = useRole()
  const r = useQuery(q.plans())
  const groups = useQuery(q.groups())
  const fleet = useQuery(q.overview())
  const items = r.data?.items ?? []
  const names: PlanNames = {
    group: (id) =>
      groups.data?.items.find((g) => g.id === id)?.name ?? `#${id}`,
    server: (id) =>
      fleet.data?.servers.find((s) => s.id === id)?.name ?? `#${id}`,
  }
  return (
    <div>
      <PageHeader
        kicker="Maintenance"
        title="Plans"
        description="A plan is a typed update, cleanup, or measure on a fixed list of servers. A person, the CLI, or an agent can propose one. Only a person signed in here can approve it, within 15 minutes of signing in. The approval lasts 30 minutes. Approving does not change a server; running the plan does."
      />
      {r.isLoading ? (
        <Loading />
      ) : r.error ? (
        <ErrorBanner error={r.error} />
      ) : items.length === 0 ? (
        <Empty title="No plans">
          Propose one from a group that requires a plan, with{' '}
          <code>esa plans propose</code>, or from an agent with an MCP token.
        </Empty>
      ) : (
        <ul className="grid gap-3">
          {items.map((p) => (
            <li key={p.id}>
              <PlanCard plan={p} names={names} canAct={canAct} />
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
