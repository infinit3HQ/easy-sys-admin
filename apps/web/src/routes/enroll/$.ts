import { createFileRoute } from '@tanstack/react-router'
import { proxyToControlPlane } from '#/server/proxy'

export const Route = createFileRoute('/enroll/$')({
  server: { handlers: { ANY: ({ request }) => proxyToControlPlane(request) } },
})
