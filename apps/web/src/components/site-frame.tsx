import { Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { Button } from '#/components/ui/button'
import { cn } from '#/lib/utils'

/** The plotting grid stays full width. The type sits in one centered column. */
export const column = 'mx-auto w-full max-w-7xl px-4 sm:px-6'

/**
 * The source repo is private, so the public pages do not offer a self-hosted
 * install. Turn this on when the repo and the deploy files are public.
 */
export const showSelfHost = false

const legal = [
  { to: '/terms', label: 'Terms' },
  { to: '/privacy', label: 'Privacy policy' },
] as const

/** Landing and legal pages share this chrome. The signed-in board does not. */
export function SiteFrame({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-dvh overflow-x-hidden">
      <header className="sticky top-0 z-40 border-b bg-bg/80 backdrop-blur-md">
        <div
          className={cn(column, 'flex h-14 items-center justify-between gap-4')}
        >
          <Link to="/" className="flex min-w-0 items-center gap-2.5">
            <img src="/favicon.svg" alt="" className="size-6 shrink-0" />
            <span className="truncate font-display text-lg font-bold tracking-wide uppercase sm:text-xl">
              easy-sys-admin
            </span>
            <span className="shrink-0 rounded-sm border border-amber/70 px-1.5 py-0.5 font-mono text-[10px] tracking-widest text-amber uppercase">
              Beta
            </span>
          </Link>
          <nav className="flex items-center gap-6 font-mono text-xs text-ink-2">
            <a href="/#watch" className="hidden hover:text-ink sm:inline">
              Watches
            </a>
            <a href="/#safety" className="hidden hover:text-ink sm:inline">
              Safety
            </a>
            <a href="/#agents" className="hidden hover:text-ink md:inline">
              MCP
            </a>
            <Button
              asChild
              size="sm"
              variant="outline"
              className="border-amber text-amber hover:bg-amber hover:text-bg"
            >
              <Link to="/app">
                <span className="hidden sm:inline">Open the board</span>
                <span className="sm:hidden">Board</span>
              </Link>
            </Button>
          </nav>
        </div>
      </header>
      {children}
      <footer className="border-t">
        <div
          className={cn(
            column,
            'flex flex-col gap-4 py-6 sm:flex-row sm:items-center sm:justify-between',
          )}
        >
          <div className="min-w-0">
            <div className="flex items-center gap-2.5">
              <img src="/favicon.svg" alt="" className="size-5 shrink-0" />
              <span className="font-display text-lg font-bold tracking-wide uppercase">
                easy-sys-admin
              </span>
              <span className="rounded-sm border border-amber/70 px-1.5 py-0.5 font-mono text-[10px] tracking-widest text-amber uppercase">
                Beta
              </span>
            </div>
            <p className="mt-1.5 font-mono text-[11px] text-muted-foreground">
              One board for a fleet of Linux servers.
            </p>
          </div>
          <nav className="flex gap-6 font-mono text-xs text-ink-2">
            {legal.map((item) => (
              <Link key={item.to} to={item.to} className="hover:text-ink">
                {item.label}
              </Link>
            ))}
          </nav>
        </div>
      </footer>
    </div>
  )
}

export function LegalPage({
  kicker,
  title,
  lede,
  children,
}: {
  kicker: string
  title: string
  lede: string
  children: ReactNode
}) {
  return (
    <SiteFrame>
      <article className={cn(column, 'py-14 sm:py-20')}>
        <p className="label mb-4 !text-amber">{kicker}</p>
        <h1 className="max-w-3xl font-display text-5xl leading-[0.9] font-bold uppercase sm:text-6xl">
          {title}
        </h1>
        <p className="mt-6 max-w-2xl text-base leading-relaxed text-ink-2">
          {lede}
        </p>
        <div className="mt-12 max-w-2xl space-y-10 text-sm leading-relaxed text-ink-2 [&_h2]:font-display [&_h2]:text-2xl [&_h2]:font-semibold [&_h2]:tracking-wide [&_h2]:text-ink [&_h2]:uppercase [&_p+p]:mt-3 [&_ul]:mt-3 [&_ul]:list-disc [&_ul]:space-y-2 [&_ul]:pl-5">
          {children}
        </div>
      </article>
    </SiteFrame>
  )
}
