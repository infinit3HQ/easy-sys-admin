import { Link, createFileRoute, useNavigate } from '@tanstack/react-router'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { GroupInput } from '@esa/api'
import { api, call } from '#/lib/api'
import { GroupForm, defaultGroup } from '#/components/group-form'
import { PageHeader } from '#/components/ui'

export const Route = createFileRoute('/app/groups/new')({
  head: () => ({ meta: [{ title: 'New group · easy-sys-admin' }] }),
  component: NewGroup,
})

function NewGroup() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const m = useMutation({
    mutationFn: (g: GroupInput) => call(api().POST('/groups', { body: g })),
    onSuccess: async (g) => {
      await qc.invalidateQueries({ queryKey: ['groups'] })
      await navigate({ to: '/app/groups/$groupId', params: { groupId: g.id } })
    },
  })
  return (
    <div className="max-w-3xl">
      <PageHeader kicker={<Link to="/app/groups">Groups</Link>} title="New group" />
      <GroupForm initial={defaultGroup} submitLabel="Create group" busy={m.isPending} error={m.error} onSubmit={(g) => m.mutate(g)} />
    </div>
  )
}
