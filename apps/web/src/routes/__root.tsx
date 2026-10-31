import { HeadContent, Scripts, createRootRouteWithContext } from '@tanstack/react-router'
import type { QueryClient } from '@tanstack/react-query'
import type { ReactNode } from 'react'

import appCss from '../styles.css?url'

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { name: 'color-scheme', content: 'dark' },
      { name: 'theme-color', content: '#0b0d0c' },
      { title: 'easy-sys-admin: one board for your whole server fleet' },
      {
        name: 'description',
        content:
          'Watch every server on one page, clean disks and patch packages on a schedule, and let AI agents read the fleet over MCP. Self-hosted.',
      },
    ],
    links: [
      { rel: 'stylesheet', href: appCss },
      { rel: 'icon', href: '/favicon.svg', type: 'image/svg+xml' },
    ],
  }),
  shellComponent: RootDocument,
  notFoundComponent: () => (
    <div className="grid min-h-screen place-items-center">
      <div className="text-center">
        <p className="font-display text-7xl font-bold uppercase text-amber">404</p>
        <p className="mt-2 font-mono text-sm text-muted">No signal on this channel.</p>
        <a href="/" className="mt-6 inline-block font-mono text-sm text-ink underline decoration-amber underline-offset-4">
          Back to the board
        </a>
      </div>
    </div>
  ),
})

function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <head>
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}
