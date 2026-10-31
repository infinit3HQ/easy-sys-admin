import {
  Link,
  createFileRoute,
  redirect,
  useNavigate,
} from '@tanstack/react-router'
import { useMutation, useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { LayersIcon, PlusIcon } from 'lucide-react'
import { ApiError, api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { plural } from '#/lib/format'
import { Until } from '#/components/time'
import {
  Choice,
  ErrorBanner,
  Field,
  KeyValue,
  Loading,
  Notice,
} from '#/components/kit'
import { LinkShell, useSessionReset } from '#/components/team'
import { Button } from '#/components/ui/button'

export const Route = createFileRoute('/share/$token')({
  // Accepting needs a browser session: send visitors who are not signed in
  // to the login page and bring them back here afterwards.
  beforeLoad: async ({ context, location, params }) => {
    try {
      await context.queryClient.ensureQueryData(q.me())
      await context.queryClient.ensureQueryData(q.sharePreview(params.token))
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        throw redirect({ to: '/login', search: { return_to: location.href } })
      }
      // Other failures (expired, used) are shown on the page.
    }
  },
  head: () => ({
    meta: [
      { title: 'Shared group · easy-sys-admin' },
      { name: 'robots', content: 'noindex' },
    ],
  }),
  component: SharePage,
})

const accessText = {
  view: 'Your team sees its servers, metrics, runs, and alerts.',
  operate:
    'Your team sees its servers. Your admins and owners can also start runs, act on services and containers, and read logs.',
} as const

function linkError(e: unknown): string {
  if (e instanceof ApiError && e.status === 404) {
    return 'This share link is not valid. It may have been used, revoked, or it expired. Ask for a new one.'
  }
  return e instanceof Error ? e.message : 'The share link cannot be read.'
}

function SharePage() {
  const { token } = Route.useParams()
  const navigate = useNavigate()
  const reset = useSessionReset()
  const { data: me } = useSuspenseQuery(q.me())
  const preview = useQuery(q.sharePreview(token))
  const p = preview.data
  const [picked, setPicked] = useState<string | null>(null)
  // One team to pick from: pick it.
  const teamId =
    picked ?? (p?.teams.length === 1 ? String(p.teams[0]?.id) : null)

  const accept = useMutation({
    mutationFn: async (id: number) => {
      const share = await call(
        api().POST('/shares/{token}/accept', {
          params: { path: { token } },
          body: { teamId: id },
        }),
      )
      await call(api().PUT('/me/account', { body: { accountId: id } }))
      return share
    },
    onSuccess: (share) =>
      reset(() =>
        navigate({
          to: '/app/groups/$groupId',
          params: { groupId: share.groupId },
        }),
      ),
  })

  return (
    <LinkShell kicker="Shared group" title={p ? p.groupName : 'A shared group'}>
      {preview.isLoading ? (
        <Loading label="Reading the link" />
      ) : !p ? (
        <>
          <ErrorBanner error={linkError(preview.error)} />
          <Button asChild variant="outline" className="w-full">
            <Link to="/app">Go to the board</Link>
          </Button>
        </>
      ) : (
        <>
          <p className="text-sm text-ink-2">
            {p.ownerTeam} shares this group and its servers with one of your
            teams.
          </p>
          <dl className="space-y-2">
            <KeyValue k="Owner" v={p.ownerTeam} />
            <KeyValue k="Access" v={p.access} />
            <KeyValue k="Servers" v={plural(p.serverCount, 'server')} />
            <KeyValue k="Expires" v={<Until iso={p.expiresAt} />} />
          </dl>
          <p className="text-xs text-muted-foreground">
            {accessText[p.access]} {p.ownerTeam} keeps the settings, schedules,
            and SSH key.
          </p>
          {p.teams.length === 0 ? (
            <>
              <Notice tone="warn">
                To accept it, you need to be an owner or admin of a team other
                than {p.ownerTeam}. A personal space cannot receive shared
                groups. Create a team, then open this link again.
              </Notice>
              <Button asChild variant="outline" className="w-full">
                <Link to="/app/team">
                  <PlusIcon /> Create a team
                </Link>
              </Button>
            </>
          ) : (
            <form
              className="space-y-4"
              onSubmit={(e) => {
                e.preventDefault()
                if (teamId) accept.mutate(Number(teamId))
              }}
            >
              <Field label="Add it to">
                <Choice
                  placeholder="Pick a team"
                  value={teamId ?? ''}
                  onChange={setPicked}
                  options={p.teams.map((t) => ({
                    value: String(t.id),
                    label: `${t.name} · ${t.role}`,
                  }))}
                />
              </Field>
              <ErrorBanner error={accept.error} />
              <Button
                type="submit"
                size="lg"
                className="w-full"
                disabled={!teamId}
                busy={accept.isPending}
              >
                <LayersIcon /> Add the group
              </Button>
            </form>
          )}
          <p className="font-mono text-[11px] text-muted-foreground">
            Signed in as {me.user.email}.
          </p>
        </>
      )}
    </LinkShell>
  )
}
