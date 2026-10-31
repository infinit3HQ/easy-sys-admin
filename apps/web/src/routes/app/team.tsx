import { createFileRoute, useNavigate } from '@tanstack/react-router'
import {
  useMutation,
  useQuery,
  useQueryClient,
  useSuspenseQuery,
} from '@tanstack/react-query'
import { useState } from 'react'
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
} from '#/components/kit'
import { CreateTeamDialog, roleText, useSessionReset } from '#/components/team'
import { Badge } from '#/components/ui/badge'
import { Button } from '#/components/ui/button'
import { Input } from '#/components/ui/input'
import { Separator } from '#/components/ui/separator'
import { useRole } from './route'

type Team = Schemas['Team']
type Member = Schemas['Member']

export const Route = createFileRoute('/app/team')({
  loader: ({ context }) => context.queryClient.ensureQueryData(q.team()),
  head: () => ({ meta: [{ title: 'Team · easy-sys-admin' }] }),
  component: TeamPage,
})

const roles: ReadonlyArray<{ value: Role; label: string }> = [
  { value: 'owner', label: 'owner' },
  { value: 'admin', label: 'admin' },
  { value: 'viewer', label: 'viewer' },
]

function TeamPage() {
  const { data: team } = useSuspenseQuery(q.team())
  return team.personal ? <PersonalSpace /> : <TeamSpace team={team} />
}

// ---- Personal space ------------------------------------------------------------

