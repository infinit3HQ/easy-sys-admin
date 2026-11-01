import { createFileRoute, useNavigate } from '@tanstack/react-router'
import {
  useMutation,
  useQuery,
  useQueryClient,
  useSuspenseQuery,
} from '@tanstack/react-query'
import type { QueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import type { ReactNode } from 'react'
import {
  ArrowRightIcon,
  LinkIcon,
  PencilIcon,
  PlusIcon,
  Trash2Icon,
} from 'lucide-react'
import type { Role, Schemas } from '@esa/api'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { plural } from '#/lib/format'
import { Ago, Until } from '#/components/time'
import {
  Choice,
  ConfirmDialog,
  CopyBlock,
  Crumb,
  ErrorBanner,
  Field,
  Loading,
  Notice,
  PageHeader,
  Panel,
  Segmented,
  Toggle,
} from '#/components/kit'
import {
  CreateOrganizationDialog,
  roleText,
  useSessionReset,
} from '#/components/org'
import { Badge } from '#/components/ui/badge'
import { Button } from '#/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '#/components/ui/dialog'
import { Input } from '#/components/ui/input'
import { Separator } from '#/components/ui/separator'
import { useRole } from './route'

type Organization = Schemas['Organization']
type Member = Schemas['Member']
type Team = Schemas['Team']

export const Route = createFileRoute('/app/org')({
  loader: ({ context }) => context.queryClient.ensureQueryData(q.org()),
  head: () => ({ meta: [{ title: 'Organization · easy-sys-admin' }] }),
  component: OrgPage,
})

const TEAM_TEXT =
  "Members of a team are admins of its server groups: they can run updates and cleanups, act on services and containers, read logs, and change those groups' settings."

function OrgPage() {
  const { data: org } = useSuspenseQuery(q.org())
  return org.personal ? <PersonalSpace /> : <OrgSpace org={org} />
}

/** The name of a role as this organization shows it. */
function roleName(org: Organization, role: Role) {
  if (role === 'owner') return 'owner'
  return role === 'admin' ? org.adminTeamName : org.viewerTeamName
}

/**
 * Team changes move admin rights on server groups, so group and server
 * pages can change as well as the team list.
 */
function refreshTeams(qc: QueryClient) {
  return Promise.all(
    ['teams', 'members', 'groups', 'group', 'server'].map((k) =>
      qc.invalidateQueries({ queryKey: [k] }),
    ),
  )
}

/** A small mono label for a team or server group name. */
function Tag({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex max-w-full items-center truncate rounded-sm border border-line-2 px-1.5 py-0.5 font-mono text-[10px] leading-none text-ink-2">
      {children}
    </span>
  )
}

// ---- Personal space ------------------------------------------------------------

function PersonalSpace() {
  const { data: me } = useSuspenseQuery(q.me())
  const navigate = useNavigate()
  const reset = useSessionReset()
  const [creating, setCreating] = useState(false)
  const orgs = me.accounts.filter((a) => !a.personal)
  const switchTo = useMutation({
    mutationFn: (accountId: number) =>
      call(api().PUT('/me/account', { body: { accountId } })),
    onSuccess: () => reset(() => navigate({ to: '/app/org' })),
  })

  return (
    <div>
      <PageHeader
        kicker="Space"
        title="Personal"
        description="This space is private. Only you can see its servers, groups, and runs, and nobody can be invited to it."
      >
        <Button onClick={() => setCreating(true)}>
          <PlusIcon /> Create an organization
        </Button>
      </PageHeader>
      <div className="grid gap-6 xl:grid-cols-2">
        <Panel title="Working with other people">
          <div className="space-y-3 text-sm text-ink-2">
            <p>
              Create an organization to look after servers with other people. An
              organization has its own servers, groups, SSH key, and members.
              Each member is an owner, an admin, or a viewer, and teams make
              members admins of chosen server groups.
            </p>
            <p>
              You can be in several organizations and switch between them from
              the menu.
            </p>
            <p>
              You can also share a group from this space with an organization.
              Open the group and create a share link in its sharing panel.
            </p>
          </div>
        </Panel>
        <Panel title={`Your organizations · ${orgs.length}`} flush>
          {orgs.length === 0 ? (
            <p className="p-4 text-sm text-muted-foreground">
              You are not in an organization yet. Create one, or ask an owner or
              admin of one for an invite link.
            </p>
          ) : (
            <ul className="divide-y divide-line/60">
              {orgs.map((o) => (
                <li
                  key={o.id}
                  className="flex items-center gap-3 px-4 py-2.5 font-mono text-sm"
                >
                  <span className="min-w-0 truncate text-ink">{o.name}</span>
                  <Badge variant={o.role === 'viewer' ? 'outline' : 'amber'}>
                    {o.role}
                  </Badge>
                  <Button
                    size="xs"
                    variant="outline"
                    className="ml-auto"
                    busy={switchTo.isPending && switchTo.variables === o.id}
                    onClick={() => switchTo.mutate(o.id)}
                  >
                    Switch <ArrowRightIcon />
                  </Button>
                </li>
              ))}
            </ul>
          )}
          <ErrorBanner error={switchTo.error} className="m-4" />
        </Panel>
      </div>
      <CreateOrganizationDialog
        open={creating}
        onClose={() => setCreating(false)}
      />
    </div>
  )
}

// ---- Organization --------------------------------------------------------------

function OrgSpace({ org }: { org: Organization }) {
  const { canAct, isOwner } = useRole()
  return (
    <div>
      <OrgHeader org={org} />
      <div className="grid gap-6 xl:grid-cols-2">
        <div className="min-w-0">
          <Members org={org} />
        </div>
        <div className="min-w-0 space-y-6">
          <Teams org={org} />
          {canAct && <Invites />}
          {isOwner && <DangerZone org={org} />}
        </div>
      </div>
    </div>
  )
}

function OrgHeader({ org }: { org: Organization }) {
  const qc = useQueryClient()
  const { isOwner } = useRole()
  const [editing, setEditing] = useState(false)
  const [name, setName] = useState(org.name)
  const rename = useMutation({
    mutationFn: () =>
      call(api().PATCH('/org', { body: { name: name.trim() } })),
    onSuccess: async (o) => {
      qc.setQueryData(q.org().queryKey, o)
      setEditing(false)
      // The switcher lists account names.
      await qc.invalidateQueries({ queryKey: ['me'] })
    },
  })
  const kicker = (
    <>
      <span>Organization</span>
      <Crumb />
      <span>{plural(org.memberCount, 'member')}</span>
      <Crumb />
      <span>you are {org.role}</span>
    </>
  )

  if (editing) {
    return (
      <div className="mb-6 animate-rise">
        <div className="label mb-1.5 flex flex-wrap items-center gap-1.5">
          {kicker}
        </div>
        <form
          className="flex flex-col gap-2 sm:flex-row sm:items-center"
          onSubmit={(e) => {
            e.preventDefault()
            if (name.trim()) rename.mutate()
          }}
        >
          <Input
            aria-label="Organization name"
            autoFocus
            autoComplete="off"
            maxLength={100}
            required
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="h-12 font-display text-3xl font-bold uppercase sm:max-w-md md:text-3xl"
          />
          <div className="flex gap-2">
            <Button type="submit" busy={rename.isPending}>
              Save
            </Button>
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                setName(org.name)
                rename.reset()
                setEditing(false)
              }}
            >
              Cancel
            </Button>
          </div>
        </form>
        <ErrorBanner error={rename.error} className="mt-3" />
      </div>
    )
  }

  return (
    <PageHeader kicker={kicker} title={org.name}>
      {isOwner && (
        <Button
          variant="outline"
          onClick={() => {
            setName(org.name)
            setEditing(true)
          }}
        >
          <PencilIcon /> Rename
        </Button>
      )}
    </PageHeader>
  )
}

