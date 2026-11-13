import { queryOptions } from '@tanstack/react-query'
import { api, call } from '#/lib/api'

// Live pages refresh on their own: the poller writes new snapshots every minute.
const LIVE = 15_000

export const q = {
  authConfig: () =>
    queryOptions({
      queryKey: ['auth-config'],
      queryFn: () => call(api().GET('/auth/config')),
      staleTime: Infinity,
    }),
  me: () =>
    queryOptions({
      queryKey: ['me'],
      queryFn: () => call(api().GET('/me')),
      staleTime: 60_000,
    }),
  overview: () =>
    queryOptions({
      queryKey: ['overview'],
      queryFn: () => call(api().GET('/fleet/overview')),
      refetchInterval: LIVE,
    }),
  server: (id: number) =>
    queryOptions({
      queryKey: ['server', id],
      queryFn: () =>
        call(
          api().GET('/servers/{serverId}', {
            params: { path: { serverId: id } },
          }),
        ),
      refetchInterval: LIVE,
    }),
  metrics: (id: number, window: '1h' | '24h' | '7d' | '30d' | '90d') =>
    queryOptions({
      queryKey: ['metrics', id, window],
      queryFn: () =>
        call(
          api().GET('/servers/{serverId}/metrics', {
            params: { path: { serverId: id }, query: { window } },
          }),
        ),
      refetchInterval: window === '1h' ? 30_000 : 120_000,
    }),
  services: (id: number) =>
    queryOptions({
      queryKey: ['services', id],
      queryFn: () =>
        call(
          api().GET('/servers/{serverId}/services', {
            params: { path: { serverId: id } },
          }),
        ),
    }),
  containers: (id: number) =>
    queryOptions({
      queryKey: ['containers', id],
      queryFn: () =>
        call(
          api().GET('/servers/{serverId}/containers', {
            params: { path: { serverId: id } },
          }),
        ),
    }),
  imageCheck: (id: number) =>
    queryOptions({
      queryKey: ['image-check', id],
      queryFn: () =>
        call(
          api().GET('/servers/{serverId}/containers/image-check', {
            params: { path: { serverId: id } },
          }),
        ),
      staleTime: 10 * 60_000,
    }),
  access: (id: number) =>
    queryOptions({
      queryKey: ['access', id],
      queryFn: () =>
        call(
          api().GET('/servers/{serverId}/access', {
            params: { path: { serverId: id } },
          }),
        ),
    }),
  security: (id: number) =>
    queryOptions({
      queryKey: ['security', id],
      queryFn: () =>
        call(
          api().GET('/servers/{serverId}/security', {
            params: { path: { serverId: id } },
          }),
        ),
    }),
  recovery: (id: number) =>
    queryOptions({
      queryKey: ['recovery', id],
      queryFn: () =>
        call(
          api().GET('/servers/{serverId}/recovery', {
            params: { path: { serverId: id } },
          }),
        ),
    }),
  advisories: (id: number) =>
    queryOptions({
      queryKey: ['advisories', id],
      queryFn: () =>
        call(
          api().GET('/servers/{serverId}/advisories', {
            params: { path: { serverId: id } },
          }),
        ),
    }),
  jobs: (id: number) =>
    queryOptions({
      queryKey: ['jobs', id],
      queryFn: () =>
        call(
          api().GET('/servers/{serverId}/jobs', {
            params: { path: { serverId: id } },
          }),
        ),
    }),
  groups: () =>
    queryOptions({
      queryKey: ['groups'],
      queryFn: () => call(api().GET('/groups')),
    }),
  group: (id: number) =>
    queryOptions({
      queryKey: ['group', id],
      queryFn: () =>
        call(
          api().GET('/groups/{groupId}', { params: { path: { groupId: id } } }),
        ),
      refetchInterval: LIVE,
    }),
  runs: (
    filter: { groupId?: number; serverId?: number; limit?: number } = {},
  ) =>
    queryOptions({
      queryKey: ['runs', filter],
      queryFn: () => call(api().GET('/runs', { params: { query: filter } })),
      refetchInterval: LIVE,
    }),
  run: (id: number) =>
    queryOptions({
      queryKey: ['run', id],
      queryFn: () =>
        call(api().GET('/runs/{runId}', { params: { path: { runId: id } } })),
      refetchInterval: (query) =>
        query.state.data?.status === 'running' ? 3000 : false,
    }),
  plans: () =>
    queryOptions({
      queryKey: ['plans'],
      queryFn: () => call(api().GET('/plans')),
      refetchInterval: LIVE,
    }),
  keys: () =>
    queryOptions({
      queryKey: ['keys'],
      queryFn: () => call(api().GET('/keys')),
    }),
  offboarding: (label: string) =>
    queryOptions({
      queryKey: ['offboarding', label],
      queryFn: () =>
        call(api().GET('/offboarding', { params: { query: { label } } })),
    }),
  maintenance: () =>
    queryOptions({
      queryKey: ['maintenance'],
      queryFn: () => call(api().GET('/maintenance')),
    }),
  incidents: (includeAcknowledged = false) =>
    queryOptions({
      queryKey: ['incidents', includeAcknowledged],
      queryFn: () =>
        call(
          api().GET('/incidents', {
            params: { query: { includeAcknowledged } },
          }),
        ),
    }),
  alerts: (all = false) =>
    queryOptions({
      queryKey: ['alerts', all],
      queryFn: () => call(api().GET('/alerts', { params: { query: { all } } })),
      refetchInterval: LIVE,
    }),
  alertRules: () =>
    queryOptions({
      queryKey: ['alert-rules'],
      queryFn: () => call(api().GET('/alerts/rules')),
    }),
  channels: () =>
    queryOptions({
      queryKey: ['channels'],
      queryFn: () => call(api().GET('/channels')),
    }),
  certs: () =>
    queryOptions({
      queryKey: ['certs'],
      queryFn: () => call(api().GET('/certs')),
    }),
  tokens: () =>
    queryOptions({
      queryKey: ['tokens'],
      queryFn: () => call(api().GET('/tokens')),
    }),
  activity: () =>
    queryOptions({
      queryKey: ['activity'],
      queryFn: () =>
        call(api().GET('/activity', { params: { query: { limit: 200 } } })),
    }),
  sshKey: () =>
    queryOptions({
      queryKey: ['ssh-key'],
      queryFn: () => call(api().GET('/settings/ssh-key')),
      staleTime: Infinity,
    }),
  org: () =>
    queryOptions({
      queryKey: ['org'],
      queryFn: () => call(api().GET('/org')),
    }),
  members: () =>
    queryOptions({
      queryKey: ['members'],
      queryFn: () => call(api().GET('/org/members')),
    }),
  invites: () =>
    queryOptions({
      queryKey: ['invites'],
      queryFn: () => call(api().GET('/org/invites')),
    }),
  teams: () =>
    queryOptions({
      queryKey: ['teams'],
      queryFn: () => call(api().GET('/org/teams')),
    }),
  groupShares: (id: number) =>
    queryOptions({
      queryKey: ['group-shares', id],
      queryFn: () =>
        call(
          api().GET('/groups/{groupId}/shares', {
            params: { path: { groupId: id } },
          }),
        ),
    }),
  shareLinks: (id: number) =>
    queryOptions({
      queryKey: ['share-links', id],
      queryFn: () =>
        call(
          api().GET('/groups/{groupId}/share-links', {
            params: { path: { groupId: id } },
          }),
        ),
    }),
  // Link previews: a failed preview (expired, used, wrong email) does not get better on retry.
  invite: (token: string) =>
    queryOptions({
      queryKey: ['invite', token],
      queryFn: () =>
        call(api().GET('/invites/{token}', { params: { path: { token } } })),
      retry: false,
    }),
  sharePreview: (token: string) =>
    queryOptions({
      queryKey: ['share-preview', token],
      queryFn: () =>
        call(api().GET('/shares/{token}', { params: { path: { token } } })),
      retry: false,
    }),
}