function PersonalSpace() {
  const { data: me } = useSuspenseQuery(q.me())
  const navigate = useNavigate()
  const reset = useSessionReset()
  const [creating, setCreating] = useState(false)
  const teams = me.accounts.filter((a) => !a.personal)
  const switchTo = useMutation({
    mutationFn: (accountId: number) =>
      call(api().PUT('/me/account', { body: { accountId } })),
    onSuccess: () => reset(() => navigate({ to: '/app/team' })),
  })

  return (
    <div>
      <PageHeader
        kicker="Space"
        title="Personal"
        description="This space is private. Only you can see its servers, groups, and runs, and nobody can be invited to it."
      >
        <Button onClick={() => setCreating(true)}>
          <PlusIcon /> Create a team
        </Button>
      </PageHeader>
      <div className="grid gap-6 xl:grid-cols-2">
        <Panel title="Working with other people">
          <div className="space-y-3 text-sm text-ink-2">
            <p>
              Create a team to look after servers with other people. A team has
              its own servers, groups, SSH key, and members. You can be in
              several teams and switch between them from the menu.
            </p>
            <p>
              You can also share a group from this space with a team. Open the
              group and create a share link in its sharing panel.
            </p>
          </div>
        </Panel>
        <Panel title={`Your teams · ${teams.length}`} flush>
          {teams.length === 0 ? (
            <p className="p-4 text-sm text-muted-foreground">
              You are not in a team yet. Create one, or ask a team owner for an
              invite link.
            </p>
          ) : (
            <ul className="divide-y divide-line/60">
              {teams.map((t) => (
                <li
                  key={t.id}
                  className="flex items-center gap-3 px-4 py-2.5 font-mono text-sm"
                >
                  <span className="min-w-0 truncate text-ink">{t.name}</span>
                  <Badge variant={t.role === 'viewer' ? 'outline' : 'amber'}>
                    {t.role}
                  </Badge>
                  <Button
                    size="xs"
                    variant="outline"
                    className="ml-auto"
                    busy={switchTo.isPending && switchTo.variables === t.id}
                    onClick={() => switchTo.mutate(t.id)}
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
      <CreateTeamDialog open={creating} onClose={() => setCreating(false)} />
    </div>
  )
}

// ---- Team ------------------------------------------------------------------------

function TeamSpace({ team }: { team: Team }) {
  const { canAct, isOwner } = useRole()
  return (
    <div>
      <TeamHeader team={team} />
      <div className={canAct ? 'grid gap-6 xl:grid-cols-2' : 'grid gap-6'}>
        <Members team={team} />
        {canAct && (
          <div className="min-w-0 space-y-6">
            <Invites />
            {isOwner && <DangerZone team={team} />}
          </div>
        )}
      </div>
    </div>
  )
}

function TeamHeader({ team }: { team: Team }) {
  const qc = useQueryClient()
  const { isOwner } = useRole()
  const [editing, setEditing] = useState(false)
  const [name, setName] = useState(team.name)
  const rename = useMutation({
    mutationFn: () =>
      call(api().PATCH('/team', { body: { name: name.trim() } })),
    onSuccess: async (t) => {
      qc.setQueryData(q.team().queryKey, t)
      setEditing(false)
      // The switcher lists account names.
      await qc.invalidateQueries({ queryKey: ['me'] })
    },
  })
  const kicker = (
    <>
      <span>Team</span>
      <Crumb />
      <span>{plural(team.memberCount, 'member')}</span>
      <Crumb />
      <span>you are {team.role}</span>
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
            aria-label="Team name"
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
                setName(team.name)
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
    <PageHeader kicker={kicker} title={team.name}>
      {isOwner && (
        <Button
          variant="outline"
          onClick={() => {
            setName(team.name)
            setEditing(true)
          }}
        >
          <PencilIcon /> Rename
        </Button>
      )}
    </PageHeader>
  )
}

function Members({ team }: { team: Team }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const reset = useSessionReset()
  const { data: me } = useSuspenseQuery(q.me())
  const { isOwner } = useRole()
  const members = useQuery(q.members())
  const [target, setTarget] = useState<Member | null>(null)
  const items = members.data?.items ?? []
  const owners = items.filter((m) => m.role === 'owner').length
  const personal = me.accounts.find((a) => a.personal)

  const setRole = useMutation({
    mutationFn: ({ userId, role }: { userId: number; role: Role }) =>
      call(
        api().PATCH('/team/members/{userId}', {
          params: { path: { userId } },
          body: { role },
        }),
      ),
    onSuccess: async (m) => {
      await qc.invalidateQueries({ queryKey: ['members'] })
      // Changing your own role changes what this page and the menu show.
      if (m.you) {
        await qc.invalidateQueries({ queryKey: ['me'] })
        await qc.invalidateQueries({ queryKey: ['team'] })
      }
    },
  })
  const remove = useMutation({
    mutationFn: async (m: Member) => {
      await call(
        api().DELETE('/team/members/{userId}', {
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
        await reset(() => navigate({ to: '/app/team' }))
        return
      }
      await qc.invalidateQueries({ queryKey: ['members'] })
      await qc.invalidateQueries({ queryKey: ['team'] })
    },
  })

  const leaving = target?.you ?? false
  const who = (m: Member) => m.name || m.email

  return (
    <Panel title={`Members · ${team.memberCount}`} flush>
      {members.isLoading ? (
        <div className="px-4">
          <Loading />
        </div>
      ) : (
        <ul className="divide-y divide-line/60">
          {items.map((m) => {
            const lastOwner = m.role === 'owner' && owners <= 1
            return (
              <li
                key={m.userId}
                className="grid gap-x-4 gap-y-2 px-4 py-3 sm:grid-cols-[minmax(0,1fr)_auto_auto] sm:items-center"
              >
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="min-w-0 truncate text-sm text-ink">
                      {who(m)}
                    </span>
                    {m.you && <Badge variant="amber">you</Badge>}
                  </div>
                  <div className="truncate font-mono text-[11px] text-muted-foreground">
                    {m.name ? `${m.email} · ` : ''}joined{' '}
                    <Ago iso={m.joinedAt} />
                  </div>
                </div>
                <div className="sm:w-28">
                  {isOwner ? (
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
                      options={roles}
                    />
                  ) : (
                    <Badge variant={m.role === 'viewer' ? 'outline' : 'amber'}>
                      {m.role}
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
                  ) : isOwner ? (
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
          A team keeps at least one owner. Make someone else an owner before you
          step down or leave.
        </p>
      )}

      <ConfirmDialog
        open={!!target}
        title={
          leaving ? `Leave ${team.name}` : `Remove ${target ? who(target) : ''}`
        }
        danger
        body={
          leaving
            ? 'You lose access to its servers, groups, and runs. You need a new invite to come back. You go to your personal space.'
            : 'They lose access to this team’s servers, groups, and runs. Their personal space stays as it is.'
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
        api().POST('/team/invites', {
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
        api().DELETE('/team/invites/{inviteId}', {
          params: { path: { inviteId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['invites'] }),
  })
  const list = invites.data?.items ?? []
  const options = isOwner ? [...roles].reverse() : roles.slice(1).reverse()

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

function DangerZone({ team }: { team: Team }) {
  const navigate = useNavigate()
  const reset = useSessionReset()
  const [open, setOpen] = useState(false)
  const del = useMutation({
    mutationFn: () =>
      call(
        api().DELETE('/team', { params: { query: { confirm: team.name } } }),
      ),
    onSuccess: async () => {
      setOpen(false)
      await reset(() => navigate({ to: '/app' }))
    },
  })
  const body =
    'Deletes the team with its servers, groups, runs, alerts, and history. Groups it shares and groups shared with it stop being shared. The esa helper stays on its servers until someone removes it.'
  return (
    <Panel title="Danger zone" className="border-fail/30">
      <p className="mb-3 text-xs text-muted-foreground">{body}</p>
      <Button variant="destructive" size="sm" onClick={() => setOpen(true)}>
        <Trash2Icon /> Delete team
      </Button>
      <ConfirmDialog
        open={open}
        title={`Delete ${team.name}`}
        danger
        confirmWord={team.name}
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