// ---- Members -------------------------------------------------------------------

function Members({ org }: { org: Organization }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const reset = useSessionReset()
  const { data: me } = useSuspenseQuery(q.me())
  const { canAct, isOwner } = useRole()
  const members = useQuery(q.members())
  const teams = useQuery(q.teams())
  const [target, setTarget] = useState<Member | null>(null)
  const items = members.data?.items ?? []
  const owners = items.filter((m) => m.role === 'owner').length
  const personal = me.accounts.find((a) => a.personal)
  const teamName = new Map((teams.data?.items ?? []).map((t) => [t.id, t.name]))

  const setRole = useMutation({
    mutationFn: ({ userId, role }: { userId: number; role: Role }) =>
      call(
        api().PATCH('/org/members/{userId}', {
          params: { path: { userId } },
          body: { role },
        }),
      ),
    onSuccess: async (m) => {
      // Your own role decides what every page shows.
      if (m.you) {
        await qc.invalidateQueries()
        return
      }
      await qc.invalidateQueries({ queryKey: ['members'] })
    },
  })
  const remove = useMutation({
    mutationFn: async (m: Member) => {
      await call(
        api().DELETE('/org/members/{userId}', {
          params: { path: { userId: m.userId } },
        }),
      )
      // Leaving: work in the personal space from now on.
      if (m.you && personal) {
        await call(
          api().PUT('/me/account', { body: { accountId: personal.id } }),
        )
      }
    },
    onSuccess: async (_, m) => {
      setTarget(null)
      if (m.you) {
        await reset(() => navigate({ to: '/app/org' }))
        return
      }
      await qc.invalidateQueries({ queryKey: ['members'] })
      await qc.invalidateQueries({ queryKey: ['org'] })
      await qc.invalidateQueries({ queryKey: ['teams'] })
    },
  })

  // Only owners grant, take away, or remove the owner role.
  const canEditRole = (m: Member) => isOwner || (canAct && m.role !== 'owner')
  const canRemove = (m: Member) => !m.you && canEditRole(m)
  const roleOptions = [
    ...(isOwner ? [{ value: 'owner', label: 'Owner' }] : []),
    { value: 'admin', label: org.adminTeamName },
    { value: 'viewer', label: org.viewerTeamName },
  ]
  const leaving = target?.you ?? false
  const who = (m: { name: string; email: string }) => m.name || m.email

  return (
    <Panel title={`Members · ${org.memberCount}`} flush>
      {members.isLoading ? (
        <div className="px-4">
          <Loading />
        </div>
      ) : (
        <ul className="divide-y divide-line/60">
          {items.map((m) => {
            const lastOwner = m.role === 'owner' && owners <= 1
            const tags = m.teamIds.flatMap((id) => {
              const n = teamName.get(id)
              return n ? [{ id, name: n }] : []
            })
            return (
              <li
                key={m.userId}
                className="grid gap-x-4 gap-y-2 px-4 py-3 sm:grid-cols-[minmax(0,1fr)_9rem_auto] sm:items-center"
              >
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="min-w-0 truncate text-sm text-ink">
                      {who(m)}
                    </span>
                    {m.role === 'owner' && <Badge>owner</Badge>}
                    {m.you && <Badge variant="amber">you</Badge>}
                  </div>
                  <div className="truncate font-mono text-[11px] text-muted-foreground">
                    {m.name ? `${m.email} · ` : ''}joined{' '}
                    <Ago iso={m.joinedAt} />
                  </div>
                  {tags.length > 0 && (
                    <div className="mt-1.5 flex flex-wrap gap-1">
                      {tags.map((t) => (
                        <Tag key={t.id}>{t.name}</Tag>
                      ))}
                    </div>
                  )}
                </div>
                <div className="min-w-0">
                  {canEditRole(m) ? (
                    <Choice
                      aria-label={`Role of ${who(m)}`}
                      size="sm"
                      className="font-mono text-xs"
                      value={m.role}
                      disabled={
                        lastOwner ||
                        (setRole.isPending &&
                          setRole.variables.userId === m.userId)
                      }
                      onChange={(v) =>
                        setRole.mutate({ userId: m.userId, role: v as Role })
                      }
                      options={roleOptions}
                    />
                  ) : m.role === 'owner' ? null : (
                    <Badge
                      variant={m.role === 'viewer' ? 'outline' : 'amber'}
                      className="max-w-full truncate"
                    >
                      {roleName(org, m.role)}
                    </Badge>
                  )}
                </div>
                <div className="flex justify-start sm:justify-end">
                  {m.you ? (
                    <Button
                      size="xs"
                      variant="ghost"
                      className="hover:text-fail"
                      disabled={lastOwner}
                      title={
                        lastOwner
                          ? 'You are the only owner. Make someone else an owner first.'
                          : undefined
                      }
                      onClick={() => setTarget(m)}
                    >
                      leave
                    </Button>
                  ) : canRemove(m) ? (
                    <Button
                      size="xs"
                      variant="ghost"
                      className="hover:text-fail"
                      onClick={() => setTarget(m)}
                    >
                      remove
                    </Button>
                  ) : null}
                </div>
              </li>
            )
          })}
        </ul>
      )}
      {(members.error || setRole.error) && (
        <div className="space-y-3 p-4">
          <ErrorBanner error={members.error} />
          <ErrorBanner error={setRole.error} />
        </div>
      )}
      {isOwner && owners <= 1 && items.length > 0 && (
        <p className="border-t border-line/60 px-4 py-3 text-xs text-muted-foreground">
          An organization keeps at least one owner. Make someone else an owner
          before you step down or leave.
        </p>
      )}

      <ConfirmDialog
        open={!!target}
        title={
          leaving ? `Leave ${org.name}` : `Remove ${target ? who(target) : ''}`
        }
        danger
        body={
          leaving
            ? 'You lose access to its servers, groups, and runs. You need a new invite to come back. You go to your personal space.'
            : "They lose access to this organization's servers, groups, and runs, and leave its teams. Their personal space stays as it is."
        }
        actionLabel={leaving ? 'Leave' : 'Remove'}
        busy={remove.isPending}
        error={remove.error}
        onConfirm={() => target && remove.mutate(target)}
        onClose={() => {
          setTarget(null)
          remove.reset()
        }}
      />
    </Panel>
  )
}

