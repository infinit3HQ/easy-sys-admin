import type { Group, Server } from '@esa/api'
import { useRole } from '#/routes/app/route'

type Effective = Server['yourRole']

/** Admin or owner on this group or server, after teams and share limits. */
const operates = (r: Effective) => r === 'admin' || r === 'owner'

/**
 * What the current user may do on a server. `yourRole` already counts teams
 * that make them an admin of the server's group, and the limit of a share.
 * A server in a group another organization shares keeps its settings with
 * that organization; "operate" access still lets admins here start runs,
 * act on services and containers, and read logs.
 */
export function useServerAccess(
  server: Pick<Server, 'access' | 'ownerTeam' | 'yourRole'>,
) {
  const { canAct } = useRole()
  const isOwn = server.access === 'own'
  return {
    isOwn,
    /** Runs, service and container actions, update checks, logs. */
    canOperate: operates(server.yourRole) && server.access !== 'view',
    /** Settings, helper, host key, enrollment, removal. */
    canManage: canAct && isOwn,
    ownerTeam: server.ownerTeam ?? 'another organization',
  }
}

/**
 * What the current user may do on a group. Runs follow `yourRole`, so a team
 * can make a viewer an admin of this group. Deleting the group and sharing
 * it stay with the organization role.
 */
export function useGroupAccess(group: Pick<Group, 'access' | 'yourRole'>) {
  const { canAct } = useRole()
  const isOwn = group.access === 'own'
  const admin = operates(group.yourRole)
  return {
    isOwn,
    /** Measure, clean, and update runs. */
    canRun: admin && group.access !== 'view',
    /** Name, timezone, cleanup and update schedules. */
    canEdit: admin && isOwn,
    /** Deleting the group and managing its shares. */
    canManage: canAct && isOwn,
  }
}
