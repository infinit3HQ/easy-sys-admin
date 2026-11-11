import {
  Link,
  Outlet,
  createFileRoute,
  useNavigate,
} from '@tanstack/react-router'
import { useMutation, useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { z } from 'zod'
import { BrushCleaningIcon, DownloadIcon, GaugeIcon } from 'lucide-react'
import type { RunKind } from '@esa/api'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import {
  ConfirmDialog,
  ErrorBanner,
  Notice,
  PageHeader,
} from '#/components/kit'
import { Button } from '#/components/ui/button'
import { useGroupAccess } from '#/components/server/use-access'

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
  component: GroupLayout,
})

function GroupLayout() {
  const { groupId } = Route.useParams()
  const { data: g } = useSuspenseQuery(q.group(groupId))
  const navigate = useNavigate()
  const { isOwn, canManage, canRun } = useGroupAccess(g)
  const [confirm, setConfirm] = useState<Extract<
    RunKind,
    'clean' | 'update'
  > | null>(null)
  const [canaries, setCanaries] = useState<number[]>([])
  const fleet = useQuery(q.overview())
  const members = (fleet.data?.servers ?? []).filter(
    (s) => s.groupId === groupId,
  )
  const empty = g.serverCount === 0
  const run = useMutation({
    mutationFn: (kind: RunKind) =>
      call(
        api().POST('/runs', {
          body: {
            kind,
            groupId,
            canaryServerIds:
              kind === 'update' && canaries.length > 0 ? canaries : undefined,
          },
        }),
      ),
    onSuccess: async (r) => {
      setConfirm(null)
      await navigate({ to: '/app/runs/$runId', params: { runId: r.id } })
    },
  })

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
        {canRun && (
          <div className="grid w-full grid-cols-3 gap-2 sm:flex sm:w-auto">
            <Button
              variant="outline"
              onClick={() => run.mutate('measure')}
              busy={run.isPending && run.variables === 'measure'}
              disabled={empty}
            >
              <GaugeIcon /> Measure
            </Button>
            <Button
              variant="outline"
              onClick={() => setConfirm('clean')}
              disabled={empty}
            >
              <BrushCleaningIcon /> Clean
            </Button>
            <Button
              onClick={() => {
                setCanaries([])
                setConfirm('update')
              }}
              disabled={empty}
            >
              <DownloadIcon /> Update
            </Button>
          </div>
        )}
      </PageHeader>
      <nav
        aria-label="Group sections"
        className="mb-6 flex gap-1 overflow-x-auto border-b"
      >
        <SectionLink to="/app/groups/$groupId" groupId={groupId} exact>
          Overview
        </SectionLink>
        <SectionLink to="/app/groups/$groupId/settings" groupId={groupId}>
          Settings
        </SectionLink>
        {canManage && (
          <SectionLink to="/app/groups/$groupId/sharing" groupId={groupId}>
            Sharing
          </SectionLink>
        )}
      </nav>
      {!isOwn && (
        <Notice className="mb-6">
          Shared by {g.ownerTeam}. You can {g.access} its servers.{' '}
          {g.access === 'operate' && !canRun
            ? 'Your viewer role keeps you read only. '
            : ''}
          Its settings and schedules stay with {g.ownerTeam}.
        </Notice>
      )}
      <ErrorBanner error={run.error} className="mb-6" />
      <Outlet />
      <ConfirmDialog
        open={confirm === 'clean' || confirm === 'update'}
        title={confirm === 'update' ? `Update ${g.name}` : `Clean ${g.name}`}
        body={
          confirm === 'update' ? (
            <div className="space-y-3">
              <p>
                Installs{' '}
                {g.updates.mode === 'all'
                  ? 'all pending updates'
                  : 'security updates'}{' '}
                on {g.serverCount} servers,{' '}
                {g.updates.batchSize > 1
                  ? `${g.updates.batchSize} at a time`
                  : 'one at a time'}
                {g.updates.rebootIfRequired ? ', rebooting where required' : ''}
                .
              </p>
              {members.length > 1 && (
                <fieldset className="space-y-1">
                  <legend className="text-xs text-muted-foreground">
                    Canaries run first. If one fails its checks, the others are
                    skipped.
                  </legend>
                  {members.map((s) => (
                    <label
                      key={s.id}
                      className="flex items-center gap-2 text-sm"
                    >
                      <input
                        type="checkbox"
                        checked={canaries.includes(s.id)}
                        onChange={(e) =>
                          setCanaries(
                            e.target.checked
                              ? [...canaries, s.id]
                              : canaries.filter((id) => id !== s.id),
                          )
                        }
                      />
                      {s.name}
                    </label>
                  ))}
                </fieldset>
              )}
            </div>
          ) : (
            `Runs the group's cleanups on ${g.serverCount} servers now.`
          )
        }
        actionLabel="Start"
        busy={run.isPending}
        error={run.error}
        onConfirm={() => confirm && run.mutate(confirm)}
        onClose={() => setConfirm(null)}
      />
    </div>
  )
}

function SectionLink({
  to,
  groupId,
  exact,
  children,
}: {
  to:
    | '/app/groups/$groupId'
    | '/app/groups/$groupId/settings'
    | '/app/groups/$groupId/sharing'
  groupId: number
  exact?: boolean
  children: string
}) {
  return (
    <Link
      to={to}
      params={{ groupId }}
      activeOptions={{ exact: !!exact }}
      className="border-b-2 border-transparent px-3 py-2 font-mono text-[11px] tracking-wider text-ink-2 uppercase hover:text-ink"
      activeProps={{
        className: 'border-amber! text-amber!',
        'aria-current': 'page',
      }}
    >
      {children}
    </Link>
  )
}
