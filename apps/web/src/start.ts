import {
  createCsrfMiddleware,
  createMiddleware,
  createStart,
} from '@tanstack/react-start'

// Paths answered by the control plane through the proxy. It sets its own
// security headers on those responses.
const PROXIED = /^\/(api|auth|enroll)(\/|$)/

function contentSecurityPolicy(nonce: string): string {
  return [
    "default-src 'self'",
    // TanStack Start puts the nonce on its inline hydration scripts.
    `script-src 'self' 'nonce-${nonce}'`,
    // React renders style attributes on the server.
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data:",
    // Vite inlines small font subsets as data: URLs.
    "font-src 'self' data:",
    "connect-src 'self'",
    "frame-ancestors 'none'",
    "base-uri 'none'",
    "object-src 'none'",
    "form-action 'self'",
  ].join('; ')
}

const securityHeaders = createMiddleware().server(
  async ({ next, pathname, request }) => {
    const nonce = Buffer.from(
      crypto.getRandomValues(new Uint8Array(16)),
    ).toString('base64')
    const result = await next({ context: { nonce } })
    if (PROXIED.test(pathname)) return result

    const h = result.response.headers
    // Vite's dev server injects its own inline scripts; enforce the policy in builds.
    if (!import.meta.env.DEV)
      h.set('content-security-policy', contentSecurityPolicy(nonce))
    h.set('x-frame-options', 'DENY')
    h.set('x-content-type-options', 'nosniff')
    h.set('referrer-policy', 'same-origin')
    const proto = (
      request.headers.get('x-forwarded-proto') ?? new URL(request.url).protocol
    )
      .split(',')[0]
      ?.trim()
      .replace(':', '')
    if (proto === 'https')
      h.set('strict-transport-security', 'max-age=31536000')
    return result
  },
)

export const startInstance = createStart(() => ({
  // Listing middleware replaces Start's default, so keep its CSRF check for server functions.
  requestMiddleware: [
    createCsrfMiddleware({ filter: (ctx) => ctx.handlerType === 'serverFn' }),
    securityHeaders,
  ],
}))
