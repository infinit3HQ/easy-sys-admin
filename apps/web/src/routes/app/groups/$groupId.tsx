import { Link, createFileRoute, useNavigate } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { z } from 'zod'
import type { GroupInput, RunKind } from '@esa/api'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Until } from '#/components/time'
import { GroupForm } from '#/components/group-form'
import { RunsTable } from '#/components/runs-table'
import { Button, ConfirmDialog, ErrorBanner, PageHeader, Panel, StatusDot } from '#/components/ui'
import { useRole } from '../route'

export const Route = createFileRoute('/app/groups/$groupId')({
  params: { parse: (p) => ({ groupId: z.coerce.number().int().positive().parse(p.groupId) }), stringify: (p) => ({ groupId: String(p.groupId) }) },
  loader: ({ context, params }) => context.queryClient.ensureQueryData(q.group(params.groupId)),
  head: ({ loaderData }) => ({ meta: [{ title: `${loaderData?.name ?? 'Group'} · easy-sys-admin` }] }),
  component: GroupPage,
})

function GroupPage() {
  const { groupId } = Route.useParams()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { canAct } = useRole()
  const { data: g } = useSuspenseQuery(q.group(groupId))
  const overview = useQuery(q.overview())
  const members = (overview.data?.servers ?? []).filter((s) => s.groupId === groupId)
  const [confirm, setConfirm] = useState<RunKind | 'delete' | null>(null)

  const save = useMutation({
    mutationFn: (body: GroupInput) => call(api().PUT('/groups/{groupId}', { params: { path: { groupId } }, body })),
    onSuccess: (d) => {
      qc.setQueryData(q.group(groupId).queryKey, d)
      return qc.invalidateQueries({ queryKey: ['groups'] })
    },
  })
  const run = useMutation({
    mutationFn: (kind: RunKind) => call(api().POST('/runs', { body: { kind, groupId } })),
    onSuccess: async (r) => {
      setConfirm(null)
      await navigate({ to: '/app/runs/$runId', params: { runId: r.id } })
    },
  })
  const del = useMutation({
    mutationFn: () => call(api().DELETE('/groups/{groupId}', { params: { path: { groupId } } })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['groups'] })
      await navigate({ to: '/app/groups' })
    },
  })

  const initial: GroupInput = { name: g.name, timezone: g.timezone, clean: g.clean, updates: g.updates }

  return (
    <div>
      <PageHeader kicker={<Link to="/app/groups">Groups</Link>} title={g.name}>
        {canAct && (
          <>
            <Button onClick={() => run.mutate('measure')} busy={run.isPending && run.variables === 'measure'} disabled={g.serverCount === 0}>
              Measure
            </Button>
            <Button onClick={() => setConfirm('clean')} disabled={g.serverCount === 0}>
              Clean now
            </Button>
            <Button variant="primary" onClick={() => setConfirm('update')} disabled={g.serverCount === 0}>
              Update now
            </Button>
          </>
        )}
      </PageHeader>
      <ErrorBanner error={run.error} />

      <div className="grid gap-6 xl:grid-cols-[1fr_1.2fr]">
        <div className="space-y-6">
          <Panel title={`Servers · ${members.length}`}>
            {members.length === 0 ? (
              <p className="text-sm text-muted">No servers yet. Move one here from its settings tab.</p>
            ) : (
              <ul className="space-y-2">
                {members.map((s) => (
                  <li key={s.id}>
                    <Link to="/app/servers/$serverId" params={{ serverId: s.id }} className="flex items-center gap-3 hover:text-amber">
                      <StatusDot status={s.status} />
                      <span className="font-mono text-sm">{s.name}</span>
                      <span className="ml-auto font-mono text-[11px] text-muted">{s.host}</span>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </Panel>
          <Panel title="Next">
            <dl className="space-y-2 font-mono text-xs">
              <div className="flex justify-between">
                <dt className="text-muted">Clean</dt>
                <dd>{g.clean.enabled ? <Until iso={g.nextClean} /> : 'off'}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted">Updates</dt>
                <dd>{g.updates.enabled ? <Until iso={g.nextUpdate} /> : 'off'}</dd>
              </div>
            </dl>
          </Panel>
          <div>
            <h2 className="label mb-3">Recent runs</h2>
            <RunsTable runs={g.lastRuns} />
          </div>
        </div>
        <div>
          <GroupForm key={g.id} initial={initial} submitLabel="Save group" busy={save.isPending} error={save.error} disabled={!canAct} onSubmit={(b) => save.mutate(b)} />
          {canAct && (
            <Button className="mt-6" variant="danger" size="sm" onClick={() => setConfirm('delete')}>
              Delete group
            </Button>
          )}
        </div>
      </div>

      <ConfirmDialog
        open={confirm === 'clean' || confirm === 'update'}
        title={confirm === 'update' ? `Update ${g.name}` : `Clean ${g.name}`}
        body={
          confirm === 'update'
            ? `Installs ${g.updates.mode === 'all' ? 'all pending updates' : 'security updates'} on ${g.serverCount} servers, one at a time${g.updates.rebootIfRequired ? ', rebooting where required' : ''}.`
            : `Runs the group's cleanups on ${g.serverCount} servers now.`
        }
        actionLabel="Start"
        busy={run.isPending}
        error={run.error}
        onConfirm={() => confirm && confirm !== 'delete' && run.mutate(confirm)}
        onClose={() => setConfirm(null)}
      />
      <ConfirmDialog
        open={confirm === 'delete'}
        title={`Delete ${g.name}`}
        danger
        confirmWord={g.name}
        body="Its servers stay, without a group, and stop being cleaned and patched on a schedule."
        actionLabel="Delete"
        busy={del.isPending}
        error={del.error}
        onConfirm={() => del.mutate()}
        onClose={() => setConfirm(null)}
      />
    </div>
  )
}
