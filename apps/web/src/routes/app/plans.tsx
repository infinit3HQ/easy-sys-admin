import { Link, createFileRoute } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Empty, ErrorBanner, Loading, PageHeader } from '#/components/kit'
import { Button } from '#/components/ui/button'
import { Card } from '#/components/ui/card'
import { useRole } from './route'

export const Route = createFileRoute('/app/plans')({
  loader: ({ context }) => context.queryClient.ensureQueryData(q.plans()),
  head: () => ({ meta: [{ title: 'Plans · easy-sys-admin' }] }),
  component: Plans,
})

function Plans() {
  const r = useQuery(q.plans())
  const items = r.data?.items ?? []
  return (
    <div>
      <PageHeader
        kicker="Maintenance"
        title="Plans"
        description="A plan is a typed update, cleanup, or measure. An agent can propose one. Only a person signed in here can approve it, and the approval lasts 30 minutes. Approving does not change a server."
      />
      {r.isLoading ? (
        <Loading />
      ) : r.error ? (
        <ErrorBanner error={r.error} />
      ) : items.length === 0 ? (
        <Empty title="No plans">
          An agent with a token proposes a plan. It shows up here for approval.
        </Empty>
      ) : (
        <ul className="grid gap-3">
          {items.map((p) => (
            <li key={p.id}>
              <Card className="p-4">
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                  <div>
                    <div className="text-sm text-ink">
                      {p.body.kind} · {p.status}
                    </div>
                    <div className="mt-1 font-mono text-[11px] break-all text-faint">
                      {p.fingerprint}
                    </div>
                    {p.reason && (
                      <p className="mt-1 text-xs text-muted-foreground">
                        {p.reason}
                      </p>
                    )}
                    {p.runId != null && (
                      <Link
                        to="/app/runs/$runId"
                        params={{ runId: p.runId }}
                        className="mt-1 inline-block text-xs text-amber"
                      >
                        Run {p.runId}
                      </Link>
                    )}
                  </div>
                  {p.status === 'submitted' && <Approve id={p.id} />}
                </div>
              </Card>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function Approve({ id }: { id: number }) {
  const { canAct } = useRole()
  const qc = useQueryClient()
  const approve = useMutation({
    mutationFn: () =>
      call(
        api().POST('/plans/{planId}/approve', {
          params: { path: { planId: id } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['plans'] }),
  })
  const reject = useMutation({
    mutationFn: () =>
      call(
        api().POST('/plans/{planId}/reject', {
          params: { path: { planId: id } },
          body: { reason: 'not now' },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['plans'] }),
  })
  if (!canAct) return null
  return (
    <div className="flex gap-2">
      <Button
        size="sm"
        variant="outline"
        busy={reject.isPending}
        onClick={() => reject.mutate()}
      >
        Reject
      </Button>
      <Button
        size="sm"
        busy={approve.isPending}
        onClick={() => approve.mutate()}
      >
        Approve
      </Button>
      {(approve.error || reject.error) && (
        <ErrorBanner error={approve.error ?? reject.error} />
      )}
    </div>
  )
}
