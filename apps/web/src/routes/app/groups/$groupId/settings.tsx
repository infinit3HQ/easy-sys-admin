import { createFileRoute, useNavigate } from '@tanstack/react-router'
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from '@tanstack/react-query'
import { useState } from 'react'
import { Trash2Icon, UnlinkIcon } from 'lucide-react'
import type { GroupInput } from '@esa/api'
import { api, call, orNotFound } from '#/lib/api'
import { q } from '#/lib/queries'
import { GroupForm } from '#/components/group-form'
import { ConfirmDialog, Panel } from '#/components/kit'
import { Button } from '#/components/ui/button'
import { useRole } from '../../route'
import { useGroupAccess } from '#/components/server/use-access'
import { Route as GroupRoute } from './route'

export const Route = createFileRoute('/app/groups/$groupId/settings')({
  loader: ({ context, params }) =>
    orNotFound(context.queryClient.ensureQueryData(q.group(params.groupId))),
  head: ({ loaderData }) => ({
    meta: [
      {
        title: `${loaderData?.name ?? 'Group'} settings · easy-sys-admin`,
      },
    ],
  }),
  component: GroupSettings,
})

function GroupSettings() {
  const { groupId } = GroupRoute.useParams()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { canAct } = useRole()
  const { data: g } = useSuspenseQuery(q.group(groupId))
  const { isOwn, canEdit, canManage } = useGroupAccess(g)
  const [confirm, setConfirm] = useState<'delete' | 'unshare' | null>(null)

  const save = useMutation({
    mutationFn: (body: GroupInput) =>
      call(
        api().PUT('/groups/{groupId}', { params: { path: { groupId } }, body }),
      ),
    onSuccess: (d) => {
      qc.setQueryData(q.group(groupId).queryKey, d)
      return qc.invalidateQueries({ queryKey: ['groups'] })
    },
  })
  const del = useMutation({
    mutationFn: () =>
      call(
        api().DELETE('/groups/{groupId}', { params: { path: { groupId } } }),
      ),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['groups'] })
      await navigate({ to: '/app/groups' })
    },
  })
  const unshare = useMutation({
    mutationFn: (shareId: number) =>
      call(
        api().DELETE('/group-shares/{shareId}', {
          params: { path: { shareId } },
        }),
      ),
    onSuccess: async () => {
      setConfirm(null)
      await qc.invalidateQueries({ queryKey: ['groups'] })
      await qc.invalidateQueries({ queryKey: ['overview'] })
      await navigate({ to: '/app/groups' })
    },
  })

  return (
    <div className="mx-auto max-w-3xl">
      <p className="mb-6 text-sm text-muted-foreground">
        Name, timezone, and when this group is cleaned and patched.
      </p>
      <GroupForm
        key={g.id}
        initial={{
          name: g.name,
          timezone: g.timezone,
          clean: g.clean,
          updates: g.updates,
          checks: g.checks,
        }}
        submitLabel="Save group"
        busy={save.isPending}
        error={save.error}
        disabled={!canEdit}
        onSubmit={(b) => save.mutate(b)}
      />
      {canAct && !isOwn && g.shareId != null && (
        <Panel title="Shared with this organization" className="mt-6">
          <p className="mb-3 text-xs text-muted-foreground">
            Remove the group and its servers from this organization.{' '}
            {g.ownerTeam} keeps them, and needs to send a new link to share them
            again.
          </p>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setConfirm('unshare')}
          >
            <UnlinkIcon /> Remove from this organization
          </Button>
        </Panel>
      )}
      {canManage && (
        <Panel title="Danger zone" className="mt-6 border-fail/30">
          <p className="mb-3 text-xs text-muted-foreground">
            Its servers stay, without a group, and stop being cleaned and
            patched on a schedule.
          </p>
          <Button
            variant="destructive"
            size="sm"
            onClick={() => setConfirm('delete')}
          >
            <Trash2Icon /> Delete group
          </Button>
        </Panel>
      )}

      <ConfirmDialog
        open={confirm === 'delete'}
        title={`Delete ${g.name}`}
        danger
        confirmWord={g.name}
        body="Its servers stay, without a group, and stop being cleaned and patched on a schedule."
        actionLabel="Delete"
        busy={del.isPending}
        error={del.error}
        onConfirm={() => del.mutate()}
        onClose={() => setConfirm(null)}
      />
      <ConfirmDialog
        open={confirm === 'unshare'}
        title={`Remove ${g.name} from this organization`}
        danger
        body={`This organization stops seeing the group and its servers. ${g.ownerTeam ?? 'The owner'} keeps them.`}
        actionLabel="Remove"
        busy={unshare.isPending}
        error={unshare.error}
        onConfirm={() => g.shareId != null && unshare.mutate(g.shareId)}
        onClose={() => setConfirm(null)}
      />
    </div>
  )
}
