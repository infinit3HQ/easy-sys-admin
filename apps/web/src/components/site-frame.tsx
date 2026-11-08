import { supportEmail } from '@esa/plans'
import { Link, useLoaderData } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { SourceOffer } from '#/components/source-offer'
import { Button } from '#/components/ui/button'
import { cn } from '#/lib/utils'

/** The plotting grid stays full width. The type sits in one centered column. */
export const column = 'mx-auto w-full max-w-7xl px-4 sm:px-6'

/** The name on the public site. It matches the Google sign-in screen. */
export const productName = 'EASY-SYS-ADMIN'

export { supportEmail }

const footerColumns = [
  {
    label: 'Go',
    links: [
      { href: '/app', label: 'Board' },
      { href: '/pricing', label: 'Pricing' },
      { href: '/#install', label: 'Self-host' },
      { href: '/#source', label: 'Source' },
    ],
  },
  {
    label: 'Read',
    links: [
      { href: '/docs/quickstart', label: 'Quickstart' },
      { href: '/docs/how-it-works', label: 'How it works' },
      { href: '/docs/cli', label: 'CLI' },
      { href: '/docs/mcp', label: 'MCP' },
    ],
  },
  {
    label: 'Legal',
    links: [
      { href: '/terms', label: 'Terms' },
      { href: '/privacy', label: 'Privacy' },
      { href: '/docs/license', label: 'License' },
    ],
  },
] as const

function SiteFooter() {
  const { source } = useLoaderData({ from: '__root__' })
  return (
    <footer className="border-t">
      <div className={cn(column, 'py-10 sm:py-14')}>
        <div className="border border-line-2">
          <div className="grid sm:grid-cols-3">
            {footerColumns.map((group, i) => (
              <div
                key={group.label}
                className={cn(
                  'p-4 sm:p-5',
                  i > 0 && 'border-t border-line-2 sm:border-t-0 sm:border-l',
                )}
              >
                <p className="label">{group.label}</p>
              <ul className="mt-4 space-y-3">
                {group.links.map((item) => (
                  <li key={item.href}>
                    <a
                      href={item.href}
                      className="font-display text-lg leading-none font-semibold text-ink uppercase hover:text-amber"
                    >
                        {item.label}
                      </a>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
          <div className="flex flex-col gap-3 border-t border-line-2 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-5">
            <SourceOffer className="font-mono text-[11px] text-muted-foreground" />
            <div className="flex flex-wrap items-center gap-x-5 gap-y-1 font-mono text-[11px]">
              <a href={source.url} className="text-ink-2 hover:text-amber">
                Repository
              </a>
              <a
                href={`mailto:${supportEmail}`}
                className="text-ink-2 hover:text-amber"
              >
                {supportEmail}
              </a>
              <span className="tracking-wide text-faint uppercase">
                AGPL-3.0-only
              </span>
            </div>
          </div>
        </div>
      </div>
    </footer>
  )
}

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
              {productName}
            </span>
            <span className="shrink-0 rounded-sm border border-amber/70 px-1.5 py-0.5 font-mono text-[10px] tracking-widest text-amber uppercase">
              Beta
            </span>
          </Link>
          <nav className="flex items-center gap-6 font-mono text-xs text-ink-2">
            <a href="/#watch" className="hidden hover:text-ink sm:inline">
              Watches
            </a>
            <a href="/#safety" className="hidden hover:text-ink lg:inline">
              Safety
            </a>
            <a href="/#agents" className="hidden hover:text-ink lg:inline">
              MCP
            </a>
            <Link to="/pricing" className="hover:text-ink">
              Pricing
            </Link>
            <Link to="/docs" className="hidden hover:text-ink sm:inline">
              Docs
            </Link>
            <a href="/#source" className="hidden hover:text-ink lg:inline">
              Source
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
      <SiteFooter />
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
