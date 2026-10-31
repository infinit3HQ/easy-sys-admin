import { createFileRoute } from '@tanstack/react-router'
import { handleMcp } from '#/server/mcp'

// MCP over Streamable HTTP. Point an MCP client at <origin>/mcp with an
// esa API token as the bearer token.
export const Route = createFileRoute('/mcp')({
  server: {
    handlers: {
      GET: ({ request }) => handleMcp(request),
      POST: ({ request }) => handleMcp(request),
      DELETE: ({ request }) => handleMcp(request),
    },
  },
})
