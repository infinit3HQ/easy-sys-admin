import { createFileRoute } from '@tanstack/react-router'
import { originFrom, seoEnabled, sitemapXml } from '#/lib/seo'

export const Route = createFileRoute('/sitemap.xml')({
  server: {
    handlers: {
      GET: ({ request }) => {
        if (!seoEnabled()) {
          return new Response('Not found\n', {
            status: 404,
            headers: { 'content-type': 'text/plain; charset=utf-8' },
          })
        }
        return new Response(sitemapXml(originFrom(request)), {
          headers: {
            'content-type': 'application/xml; charset=utf-8',
            'cache-control': 'public, max-age=3600',
          },
        })
      },
    },
  },
})
