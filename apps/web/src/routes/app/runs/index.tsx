import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { q } from '#/lib/queries'
import { RunsTable } from '#/components/runs-table'
import { PageHeader } from '#/components/kit'

export const Route = createFileRoute('/app/runs/')({
  loader: ({ context }) => context.queryClient.ensureQueryData(q.runs({ limit: 100 })),
  head: () => ({ meta: [{ title: 'Runs · easy-sys-admin' }] }),
  component: Runs,
})

function Runs() {
  const { data } = useSuspenseQuery(q.runs({ limit: 100 }))
  return (
    <div>
      <PageHeader kicker="History" title="Runs" />
      <RunsTable runs={data.items} empty="Nothing has run yet" />
    </div>
  )
}