// ---- Teams ---------------------------------------------------------------------

function Teams({ org }: { org: Organization }) {
  const { canAct } = useRole()
  const members = useQuery(q.members())
  const teams = useQuery(q.teams())
  // The dialog keeps the team it opened with while it animates closed.
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editTeam, setEditTeam] = useState<Team | null>(null)
  const items = members.data?.items ?? []
  const count = (r: Role) => items.filter((m) => m.role === r).length
  const extra = teams.data?.items ?? []
  const openDialog = (t: Team | null) => {
    setEditTeam(t)
    setDialogOpen(true)
  }

  return (
    <Panel
      title={`Teams · ${extra.length + 2}`}
      actions={
        canAct && (
          <Button size="xs" variant="outline" onClick={() => openDialog(null)}>
            <PlusIcon /> New team
          </Button>
        )
      }
      flush
    >
      <ul className="divide-y divide-line/60">
        <BuiltInTeam
          org={org}
          field="adminTeamName"
          count={count('admin')}
          text="Everyone with the admin role. Admins manage everything in the organization."
        />
        <BuiltInTeam
          org={org}
          field="viewerTeamName"
          count={count('viewer')}
          text="Everyone with the viewer role. Viewers see everything and change nothing."
        />
      </ul>
      <p className="border-t border-line/60 px-4 py-3 text-xs text-muted-foreground">
        {TEAM_TEXT}
      </p>
      {teams.isLoading ? (
        <div className="border-t border-line/60 px-4">
          <Loading />
        </div>
      ) : extra.length === 0 ? (
        <p className="border-t border-line/60 px-4 py-3 text-sm text-muted-foreground">
          No other teams yet.
        </p>
      ) : (
        <ul className="divide-y divide-line/60 border-t border-line/60">
          {extra.map((t) => (
            <TeamRow
              key={t.id}
              team={t}
              members={items}
              onEdit={() => openDialog(t)}
            />
          ))}
        </ul>
      )}
      <ErrorBanner error={teams.error} className="m-4" />
      <TeamDialog
        open={dialogOpen}
        team={editTeam}
        onClose={() => setDialogOpen(false)}
      />
    </Panel>
  )
}

