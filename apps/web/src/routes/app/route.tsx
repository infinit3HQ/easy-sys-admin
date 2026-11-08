import {
  Link,
  Outlet,
  createFileRoute,
  redirect,
  useRouter,
  useRouterState,
} from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import {
  ActivityIcon,
  BellIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronsUpDownIcon,
  HistoryIcon,
  LayersIcon,
  LayoutGridIcon,
  CircleDollarSignIcon,
  LogOutIcon,
  MenuIcon,
  PlusIcon,
  SettingsIcon,
  UsersIcon,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { ApiError, api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { cn } from '#/components/kit'
import { CreateOrganizationDialog, useSessionReset } from '#/components/org'
import { PricingDialog } from '#/components/pricing'
import { SourceOffer } from '#/components/source-offer'
import { Avatar, AvatarFallback } from '#/components/ui/avatar'
import { Button } from '#/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '#/components/ui/dropdown-menu'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '#/components/ui/sheet'

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
  head: () => ({
    meta: [{ name: 'robots', content: 'noindex, nofollow' }],
  }),
  component: AppShell,
})

const nav: ReadonlyArray<{
  to:
    | '/app'
    | '/app/groups'
    | '/app/runs'
    | '/app/alerts'
    | '/app/activity'
    | '/app/org'
    | '/app/settings'
  label: string
  icon: LucideIcon
  exact?: boolean
}> = [
  { to: '/app', label: 'Board', icon: LayoutGridIcon, exact: true },
  { to: '/app/groups', label: 'Groups', icon: LayersIcon },
  { to: '/app/runs', label: 'Runs', icon: HistoryIcon },
  { to: '/app/alerts', label: 'Alerts', icon: BellIcon },
  { to: '/app/activity', label: 'Activity', icon: ActivityIcon },
  { to: '/app/org', label: 'Organization', icon: UsersIcon },
  { to: '/app/settings', label: 'Settings', icon: SettingsIcon },
]

function AppShell() {
  const [menuOpen, setMenuOpen] = useState(false)
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const current = [...nav]
    .reverse()
    .find((n) => (n.exact ? pathname === n.to : pathname.startsWith(n.to)))

  return (
    <div className="min-h-dvh lg:grid lg:grid-cols-[232px_1fr]">
      {/* Signing out is a plain form post so it works without JavaScript too. */}
      <form id="logout-form" method="post" action="/auth/logout" hidden />

      {/* Narrow screens: a top bar with the page name and a menu button. */}
      <header className="sticky top-0 z-40 flex h-14 items-center gap-3 border-b bg-bg/85 px-4 backdrop-blur-md lg:hidden">
        <Link
          to="/app"
          activeOptions={{ exact: true }}
          className="flex items-center gap-2"
          aria-label="Board"
        >
          <img src="/favicon.svg" alt="" className="size-6" />
          <span className="font-display text-lg leading-none font-bold tracking-wide uppercase">
            esa
          </span>
        </Link>
        {current && (
          <>
            <span className="text-faint">/</span>
            <span className="truncate font-mono text-[11px] tracking-wider text-ink-2 uppercase">
              {current.label}
            </span>
          </>
        )}
        <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
          <SheetTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className="ml-auto"
              aria-label="Open menu"
            >
              <MenuIcon />
            </Button>
          </SheetTrigger>
          <SheetContent
            side="left"
            className="w-[min(18rem,85vw)] gap-0 border-r p-0"
          >
            <SheetHeader className="border-b px-4 py-4">
              <SheetTitle className="flex items-center gap-2">
                <img src="/favicon.svg" alt="" className="size-6" />
                <span className="font-display text-lg leading-none font-bold tracking-wide uppercase">
                  easy-sys-admin
                </span>
              </SheetTitle>
              <SheetDescription className="sr-only">
                Pages and account
              </SheetDescription>
            </SheetHeader>
            <NavLinks
              onNavigate={() => setMenuOpen(false)}
              className="flex-1 overflow-y-auto p-3"
            />
            <SourceOffer className="px-4 pb-3 font-mono text-[10px] leading-relaxed text-muted-foreground" />
            <AccountBox
              className="border-t p-3"
              onNavigate={() => setMenuOpen(false)}
            />
          </SheetContent>
        </Sheet>
      </header>

      {/* Wide screens: a fixed sidebar. */}
      <aside className="sticky top-0 hidden h-dvh flex-col border-r bg-bg-2/80 lg:flex">
        <Link to="/" className="flex items-center gap-2 px-5 pt-5 pb-6">
          <img src="/favicon.svg" alt="" className="size-6" />
          <span className="font-display text-lg leading-none font-bold tracking-wide uppercase">
            esa
          </span>
        </Link>
        <NavLinks className="flex-1 overflow-y-auto px-3" />
        <SourceOffer className="px-5 pb-3 font-mono text-[10px] leading-relaxed text-muted-foreground" />
        <AccountBox className="border-t p-3" />
      </aside>

      <main className="mx-auto w-full max-w-[1600px] min-w-0 px-4 py-6 sm:px-6 md:px-8 lg:py-8">
        <Outlet />
      </main>
    </div>
  )
}

