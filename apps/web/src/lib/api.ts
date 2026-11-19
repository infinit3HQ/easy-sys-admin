import { notFound } from '@tanstack/react-router'
import { createIsomorphicFn } from '@tanstack/react-start'
import { getRequestHeader } from '@tanstack/react-start/server'
import { createApiClient } from '@esa/api'
import type { ApiClient } from '@esa/api'

/** An error returned by the control plane API. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly field?: string

  constructor(status: number, code: string, message: string, field?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.field = field
  }
}

/**
 * The API client for the current environment. In the browser it calls the
 * same origin, which the web server proxies to the control plane. During
 * SSR it calls the control plane directly and forwards the visitor's cookie.
 */
export const api = createIsomorphicFn()
  .client((): ApiClient => createApiClient({ baseUrl: window.location.origin }))
  .server((): ApiClient => {
    const cookie = getRequestHeader('cookie')
    return createApiClient({
      baseUrl: process.env.ESA_API_URL ?? 'http://127.0.0.1:8080',
      headers: cookie ? { cookie } : undefined,
    })
  })

interface ApiResult<T> {
  data?: T
  error?: unknown
  response: Response
}

/** Unwraps an openapi-fetch result, throwing ApiError on failure. */
export async function call<T>(p: Promise<ApiResult<T>>): Promise<T> {
  let res: ApiResult<T>
  try {
    res = await p
  } catch (e) {
    throw new ApiError(
      0,
      'network',
      e instanceof Error ? e.message : 'The control plane cannot be reached.',
    )
  }
  if (res.response.ok) {
    return res.data as T
  }
  const body = res.error as
    { error?: { code?: string; message?: string; field?: string } } | undefined
  throw new ApiError(
    res.response.status,
    body?.error?.code ?? 'error',
    body?.error?.message ?? res.response.statusText,
    body?.error?.field,
  )
}

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message
  if (e instanceof Error) return e.message
  return 'Something went wrong.'
}

/**
 * orNotFound turns a 404 from the control plane into the router's
 * not-found, so a missing run, server, or group answers 404, not 500.
 */
export async function orNotFound<T>(p: Promise<T>): Promise<T> {
  try {
    return await p
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) throw notFound()
    throw e
  }
}
