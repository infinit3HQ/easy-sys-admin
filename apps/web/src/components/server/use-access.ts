import type { Server } from '@esa/api'
import { useRole } from '#/routes/app/route'

/**
 * What the current user may do on a server. A server in a group another
 * team shares keeps its settings with that team; "operate" access still
 * lets admins here start runs, act on services and containers, and read logs.
 */
export function useServerAccess(server: Pick<Server, 'access' | 'ownerTeam'>) {
  const { canAct } = useRole()
  const isOwn = server.access === 'own'
  return {
    isOwn,
    /** Runs, service and container actions, update checks, logs. */
    canOperate: canAct && server.access !== 'view',
    /** Settings, helper, host key, enrollment, removal. */
    canManage: canAct && isOwn,
    ownerTeam: server.ownerTeam ?? 'another team',
  }
}
