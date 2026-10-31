import { Link, createFileRoute, useNavigate } from '@tanstack/react-router'
import {
  useMutation,
  useQuery,
  useQueryClient,
  useSuspenseQuery,
} from '@tanstack/react-query'
import { useState } from 'react'
import { z } from 'zod'
import {
  BrushCleaningIcon,
  DownloadIcon,
  GaugeIcon,
  Trash2Icon,
  UnlinkIcon,
} from 'lucide-react'
import type { GroupInput, RunKind } from '@esa/api'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Until } from '#/components/time'
import { GroupForm } from '#/components/group-form'
import { RunsTable } from '#/components/runs-table'
import { GroupSharing } from '#/components/group-sharing'
import {
  ConfirmDialog,
  ErrorBanner,
  KeyValue,
  Notice,
  PageHeader,
  Panel,
  StatusDot,
} from '#/components/kit'
import { Button } from '#/components/ui/button'
import { useRole } from '../route'

export const Route = createFileRoute('/app/groups/$groupId')({
  params: {
    parse: (p) => ({
      groupId: z.coerce.number().int().positive().parse(p.groupId),
    }),
    stringify: (p) => ({ groupId: String(p.groupId) }),
  },
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(q.group(params.groupId)),
  head: ({ loaderData }) => ({
    meta: [{ title: `${loaderData?.name ?? 'Group'} · easy-sys-admin` }],
  }),
  component: GroupPage,
})

