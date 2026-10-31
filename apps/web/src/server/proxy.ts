// Forwards a request to the control plane unchanged, so the browser, the
// CLI, and enroll scripts can use one origin for the web app and the API.
// Server-only: imported from server route handlers.

const HOP_BY_HOP = new Set([
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
  'host',
  'content-length',
])

// Request headers a client must not set: the control plane trusts them for
// client addresses and audit labels. x-forwarded-for is kept as received;
// Traefik appends the real client and the control plane reads the rightmost entry.
const CLIENT_SPOOFABLE = new Set([
  'x-real-ip',
  'forwarded',
  'x-esa-client',
  'x-forwarded-host',
  'x-forwarded-proto',
])

const MAX_BODY_BYTES = 1 << 20

// Most calls answer quickly. Calls that wait on a managed server over SSH
// (probes, helper installs, apt-get update, image checks, service and
// container actions, logs) and enrollment get as long as the control plane's
// own 10 minute write timeout, plus a margin.
const DEFAULT_TIMEOUT_MS = 60_000
const SLOW_TIMEOUT_MS = 11 * 60_000
const SLOW_PATHS = /^\/(api\/v1\/(servers|certs)(\/|$)|enroll\/)/

export function apiOrigin(): string {
  return process.env.ESA_API_URL ?? 'http://127.0.0.1:8080'
}

function errorResponse(
  status: number,
  code: string,
  message: string,
): Response {
  return Response.json({ error: { code, message } }, { status })
}

export async function proxyToControlPlane(request: Request): Promise<Response> {
  const incoming = new URL(request.url)
  const target = new URL(incoming.pathname + incoming.search, apiOrigin())

  const length = request.headers.get('content-length')
  if (length !== null && !(Number(length) <= MAX_BODY_BYTES)) {
    return errorResponse(
      413,
      'too_large',
      'The request body is larger than 1 MiB.',
    )
  }

  const headers = new Headers()
  request.headers.forEach((value, key) => {
    const k = key.toLowerCase()
    if (!HOP_BY_HOP.has(k) && !CLIENT_SPOOFABLE.has(k)) headers.set(key, value)
  })
  headers.set('x-forwarded-host', incoming.host)
  headers.set('x-forwarded-proto', incoming.protocol.replace(':', ''))

  const hasBody = request.method !== 'GET' && request.method !== 'HEAD'
  const slow = SLOW_PATHS.test(incoming.pathname)
  const timeout = slow ? SLOW_TIMEOUT_MS : DEFAULT_TIMEOUT_MS
  // A slow call (a helper install, an upgrade start) keeps running when the
  // browser tab closes; cancelling it upstream would stop an install halfway.
  const signal = slow
    ? AbortSignal.timeout(timeout)
    : AbortSignal.any([request.signal, AbortSignal.timeout(timeout)])
  let upstream: Response
  try {
    upstream = await fetch(target, {
      method: request.method,
      headers,
      body: hasBody ? request.body : undefined,
      redirect: 'manual',
      signal,
      // Required by Node when streaming a request body.
      ...(hasBody ? { duplex: 'half' } : {}),
    })
  } catch (e) {
    if (e instanceof DOMException && e.name === 'TimeoutError') {
      return errorResponse(
        504,
        'timeout',
        'The control plane did not answer in time.',
      )
    }
    return errorResponse(
      502,
      'unavailable',
      'The control plane is not reachable from the web server.',
    )
  }

  const out = new Headers()
  upstream.headers.forEach((value, key) => {
    if (
      !HOP_BY_HOP.has(key.toLowerCase()) &&
      key.toLowerCase() !== 'set-cookie'
    )
      out.append(key, value)
  })
  // Set-Cookie must be copied one by one; Headers.forEach joins them.
  for (const cookie of upstream.headers.getSetCookie())
    out.append('set-cookie', cookie)

  return new Response(upstream.body, {
    status: upstream.status,
    statusText: upstream.statusText,
    headers: out,
  })
}
