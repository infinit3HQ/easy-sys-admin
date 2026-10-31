import { Link, Outlet, createFileRoute, redirect, useRouter } from '@tanstack/react-router'
import { useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { ApiError, api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { cx } from '#/components/ui'

export const Route = createFileRoute('/app')({
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData(q.me())
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        throw redirect({ to: '/login', search: { return_to: location.href } })
      }
      throw e
    }
  },
  component: AppShell,
})

const nav = [
  { to: '/app', label: 'Board', exact: true },
  { to: '/app/groups', label: 'Groups' },
  { to: '/app/runs', label: 'Runs' },
  { to: '/app/alerts', label: 'Alerts' },
  { to: '/app/activity', label: 'Activity' },
  { to: '/app/settings', label: 'Settings' },
] as const

function AppShell() {
  const { data: me } = useSuspenseQuery(q.me())
  const qc = useQueryClient()
  const router = useRouter()

  return (
    <div className="grid min-h-screen grid-rows-[auto_1fr] lg:grid-cols-[220px_1fr] lg:grid-rows-1">
      <aside className="flex items-center gap-4 border-b border-line bg-bg-2/80 px-4 py-3 lg:sticky lg:top-0 lg:h-screen lg:flex-col lg:items-stretch lg:border-b-0 lg:border-r lg:px-3 lg:py-5">
        <Link to="/" className="flex items-center gap-2 px-2">
          <img src="/favicon.svg" alt="" className="size-6" />
          <span className="font-display text-lg font-bold uppercase leading-none tracking-wide">esa</span>
        </Link>
        <nav className="flex flex-1 gap-1 overflow-x-auto lg:mt-8 lg:flex-col">
          {nav.map((n) => (
            <Link
              key={n.to}
              to={n.to}
              activeOptions={{ exact: 'exact' in n }}
              className="whitespace-nowrap rounded px-3 py-2 font-mono text-xs uppercase tracking-wider text-ink-2 hover:bg-panel-2 hover:text-ink"
              activeProps={{ className: '!bg-amber/10 !text-amber' }}
            >
              {n.label}
            </Link>
          ))}
        </nav>
        <div className="hidden space-y-3 border-t border-line px-2 pt-4 lg:block">
          {me.accounts.length > 1 ? (
            <select
              aria-label="Account"
              className="input !py-1.5 font-mono !text-xs"
              value={me.account.id}
              onChange={async (e) => {
                await call(api().PUT('/me/account', { body: { accountId: Number(e.target.value) } }))
                qc.clear()
                await router.invalidate()
              }}
            >
              {me.accounts.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
            </select>
          ) : (
            <div className="truncate font-mono text-xs text-ink-2">{me.account.name}</div>
          )}
          <div className="flex items-center justify-between gap-2">
            <div className="min-w-0">
              <div className="truncate text-sm text-ink">{me.user.name || me.user.email}</div>
              <div className={cx('font-mono text-[10px] uppercase tracking-wider', me.account.role === 'viewer' ? 'text-muted' : 'text-amber')}>
                {me.account.role}
              </div>
            </div>
            <form method="post" action="/auth/logout">
              <button className="font-mono text-[10px] uppercase tracking-wider text-muted hover:text-fail">Sign out</button>
            </form>
          </div>
        </div>
      </aside>
      <main className="min-w-0 px-4 py-6 md:px-8 lg:py-8">
        <Outlet />
      </main>
    </div>
  )
}

/** The current user's role helpers, for hiding controls they cannot use. */
export function useRole() {
  const { data: me } = useSuspenseQuery(q.me())
  const role = me.account.role
  return { role, canAct: role === 'owner' || role === 'admin', isOwner: role === 'owner' }
}
