import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { orNotFound } from '#/lib/api'
import { q } from '#/lib/queries'
import { GroupSharing } from '#/components/group-sharing'
import { Notice } from '#/components/kit'
import { useGroupAccess } from '#/components/server/use-access'
import { Route as GroupRoute } from './route'

export const Route = createFileRoute('/app/groups/$groupId/sharing')({
  loader: ({ context, params }) =>
    orNotFound(context.queryClient.ensureQueryData(q.group(params.groupId))),
  head: ({ loaderData }) => ({
    meta: [
      {
        title: `Share ${loaderData?.name ?? 'group'} · easy-sys-admin`,
      },
    ],
  }),
  component: GroupSharePage,
})

function GroupSharePage() {
  const { groupId } = GroupRoute.useParams()
  const { data: g } = useSuspenseQuery(q.group(groupId))
  const { canManage } = useGroupAccess(g)

  if (!canManage) {
    return (
      <Notice>Sharing stays with the organization that owns this group.</Notice>
    )
  }

  return (
    <div className="mx-auto max-w-3xl">
      <p className="mb-6 text-sm text-muted-foreground">
        Who else can see this group, and links you can send them.
      </p>
      <GroupSharing groupId={g.id} />
    </div>
  )
}