function NavLinks({
  onNavigate,
  className,
}: {
  onNavigate?: () => void
  className?: string
}) {
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
          <n.icon
            className="size-4 shrink-0 opacity-70 group-aria-[current=page]:opacity-100"
            aria-hidden
          />
          {n.label}
        </Link>
      ))}
    </nav>
  )
}

/** The shadcn sidebar account row: avatar, name, and one line under it. */
function AccountMark({
  who,
  role,
  detail,
}: {
  who: string
  role?: string
  detail?: string
}) {
  return (
    <>
      <Avatar className="size-8 rounded-md">
        <AvatarFallback className="rounded-md border border-line-2 bg-panel font-display text-sm font-bold text-amber uppercase">
          {who.slice(0, 1)}
        </AvatarFallback>
      </Avatar>
      <span className="grid min-w-0 flex-1 text-left text-sm leading-tight">
        <span className="truncate text-ink">{who}</span>
        {role && (
          <span
            className={cn(
              'truncate font-mono text-[10px] tracking-wider uppercase',
              role === 'viewer' ? 'text-muted-foreground' : 'text-amber',
            )}
          >
            {role}
          </span>
        )}
        {detail && (
          <span className="truncate font-mono text-[11px] text-muted-foreground">
            {detail}
          </span>
        )}
      </span>
    </>
  )
}

/** Account switcher and the signed-in user, with sign-out in a menu. */
function AccountBox({
  className,
  onNavigate,
}: {
  className?: string
  onNavigate?: () => void
}) {
  const { data: me } = useSuspenseQuery(q.me())
  const reset = useSessionReset()
  const router = useRouter()
  const [creating, setCreating] = useState(false)
  const [plansOpen, setPlansOpen] = useState(false)
  const who = me.user.name || me.user.email
  // The personal space first, then organizations in the order the API lists them.
  const spaces = [
    ...me.accounts.filter((a) => a.personal),
    ...me.accounts.filter((a) => !a.personal),
  ]
  const ownedOrgs = me.accounts.filter(
    (a) => !a.personal && a.role === 'owner',
  ).length
  const canCreateOrg = !me.viaToken && ownedOrgs < me.limits.maxOrganizations
  async function switchSpace(accountId: number) {
    if (accountId === me.account.id) return
    await call(api().PUT('/me/account', { body: { accountId } }))
    // A server, group, or run page belongs to the old space; go to its list instead.
    const path = router.state.location.pathname
    const detail = /^\/app\/(servers|groups|runs)\/[^/]+/.exec(path)
    await reset(async () => {
      if (!detail) return
      await router.navigate({
        to:
          detail[1] === 'servers'
            ? '/app'
            : `/app/${detail[1] as 'groups' | 'runs'}`,
      })
    })
    onNavigate?.()
  }

  return (
    <div className={cn('space-y-2', className)}>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            type="button"
            variant="outline"
            size="sm"
            aria-label="Space"
            className="w-full justify-between font-mono font-normal text-ink"
          >
            <span className="min-w-0 truncate">
              {me.account.personal ? 'Personal' : me.account.name}
            </span>
            <ChevronDownIcon className="opacity-50" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent
          side="top"
          align="start"
          className="w-(--radix-dropdown-menu-trigger-width)"
        >
          {spaces.map((a) => (
            <DropdownMenuItem
              key={a.id}
              onSelect={() => {
                void switchSpace(a.id)
              }}
            >
              <span className="min-w-0 flex-1 truncate">
                {a.personal ? 'Personal' : a.name}
              </span>
              {a.id === me.account.id && <CheckIcon className="text-ink" />}
            </DropdownMenuItem>
          ))}
          {canCreateOrg && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                onSelect={() => setCreating(true)}
                className="text-amber focus:text-amber"
              >
                <PlusIcon className="text-amber" /> Create organization
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
      <CreateOrganizationDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreated={onNavigate}
      />
      <PricingDialog open={plansOpen} onOpenChange={setPlansOpen} />
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            className="flex h-12 w-full items-center gap-2 overflow-hidden rounded-md border border-transparent px-2.5 py-2 text-left text-sm outline-none hover:bg-panel-2 focus-visible:ring-[3px] focus-visible:ring-ring/50 data-[state=open]:bg-panel-2"
          >
            <AccountMark who={who} role={me.account.role} />
            <ChevronsUpDownIcon
              className="ml-auto size-4 shrink-0 text-muted-foreground"
              aria-hidden
            />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent
          side="right"
          align="end"
          sideOffset={4}
          className="w-(--radix-dropdown-menu-trigger-width) min-w-56"
        >
          <DropdownMenuLabel className="p-0 font-normal">
            <div className="flex items-center gap-2 px-1 py-1.5 text-left text-sm">
              <AccountMark
                who={who}
                detail={me.user.name ? me.user.email : undefined}
              />
            </div>
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onSelect={() => {
              setPlansOpen(true)
            }}
          >
            <CircleDollarSignIcon /> Plans
          </DropdownMenuItem>
          <DropdownMenuItem asChild>
            <Link to="/app/settings" onClick={onNavigate}>
              <SettingsIcon /> Settings
            </Link>
          </DropdownMenuItem>
          <DropdownMenuItem
            variant="destructive"
            onSelect={() => {
              const form = document.getElementById('logout-form')
              if (form instanceof HTMLFormElement) form.requestSubmit()
            }}
          >
            <LogOutIcon /> Sign out
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
