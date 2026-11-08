import { createServerFn } from '@tanstack/react-start'
import { docPathnames } from '#/lib/docs'

// Public pages a crawler may index when ESA_SEO is on. Everything else stays out.
const PUBLIC_PATHS = new Set([
  '/',
  '/privacy',
  '/terms',
  '/pricing',
  ...docPathnames,
])

export function seoEnabled(): boolean {
  return ['1', 'true', 'yes', 'on'].includes(
    (process.env.ESA_SEO ?? '').toLowerCase(),
  )
}

export const loadSeo = createServerFn({ method: 'GET' }).handler(async () =>
  seoEnabled(),
)

export function indexablePath(pathname: string): boolean {
  return (
    PUBLIC_PATHS.has(pathname) ||
    pathname === '/robots.txt' ||
    pathname === '/sitemap.xml'
  )
}

// Origin for absolute sitemap URLs. ESA_PUBLIC_URL wins. Otherwise the request host.
export function originFrom(request: Request): string {
  const configured = process.env.ESA_PUBLIC_URL
  if (configured) {
    try {
      const url = new URL(configured)
      if (url.protocol === 'http:' || url.protocol === 'https:')
        return url.origin
    } catch {
      // A bad value falls through to the request host.
    }
  }
  const url = new URL(request.url)
  const host = (request.headers.get('x-forwarded-host') ?? url.host)
    .split(',')[0]
    ?.trim()
  if (!host || /[\s/\\]/.test(host)) return url.origin
  const proto = (
    request.headers.get('x-forwarded-proto') ?? url.protocol.replace(':', '')
  )
    .split(',')[0]
    ?.trim()
  const scheme = proto === 'http' || proto === 'https' ? proto : 'https'
  return `${scheme}://${host}`
}

export function robotsTxt(seo: boolean, origin: string): string {
  if (!seo) return 'User-agent: *\nDisallow: /\n'
  const lines = [
    'User-agent: *',
    'Allow: /',
    'Disallow: /app',
    'Disallow: /api',
    'Disallow: /auth',
    'Disallow: /enroll',
    'Disallow: /login',
    'Disallow: /invite',
    'Disallow: /share',
    'Disallow: /mcp',
    'Disallow: /healthz',
  ]
  if (origin) lines.push('', `Sitemap: ${origin}/sitemap.xml`)
  return `${lines.join('\n')}\n`
}

export function sitemapXml(origin: string): string {
  const body = ['/', '/pricing', '/privacy', '/terms', ...docPathnames]
    .map((path) => `  <url><loc>${origin}${path}</loc></url>`)
    .join('\n')
  return `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${body}\n</urlset>\n`
}
