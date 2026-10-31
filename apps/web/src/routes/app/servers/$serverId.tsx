import { Link, createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { z } from 'zod'
import { q } from '#/lib/queries'
import { Ago } from '#/components/time'
import { PageHeader, StatusBadge, cx } from '#/components/ui'
import { HostKeyAlert, PendingEnrollment } from '#/components/server/banners'
import { StatusTab } from '#/components/server/status-tab'
import { UpdatesTab } from '#/components/server/updates-tab'
import { ServicesTab } from '#/components/server/services-tab'
import { ContainersTab } from '#/components/server/containers-tab'
import { LogsTab } from '#/components/server/logs-tab'
import { AccessTab, JobsTab, SecurityTab } from '#/components/server/audit-tabs'
import { HistoryTab } from '#/components/server/history-tab'
import { SettingsTab } from '#/components/server/settings-tab'

const tabs = ['status', 'updates', 'services', 'containers', 'logs', 'access', 'security', 'jobs', 'history', 'settings'] as const
type Tab = (typeof tabs)[number]

export const Route = createFileRoute('/app/servers/$serverId')({
  validateSearch: z.object({ tab: z.enum(tabs).catch('status').default('status') }),
  params: { parse: (p) => ({ serverId: z.coerce.number().int().positive().parse(p.serverId) }), stringify: (p) => ({ serverId: String(p.serverId) }) },
  loader: ({ context, params }) => context.queryClient.ensureQueryData(q.server(params.serverId)),
  head: ({ loaderData }) => ({ meta: [{ title: `${loaderData?.server.name ?? 'Server'} · easy-sys-admin` }] }),
  component: ServerPage,
})

function ServerPage() {
  const { serverId } = Route.useParams()
  const { tab } = Route.useSearch()
  const { data } = useSuspenseQuery(q.server(serverId))
  const s = data.server
  const ready = s.enrollment === 'ready' && !s.hostKeyMismatch

  return (
    <div>
      <PageHeader
        kicker={
          <span className="flex items-center gap-2">
            <Link to="/app" className="hover:text-ink">
              Board
            </Link>
            <span>/</span>
            <span>{s.groupName ?? 'no group'}</span>
          </span>
        }
        title={s.name}
      >
        <div className="text-right">
          <StatusBadge status={s.status} />
          <div className="mt-1 font-mono text-[11px] text-muted">
            {s.host}:{s.port} · seen <Ago iso={s.lastSeenAt} />
          </div>
        </div>
      </PageHeader>

      {s.hostKeyMismatch && <HostKeyAlert server={s} />}
      {s.enrollment === 'pending' && <PendingEnrollment server={s} />}

      <nav className="mb-6 flex gap-1 overflow-x-auto border-b border-line">
        {tabs.map((t) => (
          <Link
            key={t}
            to="."
            search={{ tab: t }}
            className={cx(
              '-mb-px whitespace-nowrap border-b-2 px-3 py-2 font-mono text-[11px] uppercase tracking-wider',
              tab === t ? 'border-amber text-amber' : 'border-transparent text-muted hover:text-ink',
            )}
          >
            {t}
          </Link>
        ))}
      </nav>

      <TabBody tab={tab} serverId={serverId} ready={ready} />
    </div>
  )
}

function TabBody({ tab, serverId, ready }: { tab: Tab; serverId: number; ready: boolean }) {
  const live = ['services', 'containers', 'logs', 'access', 'security', 'jobs'].includes(tab)
  if (live && !ready) {
    return <p className="font-mono text-sm text-muted">This server cannot be reached until it is enrolled and its host key is trusted.</p>
  }
  switch (tab) {
    case 'status':
      return <StatusTab serverId={serverId} />
    case 'updates':
      return <UpdatesTab serverId={serverId} />
    case 'services':
      return <ServicesTab serverId={serverId} />
    case 'containers':
      return <ContainersTab serverId={serverId} />
    case 'logs':
      return <LogsTab serverId={serverId} />
    case 'access':
      return <AccessTab serverId={serverId} />
    case 'security':
      return <SecurityTab serverId={serverId} />
    case 'jobs':
      return <JobsTab serverId={serverId} />
    case 'history':
      return <HistoryTab serverId={serverId} />
    case 'settings':
      return <SettingsTab serverId={serverId} />
  }
}
