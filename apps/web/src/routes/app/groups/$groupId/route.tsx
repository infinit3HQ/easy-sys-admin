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
  const [watch, setWatch] = useState(0)
  const [gate, setGate] = useState(false)
  const fleet = useQuery(q.overview())
  const members = (fleet.data?.servers ?? []).filter(
    (s) => s.groupId === groupId,
  )
  const empty = g.serverCount === 0
  // A group that requires a plan refuses a direct clean or update, so the
  // buttons propose a plan for a person to approve on the Plans page. A
  // plan lives in your own space, so a shared group cannot get one here.
  const needsPlan = g.updates.planRequired
  const canPlan = needsPlan && isOwn
  const propose = useMutation({
    mutationFn: (kind: 'clean' | 'update') =>
      call(
        api().POST('/plans', {
          body:
            kind === 'update'
              ? {
                  kind,
                  groupId,
                  updateMode: g.updates.mode,
                  hold: g.updates.hold,
                  rebootIfRequired: g.updates.rebootIfRequired,
                  canaryServerIds: canaries.length > 0 ? canaries : undefined,
                  observeSeconds:
                    canaries.length > 0 && watch > 0 ? watch : undefined,
                }
              : {
                  kind,
                  groupId,
                  buildCache: g.clean.buildCache,
                  vscode: g.clean.vscode,
                  journal: g.clean.journal,
                  journalSize: g.clean.journalSize,
                },
        }),
      ),
    onSuccess: async () => {
      setConfirm(null)
      await navigate({ to: '/app/plans' })
    },
  })
  const start = needsPlan ? propose : null
  const run = useMutation({
    mutationFn: (kind: RunKind) =>
      call(
        api().POST('/runs', {
          body: {
            kind,
            groupId,
            canaryServerIds:
              kind === 'update' && canaries.length > 0 ? canaries : undefined,
            observeSeconds:
              kind === 'update' && canaries.length > 0 && watch > 0
                ? watch
                : undefined,
            waitAfterCanaries:
              kind === 'update' && canaries.length > 0 && gate
                ? true
                : undefined,
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
            {(!needsPlan || canPlan) && (
              <>
                <Button
                  variant="outline"
                  onClick={() => setConfirm('clean')}
                  disabled={empty}
                >
                  <BrushCleaningIcon /> {needsPlan ? 'Plan clean' : 'Clean'}
                </Button>
                <Button
                  onClick={() => {
                    setCanaries([])
                    setWatch(0)
                    setGate(false)
                    setConfirm('update')
                  }}
                  disabled={empty}
                >
                  <DownloadIcon /> {needsPlan ? 'Plan update' : 'Update'}
                </Button>
              </>
            )}
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
      {needsPlan && canRun && (
        <Notice className="mb-6">
          This group requires an approved plan for cleanups and updates.{' '}
          {canPlan
            ? 'Propose one here, then approve and run it on the '
            : 'Its owner proposes and approves plans on their '}
          <Link to="/app/plans" className="text-amber">
            Plans
          </Link>{' '}
          page.
        </Notice>
      )}
      <ErrorBanner error={run.error} className="mb-6" />
      <Outlet />
      <ConfirmDialog
        open={confirm === 'clean' || confirm === 'update'}
        title={
          start
            ? `Plan ${confirm === 'update' ? 'an update' : 'a clean'} of ${g.name}`
            : `${confirm === 'update' ? 'Update' : 'Clean'} ${g.name}`
        }
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
              {canaries.length > 0 && !needsPlan && (
                <label className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={gate}
                    onChange={(e) => setGate(e.target.checked)}
                  />
                  Wait for me after the canaries. The run pauses until you
                  continue or stop it, and stops by itself after 24 hours.
                </label>
              )}
              {canaries.length > 0 && g.checks.length > 0 && (
                <label className="block text-xs text-muted-foreground">
                  Watch the canaries for
                  <input
                    type="number"
                    min={0}
                    max={600}
                    value={watch}
                    className="mx-2 w-16 rounded border bg-transparent px-2 py-1 font-mono text-ink"
                    onChange={(e) =>
                      setWatch(
                        Math.min(600, Math.max(0, Number(e.target.value) || 0)),
                      )
                    }
                  />
                  seconds. Checks repeat every 15 seconds. 0 checks once.
                </label>
              )}
            </div>
          ) : (
            `Runs the group's cleanups on ${g.serverCount} servers${start ? ' once a person approves the plan' : ' now'}.`
          )
        }
        actionLabel={start ? 'Propose plan' : 'Start'}
        busy={start ? start.isPending : run.isPending}
        error={start ? start.error : run.error}
        onConfirm={() =>
          confirm && (start ? start.mutate(confirm) : run.mutate(confirm))
        }
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
