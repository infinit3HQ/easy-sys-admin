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

export function apiOrigin(): string {
  return process.env.ESA_API_URL ?? 'http://127.0.0.1:8080'
}

export async function proxyToControlPlane(request: Request): Promise<Response> {
  const incoming = new URL(request.url)
  const target = new URL(incoming.pathname + incoming.search, apiOrigin())

  const headers = new Headers()
  request.headers.forEach((value, key) => {
    if (!HOP_BY_HOP.has(key.toLowerCase())) headers.set(key, value)
  })
  headers.set('x-forwarded-host', incoming.host)
  headers.set('x-forwarded-proto', incoming.protocol.replace(':', ''))

  const hasBody = request.method !== 'GET' && request.method !== 'HEAD'
  let upstream: Response
  try {
    upstream = await fetch(target, {
      method: request.method,
      headers,
      body: hasBody ? request.body : undefined,
      redirect: 'manual',
      // Required by Node when streaming a request body.
      ...(hasBody ? { duplex: 'half' } : {}),
    })
  } catch {
    return Response.json(
      { error: { code: 'unavailable', message: 'The control plane is not reachable from the web server.' } },
      { status: 502 },
    )
  }

  const out = new Headers()
  upstream.headers.forEach((value, key) => {
    if (!HOP_BY_HOP.has(key.toLowerCase()) && key.toLowerCase() !== 'set-cookie') out.append(key, value)
  })
  // Set-Cookie must be copied one by one; Headers.forEach joins them.
  for (const cookie of upstream.headers.getSetCookie()) out.append('set-cookie', cookie)

  return new Response(upstream.body, { status: upstream.status, statusText: upstream.statusText, headers: out })
}
