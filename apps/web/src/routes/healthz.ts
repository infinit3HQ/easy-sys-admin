import { createFileRoute } from '@tanstack/react-router'
import { apiOrigin } from '#/server/proxy'

// Health of the web server and its link to the control plane.
export const Route = createFileRoute('/healthz')({
  server: {
    handlers: {
      GET: async () => {
        try {
          const res = await fetch(new URL('/healthz', apiOrigin()), { signal: AbortSignal.timeout(3000) })
          const body = (await res.json()) as { status: string }
          return Response.json({ web: 'ok', controlPlane: body.status }, { status: res.ok ? 200 : 503 })
        } catch {
          return Response.json({ web: 'ok', controlPlane: 'unreachable' }, { status: 503 })
        }
      },
    },
  },
})
