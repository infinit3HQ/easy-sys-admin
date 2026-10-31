import { Link, Outlet, createFileRoute, redirect, useRouter, useRouterState } from '@tanstack/react-router'
import { useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { ActivityIcon, BellIcon, ChevronsUpDownIcon, HistoryIcon, LayersIcon, LayoutGridIcon, LogOutIcon, MenuIcon, SettingsIcon } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { ApiError, api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Choice, cn } from '#/components/kit'
import { Button } from '#/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '#/components/ui/dropdown-menu'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from '#/components/ui/sheet'

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

const nav: ReadonlyArray<{
  to: '/app' | '/app/groups' | '/app/runs' | '/app/alerts' | '/app/activity' | '/app/settings'
  label: string
  icon: LucideIcon
  exact?: boolean
}> = [
  { to: '/app', label: 'Board', icon: LayoutGridIcon, exact: true },
  { to: '/app/groups', label: 'Groups', icon: LayersIcon },
  { to: '/app/runs', label: 'Runs', icon: HistoryIcon },
  { to: '/app/alerts', label: 'Alerts', icon: BellIcon },
  { to: '/app/activity', label: 'Activity', icon: ActivityIcon },
  { to: '/app/settings', label: 'Settings', icon: SettingsIcon },
]

function AppShell() {
  const [menuOpen, setMenuOpen] = useState(false)
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const current = [...nav].reverse().find((n) => (n.exact ? pathname === n.to : pathname.startsWith(n.to)))

  return (
    <div className="min-h-dvh lg:grid lg:grid-cols-[232px_1fr]">
      {/* Signing out is a plain form post so it works without JavaScript too. */}
      <form id="logout-form" method="post" action="/auth/logout" hidden />

      {/* Narrow screens: a top bar with the page name and a menu button. */}
      <header className="sticky top-0 z-40 flex h-14 items-center gap-3 border-b bg-bg/85 px-4 backdrop-blur-md lg:hidden">
        <Link to="/app" className="flex items-center gap-2" aria-label="Board">
          <img src="/favicon.svg" alt="" className="size-6" />
          <span className="font-display text-lg leading-none font-bold tracking-wide uppercase">esa</span>
        </Link>
        {current && (
          <>
            <span className="text-faint">/</span>
            <span className="truncate font-mono text-[11px] tracking-wider text-ink-2 uppercase">{current.label}</span>
          </>
        )}
        <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
          <SheetTrigger asChild>
            <Button variant="ghost" size="icon" className="ml-auto" aria-label="Open menu">
              <MenuIcon />
            </Button>
          </SheetTrigger>
          <SheetContent side="left" className="w-[min(18rem,85vw)] gap-0 border-r p-0">
            <SheetHeader className="border-b px-4 py-4">
              <SheetTitle className="flex items-center gap-2">
                <img src="/favicon.svg" alt="" className="size-6" />
                <span className="font-display text-lg leading-none font-bold tracking-wide uppercase">easy-sys-admin</span>
              </SheetTitle>
              <SheetDescription className="sr-only">Pages and account</SheetDescription>
            </SheetHeader>
            <NavLinks onNavigate={() => setMenuOpen(false)} className="flex-1 overflow-y-auto p-3" />
            <AccountBox className="border-t p-3" />
          </SheetContent>
        </Sheet>
      </header>

      {/* Wide screens: a fixed sidebar. */}
      <aside className="sticky top-0 hidden h-dvh flex-col border-r bg-bg-2/80 lg:flex">
        <Link to="/" className="flex items-center gap-2 px-5 pt-5 pb-6">
          <img src="/favicon.svg" alt="" className="size-6" />
          <span className="font-display text-lg leading-none font-bold tracking-wide uppercase">esa</span>
        </Link>
        <NavLinks className="flex-1 overflow-y-auto px-3" />
        <AccountBox className="border-t p-3" />
      </aside>

      <main className="mx-auto w-full max-w-[1600px] min-w-0 px-4 py-6 sm:px-6 md:px-8 lg:py-8">
        <Outlet />
      </main>
    </div>
  )
}

function NavLinks({ onNavigate, className }: { onNavigate?: () => void; className?: string }) {
  return (
    <nav className={cn('flex flex-col gap-0.5', className)}>
      {nav.map((n) => (
        <Link
          key={n.to}
          to={n.to}
          onClick={onNavigate}
          activeOptions={{ exact: !!n.exact }}
          className="group flex items-center gap-3 rounded-md px-3 py-2.5 font-mono text-xs tracking-wider text-ink-2 uppercase transition-colors hover:bg-panel-2 hover:text-ink lg:py-2"
          activeProps={{
            className: 'bg-amber/10! text-amber!',
            'aria-current': 'page',
          }}
        >
          <n.icon className="size-4 shrink-0 opacity-70 group-aria-[current=page]:opacity-100" aria-hidden />
          {n.label}
        </Link>
      ))}
    </nav>
  )
}

/** Account switcher and the signed-in user, with sign-out in a menu. */
function AccountBox({ className }: { className?: string }) {
  const { data: me } = useSuspenseQuery(q.me())
  const qc = useQueryClient()
  const router = useRouter()
  const who = me.user.name || me.user.email

  return (
    <div className={cn('space-y-2', className)}>
      {me.accounts.length > 1 && (
        <Choice
          aria-label="Account"
          size="sm"
          className="font-mono text-xs"
          value={String(me.account.id)}
          options={me.accounts.map((a) => ({
            value: String(a.id),
            label: a.name,
          }))}
          onChange={async (v) => {
            await call(api().PUT('/me/account', { body: { accountId: Number(v) } }))
            qc.clear()
            await router.invalidate()
          }}
        />
      )}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            className="flex w-full cursor-pointer items-center gap-3 rounded-md px-2 py-2 text-left transition-colors outline-none hover:bg-panel-2 focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            <span className="grid size-8 shrink-0 place-items-center rounded-md border border-line-2 bg-panel font-display text-sm font-bold text-amber uppercase">
              {who.slice(0, 1)}
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm text-ink">{who}</span>
              <span
                className={cn('block truncate font-mono text-[10px] tracking-wider uppercase', me.account.role === 'viewer' ? 'text-muted-foreground' : 'text-amber')}
              >
                {me.accounts.length > 1 ? me.account.role : `${me.account.name} · ${me.account.role}`}
              </span>
            </span>
            <ChevronsUpDownIcon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent side="top" align="start" className="w-(--radix-dropdown-menu-trigger-width) min-w-56">
          <DropdownMenuLabel className="font-normal">
            <span className="block truncate text-sm text-ink">{who}</span>
            {me.user.name && <span className="block truncate font-mono text-[11px] text-muted-foreground">{me.user.email}</span>}
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem asChild>
            <Link to="/app/settings">
              <SettingsIcon /> Settings
            </Link>
          </DropdownMenuItem>
          <DropdownMenuItem asChild variant="destructive">
            <button type="submit" form="logout-form" className="w-full">
              <LogOutIcon /> Sign out
            </button>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}

/** The current user's role helpers, for hiding controls they cannot use. */
export function useRole() {
  const { data: me } = useSuspenseQuery(q.me())
  const role = me.account.role
  return {
    role,
    canAct: role === 'owner' || role === 'admin',
    isOwner: role === 'owner',
  }
}
