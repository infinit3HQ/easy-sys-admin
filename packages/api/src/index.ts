import createClient from 'openapi-fetch'
import type { components, paths } from './schema.gen.ts'

export type { components, paths }

type Schemas = components['schemas']

export type ApiError = Schemas['Error']
export type AuthConfig = Schemas['AuthConfig']
export type Role = Schemas['Role']
export type User = Schemas['User']
export type AccountMembership = Schemas['AccountMembership']
export type Me = Schemas['Me']
export type ServerStatus = Schemas['ServerStatus']
export type ServerSummary = Schemas['ServerSummary']
export type FleetTotals = Schemas['FleetTotals']
export type FleetOverview = Schemas['FleetOverview']

export type ApiClient = ReturnType<typeof createApiClient>

export interface ApiClientOptions {
  /** Origin of the control plane, for example `http://esa-api:8080`. */
  baseUrl: string
  /** Headers sent with every request, for example the caller's session cookie. */
  headers?: Record<string, string>
}

/** Typed client for the control plane API. Paths and bodies come from openapi.yaml. */
export function createApiClient({ baseUrl, headers }: ApiClientOptions) {
  return createClient<paths>({
    baseUrl: new URL('/api/v1', baseUrl).toString(),
    headers,
  })
}
