import { createFileRoute } from '@tanstack/react-router'
import { originFrom, robotsTxt, seoEnabled } from '#/lib/seo'

export const Route = createFileRoute('/robots.txt')({
  server: {
    handlers: {
      GET: ({ request }) =>
        new Response(robotsTxt(seoEnabled(), originFrom(request)), {
          headers: {
            'content-type': 'text/plain; charset=utf-8',
            'cache-control': 'public, max-age=3600',
          },
        }),
    },
  },
})
