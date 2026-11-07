import { createIsomorphicFn } from '@tanstack/react-start'
import { getRequestHeader } from '@tanstack/react-start/server'

/**
 * The origin of this site. In the browser that is the address in the bar.
 * On the server it is ESA_PUBLIC_URL, then the request host.
 */
export const siteOrigin = createIsomorphicFn()
  .client(() => window.location.origin)
  .server(() => {
    const configured = process.env.ESA_PUBLIC_URL
    if (configured) {
      try {
        return new URL(configured).origin
      } catch {
        // A bad value falls through to the request host.
      }
    }
    const host = (
      getRequestHeader('x-forwarded-host') ?? getRequestHeader('host')
    )
      ?.split(',')[0]
      ?.trim()
    if (!host) return ''
    const proto = (
      getRequestHeader('x-forwarded-proto') ??
      (host.startsWith('localhost') || host.startsWith('127.0.0.1')
        ? 'http'
        : 'https')
    )
      .split(',')[0]
      ?.trim()
      .replace(/:$/, '')
    return `${proto}://${host}`
  })
