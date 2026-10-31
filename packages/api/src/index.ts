import createClient from 'openapi-fetch'
import type { components, paths } from './schema.gen.ts'

export type { components, paths }

/** Every schema in openapi.yaml, by name. */
export type Schemas = components['schemas']

export type ApiError = Schemas['Error']
export type AuthConfig = Schemas['AuthConfig']
export type Role = Schemas['Role']
export type Scope = Schemas['Scope']
export type User = Schemas['User']
export type AccountMembership = Schemas['AccountMembership']
export type Me = Schemas['Me']

export type ServerStatus = Schemas['ServerStatus']
export type Server = Schemas['Server']
export type ServerDetail = Schemas['ServerDetail']
export type ServerMetrics = Schemas['ServerMetrics']
export type FleetTotals = Schemas['FleetTotals']
export type FleetOverview = Schemas['FleetOverview']
export type ProbeResult = Schemas['ProbeResult']
export type Login = Schemas['Login']
export type Enrollment = Schemas['Enrollment']
export type MetricPoint = Schemas['MetricPoint']

export type HostStatus = Schemas['HostStatus']
export type Disk = Schemas['Disk']
export type PendingUpdates = Schemas['PendingUpdates']
export type PendingPackage = Schemas['PendingPackage']
export type ServiceUnit = Schemas['ServiceUnit']
export type Container = Schemas['Container']
export type ImageCheck = Schemas['ImageCheck']
export type LogLine = Schemas['LogLine']
export type AccessReport = Schemas['AccessReport']
export type SecurityReport = Schemas['SecurityReport']
export type JobsReport = Schemas['JobsReport']

export type Day = Schemas['Day']
export type CleanPolicy = Schemas['CleanPolicy']
export type UpdatePolicy = Schemas['UpdatePolicy']
export type GroupInput = Schemas['GroupInput']
export type Group = Schemas['Group']
export type RunKind = Schemas['RunKind']
export type RunOptions = Schemas['RunOptions']
export type Run = Schemas['Run']
export type RunServer = Schemas['RunServer']

export type Alert = Schemas['Alert']
export type AlertRule = Schemas['AlertRule']
export type ChannelKind = Schemas['ChannelKind']
export type ChannelConfig = Schemas['ChannelConfig']
export type Channel = Schemas['Channel']
export type Cert = Schemas['Cert']
export type Token = Schemas['Token']
export type ActivityEntry = Schemas['ActivityEntry']

export type ApiClient = ReturnType<typeof createApiClient>

export interface ApiClientOptions {
  /** Origin of the control plane or the web app, for example `http://127.0.0.1:8080`. */
  baseUrl: string
  /** Headers sent with every request, for example the caller's cookie or bearer token. */
  headers?: Record<string, string>
}

/** Typed client for the control plane API. Paths and bodies come from openapi.yaml. */
export function createApiClient({ baseUrl, headers }: ApiClientOptions) {
  return createClient<paths>({
    baseUrl: new URL('/api/v1', baseUrl).toString(),
    headers,
  })
}