/** Admins or viewers: the team is the role, so only its name can change. */
function BuiltInTeam({
  org,
  field,
  count,
  text,
}: {
  org: Organization
  field: 'adminTeamName' | 'viewerTeamName'
  count: number
  text: string
}) {
  const qc = useQueryClient()
  const { canAct } = useRole()
  const name = org[field]
  const [editing, setEditing] = useState(false)
  const [value, setValue] = useState(name)
  const rename = useMutation({
    mutationFn: () => {
      const v = value.trim()
      return call(
        api().PATCH('/org', {
          body:
            field === 'adminTeamName'
              ? { adminTeamName: v }
              : { viewerTeamName: v },
        }),
      )
    },
    onSuccess: (o) => {
      qc.setQueryData(q.org().queryKey, o)
      setEditing(false)
    },
  })

  return (
    <li className="px-4 py-3">
      {editing ? (
        <form
          className="flex flex-col gap-2 sm:flex-row sm:items-center"
          onSubmit={(e) => {
            e.preventDefault()
            if (value.trim()) rename.mutate()
          }}
        >
          <Input
            aria-label="Team name"
            autoFocus
            autoComplete="off"
            maxLength={100}
            required
            value={value}
            onChange={(e) => setValue(e.target.value)}
            className="font-mono sm:max-w-xs"
          />
          <div className="flex gap-2">
            <Button type="submit" size="sm" busy={rename.isPending}>
              Save
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => {
                rename.reset()
                setEditing(false)
              }}
            >
              Cancel
            </Button>
          </div>
        </form>
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          <span className="min-w-0 truncate font-mono text-sm text-ink">
            {name}
          </span>
          <Badge variant="secondary">built in</Badge>
          <span className="font-mono text-[11px] text-muted-foreground">
            {plural(count, 'member')}
          </span>
          {canAct && (
            <Button
              size="xs"
              variant="ghost"
              className="ml-auto"
              aria-label={`Rename ${name}`}
              onClick={() => {
                setValue(name)
                setEditing(true)
              }}
            >
              <PencilIcon /> rename
            </Button>
          )}
        </div>
      )}
      <p className="mt-1 text-xs text-muted-foreground">{text}</p>
      <ErrorBanner error={rename.error} className="mt-2" />
    </li>
  )
}

