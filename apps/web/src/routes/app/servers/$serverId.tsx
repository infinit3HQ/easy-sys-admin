import { Link, createFileRoute, useNavigate } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { z } from 'zod'
import { orNotFound } from '#/lib/api'
import { q } from '#/lib/queries'
import { Ago } from '#/components/time'
import { Crumb, Notice, PageHeader, StatusBadge } from '#/components/kit'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '#/components/ui/tabs'
import { HostKeyAlert, PendingEnrollment } from '#/components/server/banners'
import { StatusTab } from '#/components/server/status-tab'
import { UpdatesTab } from '#/components/server/updates-tab'
import { ServicesTab } from '#/components/server/services-tab'
import { ContainersTab } from '#/components/server/containers-tab'
import { LogsTab } from '#/components/server/logs-tab'
import { AccessTab, JobsTab, SecurityTab } from '#/components/server/audit-tabs'
import { HistoryTab } from '#/components/server/history-tab'
import { SettingsTab } from '#/components/server/settings-tab'
import { useServerAccess } from '#/components/server/use-access'

const tabs = [
  'status',
  'updates',
  'services',
  'containers',
  'logs',
  'access',
  'security',
  'jobs',
  'history',
  'settings',
] as const
type Tab = (typeof tabs)[number]

export const Route = createFileRoute('/app/servers/$serverId')({
  validateSearch: z.object({
    tab: z.enum(tabs).catch('status').default('status'),
  }),
  params: {
    parse: (p) => ({
      serverId: z.coerce.number().int().positive().parse(p.serverId),
    }),
    stringify: (p) => ({ serverId: String(p.serverId) }),
  },
  loader: ({ context, params }) =>
    orNotFound(context.queryClient.ensureQueryData(q.server(params.serverId))),
  head: ({ loaderData }) => ({
    meta: [
      { title: `${loaderData?.server.name ?? 'Server'} · easy-sys-admin` },
    ],
  }),
  component: ServerPage,
})

function ServerPage() {
  const { serverId } = Route.useParams()
  const { tab } = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })
  const { data } = useSuspenseQuery(q.server(serverId))
  const s = data.server
  const ready = s.enrollment === 'ready' && !s.hostKeyMismatch
  const access = useServerAccess(s)

  return (
    <div>
      <PageHeader
        kicker={
          <>
            <Link to="/app" className="hover:text-ink">
              Board
            </Link>
            <Crumb />
            {s.groupId ? (
              <Link
                to="/app/groups/$groupId"
                params={{ groupId: s.groupId }}
                className="hover:text-ink"
              >
                {s.groupName}
              </Link>
            ) : (
              <span>no group</span>
            )}
          </>
        }
        title={s.name}
        description={s.note || undefined}
      >
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 sm:block sm:text-right">
          <StatusBadge status={s.status} />
          <div className="font-mono text-[11px] text-muted-foreground sm:mt-1">
            {s.host}:{s.port} · seen <Ago iso={s.lastSeenAt} />
          </div>
        </div>
      </PageHeader>

      {!access.isOwn && (
        <Notice className="mb-6">
          Shared by {access.ownerTeam}. You can {s.access} this server. Its
          settings, helper, and host key stay with {access.ownerTeam}.
        </Notice>
      )}
      {s.hostKeyMismatch && (
        <HostKeyAlert server={s} canManage={access.canManage} />
      )}
      {s.enrollment === 'pending' && (
        <PendingEnrollment server={s} canManage={access.canManage} />
      )}

      <Tabs
        value={tab}
        onValueChange={(t) =>
          void navigate({
            search: { tab: t as Tab },
            replace: true,
            resetScroll: false,
          })
        }
      >
        <TabsList aria-label="Server sections">
          {tabs.map((t) => (
            <TabsTrigger key={t} value={t}>
              {t}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value={tab}>
          <TabBody tab={tab} serverId={serverId} ready={ready} />
        </TabsContent>
      </Tabs>
    </div>
  )
}

function TabBody({
  tab,
  serverId,
  ready,
}: {
  tab: Tab
  serverId: number
  ready: boolean
}) {
  const live = [
    'services',
    'containers',
    'logs',
    'access',
    'security',
    'jobs',
  ].includes(tab)
  if (live && !ready) {
    return (
      <Notice>
        This server cannot be reached until it is enrolled and its host key is
        trusted.
      </Notice>
    )
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
