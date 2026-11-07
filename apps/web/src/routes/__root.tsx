import {
  HeadContent,
  Scripts,
  createRootRouteWithContext,
} from '@tanstack/react-router'
import type { QueryClient } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { Button } from '#/components/ui/button'

import appCss from '../styles.css?url'

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()(
  {
    head: () => ({
      meta: [
        { charSet: 'utf-8' },
        {
          name: 'viewport',
          content: 'width=device-width, initial-scale=1, viewport-fit=cover',
        },
        { name: 'color-scheme', content: 'dark' },
        { name: 'theme-color', content: '#0b0d0c' },
        { title: 'Easy Sys Admin' },
        {
          name: 'description',
          content:
            'Easy Sys Admin is a web app for people who administer their own Linux servers. It shows the fleet on one board, installs updates, and cleans disks on a schedule.',
        },
      ],
      links: [
        { rel: 'stylesheet', href: appCss },
        { rel: 'icon', href: '/favicon.svg', type: 'image/svg+xml' },
      ],
    }),
    shellComponent: RootDocument,
    notFoundComponent: () => (
      <div className="grid min-h-dvh place-items-center px-6">
        <div className="text-center">
          <p className="font-display text-7xl font-bold text-amber uppercase sm:text-8xl">
            404
          </p>
          <p className="mt-2 font-mono text-sm text-muted-foreground">
            No signal on this channel.
          </p>
          <Button asChild variant="outline" className="mt-6">
            <a href="/app">Back to the board</a>
          </Button>
        </div>
      </div>
    ),
  },
)

function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="en" className="dark">
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