function TeamRow({
  team,
  members,
  onEdit,
}: {
  team: Team
  members: ReadonlyArray<Member>
  onEdit: () => void
}) {
  const qc = useQueryClient()
  const { canAct } = useRole()
  const [deleting, setDeleting] = useState(false)
  const inTeam = new Set(team.members.map((m) => m.userId))
  const candidates = members.filter((m) => !inTeam.has(m.userId))
  const path = (userId: number) => ({
    params: { path: { teamId: team.id, userId } },
  })
  const add = useMutation({
    mutationFn: (userId: number) =>
      call(api().PUT('/org/teams/{teamId}/members/{userId}', path(userId))),
    onSuccess: () => refreshTeams(qc),
  })
  const drop = useMutation({
    mutationFn: (userId: number) =>
      call(api().DELETE('/org/teams/{teamId}/members/{userId}', path(userId))),
    onSuccess: () => refreshTeams(qc),
  })
  const del = useMutation({
    mutationFn: () =>
      call(
        api().DELETE('/org/teams/{teamId}', {
          params: { path: { teamId: team.id } },
        }),
      ),
    onSuccess: async () => {
      setDeleting(false)
      await refreshTeams(qc)
    },
  })
  const who = (m: { name: string; email: string }) => m.name || m.email

  return (
    <li className="space-y-3 px-4 py-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="min-w-0 truncate font-mono text-sm text-ink">
          {team.name}
        </span>
        <span className="font-mono text-[11px] text-muted-foreground">
          {plural(team.members.length, 'member')}
        </span>
        {canAct && (
          <span className="ml-auto flex gap-1">
            <Button
              size="xs"
              variant="ghost"
              aria-label={`Edit ${team.name}`}
              onClick={onEdit}
            >
              <PencilIcon /> edit
            </Button>
            <Button
              size="xs"
              variant="ghost"
              className="hover:text-fail"
              aria-label={`Delete ${team.name}`}
              onClick={() => setDeleting(true)}
            >
              <Trash2Icon /> delete
            </Button>
          </span>
        )}
      </div>
      <div>
        <div className="label mb-1.5">Server groups</div>
        {team.groups.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            None yet, so the team gives no rights.
          </p>
        ) : (
          <div className="flex flex-wrap gap-1">
            {team.groups.map((g) => (
              <Tag key={g.id}>
                {g.name}
                {g.ownerTeam && (
                  <span className="ml-1 text-faint">· {g.ownerTeam}</span>
                )}
              </Tag>
            ))}
          </div>
        )}
      </div>
      <div>
        <div className="label mb-1.5">Members</div>
        {team.members.length === 0 ? (
          <p className="text-xs text-muted-foreground">Nobody yet.</p>
        ) : (
          <ul className="space-y-1">
            {team.members.map((m) => (
              <li key={m.userId} className="flex min-h-7 items-center gap-2">
                <span className="min-w-0 truncate text-sm text-ink">
                  {who(m)}
                </span>
                {m.name && (
                  <span className="hidden min-w-0 truncate font-mono text-[11px] text-muted-foreground sm:inline">
                    {m.email}
                  </span>
                )}
                {canAct && (
                  <Button
                    size="xs"
                    variant="ghost"
                    className="ml-auto hover:text-fail"
                    aria-label={`Take ${who(m)} out of ${team.name}`}
                    busy={drop.isPending && drop.variables === m.userId}
                    onClick={() => drop.mutate(m.userId)}
                  >
                    remove
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
        {canAct && candidates.length > 0 && (
          <Choice
            aria-label={`Add a member to ${team.name}`}
            size="sm"
            className="mt-2 font-mono text-xs sm:max-w-xs"
            placeholder="Add a member"
            value=""
            disabled={add.isPending}
            onChange={(v) => add.mutate(Number(v))}
            options={candidates.map((m) => ({
              value: String(m.userId),
              label: who(m),
            }))}
          />
        )}
      </div>
      <ErrorBanner error={add.error ?? drop.error} />
      <ConfirmDialog
        open={deleting}
        title={`Delete ${team.name}`}
        danger
        body="Its members lose admin rights on its server groups. They keep their organization role."
        actionLabel="Delete"
        busy={del.isPending}
        error={del.error}
        onConfirm={() => del.mutate()}
        onClose={() => {
          setDeleting(false)
          del.reset()
        }}
      />
    </li>
  )
}

/** Create a team, or change its name and server groups. */
function TeamDialog({
  open,
  team,
  onClose,
}: {
  open: boolean
  team: Team | null
  onClose: () => void
}) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        {/* The content mounts on every open, so the form starts from the team as it is now. */}
        <TeamForm team={team} onClose={onClose} />
      </DialogContent>
    </Dialog>
  )
}

function TeamForm({
  team,
  onClose,
}: {
  team: Team | null
  onClose: () => void
}) {
  const qc = useQueryClient()
  const groups = useQuery(q.groups())
  const [name, setName] = useState(team?.name ?? '')
  const [picked, setPicked] = useState<ReadonlySet<number>>(
    () => new Set(team?.groups.map((g) => g.id)),
  )
  const save = useMutation({
    mutationFn: () => {
      const body = { name: name.trim(), groupIds: [...picked] }
      return team
        ? call(
            api().PUT('/org/teams/{teamId}', {
              params: { path: { teamId: team.id } },
              body,
            }),
          )
        : call(api().POST('/org/teams', { body }))
    },
    onSuccess: async () => {
      onClose()
      await refreshTeams(qc)
    },
  })
  // Own groups and groups shared with this organization. A group the team
  // holds stays listed even when the group list no longer has it.
  const listed = (groups.data?.items ?? []).map((g) => ({
    id: g.id,
    name: g.name,
    ownerTeam: g.ownerTeam,
  }))
  const options = [
    ...listed,
    ...(team?.groups ?? []).filter((g) => !listed.some((l) => l.id === g.id)),
  ]
  const toggle = (id: number, on: boolean) =>
    setPicked((prev) => {
      const next = new Set(prev)
      if (on) next.add(id)
      else next.delete(id)
      return next
    })

  return (
    <form
      className="grid gap-4"
      onSubmit={(e) => {
        e.preventDefault()
        if (name.trim()) save.mutate()
      }}
    >
      <DialogHeader>
        <DialogTitle className="break-words">
          {team ? `Edit ${team.name}` : 'New team'}
        </DialogTitle>
        <DialogDescription className="text-ink-2">
          Members of the team are admins of the server groups you pick here.
          {team ? '' : ' Add members once the team exists.'}
        </DialogDescription>
      </DialogHeader>
      <Field label="Name">
        <Input
          autoFocus
          autoComplete="off"
          maxLength={100}
          required
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="web"
        />
      </Field>
      <div className="grid min-w-0 gap-1.5">
        <div className="label">Server groups · {picked.size}</div>
        {groups.isLoading ? (
          <Loading />
        ) : options.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            No groups yet. Create one on the groups page first.
          </p>
        ) : (
          <div className="max-h-64 overflow-y-auto rounded-md border border-line px-3 py-1.5">
            {options.map((g) => (
              <Toggle
                key={g.id}
                checked={picked.has(g.id)}
                onChange={(on) => toggle(g.id, on)}
                label={<span className="font-mono">{g.name}</span>}
                hint={g.ownerTeam ? `Shared by ${g.ownerTeam}` : undefined}
              />
            ))}
          </div>
        )}
      </div>
      <ErrorBanner error={groups.error ?? save.error} />
      <DialogFooter className="pt-2">
        <Button type="button" variant="ghost" onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" busy={save.isPending} disabled={!name.trim()}>
          {team ? 'Save team' : 'Create team'}
        </Button>
      </DialogFooter>
    </form>
  )
}

// ---- Invites -------------------------------------------------------------------

function Invites() {
  const qc = useQueryClient()
  const { isOwner } = useRole()
  const invites = useQuery(q.invites())
  const [role, setRole] = useState<Role>('viewer')
  const [email, setEmail] = useState('')
  const [created, setCreated] = useState<Schemas['CreatedInvite'] | null>(null)
  const create = useMutation({
    mutationFn: () =>
      call(
        api().POST('/org/invites', {
          body: { role, email: email.trim() || undefined },
        }),
      ),
    onSuccess: (r) => {
      setCreated(r)
      setEmail('')
      return qc.invalidateQueries({ queryKey: ['invites'] })
    },
  })
  const revoke = useMutation({
    mutationFn: (inviteId: number) =>
      call(
        api().DELETE('/org/invites/{inviteId}', {
          params: { path: { inviteId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['invites'] }),
  })
  const list = invites.data?.items ?? []
  // Only owners invite owners.
  const options: ReadonlyArray<{ value: Role; label: string }> = [
    { value: 'viewer', label: 'viewer' },
    { value: 'admin', label: 'admin' },
    ...(isOwner ? [{ value: 'owner' as const, label: 'owner' }] : []),
  ]

  return (
    <Panel title="Invites">
      {created && (
        <div className="mb-6 space-y-3">
          <Notice tone="warn">
            Copy the link now. esa stores only a hash and cannot show it again.
          </Notice>
          <CopyBlock value={created.url} />
          <p className="text-xs text-muted-foreground">
            It works once, for{' '}
            {created.invite.email ? (
              <span className="font-mono text-ink-2">
                {created.invite.email}
              </span>
            ) : (
              'anyone with the link'
            )}
            , and expires <Until iso={created.invite.expiresAt} />. They join as{' '}
            {created.invite.role}.
          </p>
        </div>
      )}
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
      >
        <Field label="Role" hint={roleText[role]}>
          <Segmented
            aria-label="Role"
            value={role}
            onChange={setRole}
            options={options}
          />
        </Field>
        <Field
          label="Email (optional)"
          hint="Only this address can accept. Leave it empty for anyone with the link."
        >
          <Input
            type="email"
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            maxLength={200}
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="sam@example.com"
          />
        </Field>
        <ErrorBanner error={create.error} />
        <Button
          type="submit"
          busy={create.isPending}
          className="w-full sm:w-auto"
        >
          <LinkIcon /> Create invite link
        </Button>
      </form>
      <ErrorBanner error={invites.error ?? revoke.error} className="mt-4" />
      {list.length > 0 && (
        <>
          <Separator className="my-5" />
          <div className="label mb-3">Pending · {list.length}</div>
          <ul className="space-y-3">
            {list.map((i) => (
              <li
                key={i.id}
                className="flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-xs"
              >
                <Badge variant={i.role === 'viewer' ? 'outline' : 'amber'}>
                  {i.role}
                </Badge>
                <span className="min-w-0 truncate text-ink">
                  {i.email || 'anyone with the link'}
                </span>
                <span className="text-faint">by {i.createdBy}</span>
                <span className="ml-auto flex items-center gap-2">
                  <span className="text-faint">
                    expires <Until iso={i.expiresAt} />
                  </span>
                  <Button
                    size="xs"
                    variant="ghost"
                    className="hover:text-fail"
                    onClick={() => revoke.mutate(i.id)}
                    busy={revoke.isPending && revoke.variables === i.id}
                  >
                    revoke
                  </Button>
                </span>
              </li>
            ))}
          </ul>
        </>
      )}
    </Panel>
  )
}

// ---- Danger zone ---------------------------------------------------------------

function DangerZone({ org }: { org: Organization }) {
  const navigate = useNavigate()
  const reset = useSessionReset()
  const [open, setOpen] = useState(false)
  const del = useMutation({
    mutationFn: () =>
      call(api().DELETE('/org', { params: { query: { confirm: org.name } } })),
    onSuccess: async () => {
      setOpen(false)
      await reset(() => navigate({ to: '/app' }))
    },
  })
  const body =
    'Deletes the organization with its servers, groups, teams, runs, alerts, and history. Groups it shares and groups shared with it stop being shared. The esa helper stays on its servers until someone removes it.'
  return (
    <Panel title="Danger zone" className="border-fail/30">
      <p className="mb-3 text-xs text-muted-foreground">{body}</p>
      <Button variant="destructive" size="sm" onClick={() => setOpen(true)}>
        <Trash2Icon /> Delete organization
      </Button>
      <ConfirmDialog
        open={open}
        title={`Delete ${org.name}`}
        danger
        confirmWord={org.name}
        body={body}
        actionLabel="Delete"
        busy={del.isPending}
        error={del.error}
        onConfirm={() => del.mutate()}
        onClose={() => {
          setOpen(false)
          del.reset()
        }}
      />
    </Panel>
  )
}