function GroupPage() {
  const { groupId } = Route.useParams()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { canAct } = useRole()
  const { data: g } = useSuspenseQuery(q.group(groupId))
  const overview = useQuery(q.overview())
  const members = (overview.data?.servers ?? []).filter(
    (s) => s.groupId === groupId,
  )
  const [confirm, setConfirm] = useState<RunKind | 'delete' | 'unshare' | null>(
    null,
  )
  // A group another team shares: its settings stay theirs, runs need operate access.
  const own = g.access === 'own'
  const canRun = canAct && g.access !== 'view'

  const save = useMutation({
    mutationFn: (body: GroupInput) =>
      call(
        api().PUT('/groups/{groupId}', { params: { path: { groupId } }, body }),
      ),
    onSuccess: (d) => {
      qc.setQueryData(q.group(groupId).queryKey, d)
      return qc.invalidateQueries({ queryKey: ['groups'] })
    },
  })
  const run = useMutation({
    mutationFn: (kind: RunKind) =>
      call(api().POST('/runs', { body: { kind, groupId } })),
    onSuccess: async (r) => {
      setConfirm(null)
      await navigate({ to: '/app/runs/$runId', params: { runId: r.id } })
    },
  })
  const del = useMutation({
    mutationFn: () =>
      call(
        api().DELETE('/groups/{groupId}', { params: { path: { groupId } } }),
      ),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['groups'] })
      await navigate({ to: '/app/groups' })
    },
  })
  const unshare = useMutation({
    mutationFn: (shareId: number) =>
      call(
        api().DELETE('/group-shares/{shareId}', {
          params: { path: { shareId } },
        }),
      ),
    onSuccess: async () => {
      setConfirm(null)
      await qc.invalidateQueries({ queryKey: ['groups'] })
      await qc.invalidateQueries({ queryKey: ['overview'] })
      await navigate({ to: '/app/groups' })
    },
  })

  const initial: GroupInput = {
    name: g.name,
    timezone: g.timezone,
    clean: g.clean,
    updates: g.updates,
  }
  const empty = g.serverCount === 0

  return (
    <div>
      <PageHeader
        kicker={
          <Link to="/app/groups" className="hover:text-ink">
            Groups
          </Link>
        }
        title={g.name}
      >
        {canAct && (
          <div className="grid w-full grid-cols-3 gap-2 sm:flex sm:w-auto">
            <Button
              variant="outline"
              onClick={() => run.mutate('measure')}
              busy={run.isPending && run.variables === 'measure'}
              disabled={empty || !canRun}
            >
              <GaugeIcon /> Measure
            </Button>
            <Button
              variant="outline"
              onClick={() => setConfirm('clean')}
              disabled={empty || !canRun}
            >
              <BrushCleaningIcon /> Clean
            </Button>
            <Button
              onClick={() => setConfirm('update')}
              disabled={empty || !canRun}
            >
              <DownloadIcon /> Update
            </Button>
          </div>
        )}
      </PageHeader>
      {!own && (
        <Notice className="mb-6">
          Shared by {g.ownerTeam}. You can {g.access} its servers.{' '}
          {g.access === 'operate' && !canAct
            ? 'Your viewer role keeps you read only. '
            : ''}
          Its settings and schedules stay with {g.ownerTeam}.
        </Notice>
      )}
      <ErrorBanner error={run.error} className="mb-6" />

      <div className="grid gap-6 xl:grid-cols-[1fr_1.2fr]">
        <div className="min-w-0 space-y-6">
          <Panel title={`Servers · ${members.length}`} flush>
            {members.length === 0 ? (
              <p className="p-4 text-sm text-muted-foreground">
                {own
                  ? 'No servers yet. Move one here from its settings tab.'
                  : 'No servers in it right now.'}
              </p>
            ) : (
              <ul className="divide-y divide-line/60">
                {members.map((s) => (
                  <li key={s.id}>
                    <Link
                      to="/app/servers/$serverId"
                      params={{ serverId: s.id }}
                      className="flex items-center gap-3 px-4 py-2.5 transition-colors hover:bg-panel-2 hover:text-amber"
                    >
                      <StatusDot status={s.status} />
                      <span className="min-w-0 truncate font-mono text-sm">
                        {s.name}
                      </span>
                      <span className="ml-auto shrink-0 font-mono text-[11px] text-muted-foreground">
                        {s.host}
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </Panel>
          <Panel title="Next">
            <dl className="space-y-2">
              <KeyValue
                k="Clean"
                v={g.clean.enabled ? <Until iso={g.nextClean} /> : 'off'}
              />
              <KeyValue
                k="Updates"
                v={g.updates.enabled ? <Until iso={g.nextUpdate} /> : 'off'}
              />
            </dl>
          </Panel>
          <section>
            <h2 className="label mb-3">Recent runs</h2>
            <RunsTable runs={g.lastRuns} />
          </section>
        </div>
        <div className="min-w-0">
          <GroupForm
            key={g.id}
            initial={initial}
            submitLabel="Save group"
            busy={save.isPending}
            error={save.error}
            disabled={!canAct || !own}
            onSubmit={(b) => save.mutate(b)}
          />
          {canAct && own && <GroupSharing groupId={g.id} />}
          {canAct && !own && g.shareId != null && (
            <Panel title="Shared with this team" className="mt-6">
              <p className="mb-3 text-xs text-muted-foreground">
                Remove the group and its servers from this team. {g.ownerTeam}{' '}
                keeps them, and needs to send a new link to share them again.
              </p>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setConfirm('unshare')}
              >
                <UnlinkIcon /> Remove from this team
              </Button>
            </Panel>
          )}
          {canAct && own && (
            <Panel title="Danger zone" className="mt-6 border-fail/30">
              <p className="mb-3 text-xs text-muted-foreground">
                Its servers stay, without a group, and stop being cleaned and
                patched on a schedule.
              </p>
              <Button
                variant="destructive"
                size="sm"
                onClick={() => setConfirm('delete')}
              >
                <Trash2Icon /> Delete group
              </Button>
            </Panel>
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
        onConfirm={() =>
          (confirm === 'clean' || confirm === 'update') && run.mutate(confirm)
        }
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
      <ConfirmDialog
        open={confirm === 'unshare'}
        title={`Remove ${g.name} from this team`}
        danger
        body={`This team stops seeing the group and its servers. ${g.ownerTeam ?? 'The owner'} keeps them.`}
        actionLabel="Remove"
        busy={unshare.isPending}
        error={unshare.error}
        onConfirm={() => g.shareId != null && unshare.mutate(g.shareId)}
        onClose={() => setConfirm(null)}
      />
    </div>
  )
}
