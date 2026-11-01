import {
  Link,
  createFileRoute,
  redirect,
  useNavigate,
} from '@tanstack/react-router'
import { useMutation, useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { UsersIcon } from 'lucide-react'
import { ApiError, api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Until } from '#/components/time'
import { ErrorBanner, KeyValue, Loading, Notice } from '#/components/kit'
import { LinkShell, roleText, useSessionReset } from '#/components/org'
import { Button } from '#/components/ui/button'

export const Route = createFileRoute('/invite/$token')({
  // Accepting needs a browser session: send visitors who are not signed in
  // to the login page and bring them back here afterwards.
  beforeLoad: async ({ context, location, params }) => {
    try {
      await context.queryClient.ensureQueryData(q.me())
      await context.queryClient.ensureQueryData(q.invite(params.token))
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        throw redirect({ to: '/login', search: { return_to: location.href } })
      }
      // Other failures (expired, used, wrong email) are shown on the page.
    }
  },
  head: () => ({
    meta: [
      { title: 'Invite · easy-sys-admin' },
      { name: 'robots', content: 'noindex' },
    ],
  }),
  component: InvitePage,
})

function linkError(e: unknown): string {
  if (e instanceof ApiError && e.status === 404) {
    return 'This invite link is not valid. It may have been used, revoked, or it expired. Ask for a new one.'
  }
  return e instanceof Error ? e.message : 'The invite cannot be read.'
}

function InvitePage() {
  const { token } = Route.useParams()
  const navigate = useNavigate()
  const reset = useSessionReset()
  const { data: me } = useSuspenseQuery(q.me())
  const invite = useQuery(q.invite(token))
  const accept = useMutation({
    mutationFn: async () => {
      const m = await call(
        api().POST('/invites/{token}/accept', { params: { path: { token } } }),
      )
      // Accepting switches the session already; this also covers a member
      // who opens an invite to an organization they are in.
      await call(api().PUT('/me/account', { body: { accountId: m.id } }))
      return m
    },
    onSuccess: () => reset(() => navigate({ to: '/app/org' })),
  })

  const p = invite.data
  const signedIn = (
    <p className="font-mono text-[11px] text-muted-foreground">
      Signed in as {me.user.email}.
    </p>
  )

  return (
    <LinkShell
      kicker="Invite"
      title={p ? `Join ${p.teamName}` : 'Join an organization'}
    >
      {invite.isLoading ? (
        <Loading label="Reading the invite" />
      ) : !p ? (
        <>
          <ErrorBanner error={linkError(invite.error)} />
          {signedIn}
          <Button asChild variant="outline" className="w-full">
            <Link to="/app">Go to the board</Link>
          </Button>
        </>
      ) : (
        <>
          <p className="text-sm text-ink-2">
            {p.invitedBy} invited you to the organization {p.teamName}.
          </p>
          <dl className="space-y-2">
            <KeyValue k="Role" v={p.role} />
            <KeyValue k="Expires" v={<Until iso={p.expiresAt} />} />
          </dl>
          <p className="text-xs text-muted-foreground">{roleText[p.role]}</p>
          {p.alreadyMember && (
            <Notice>
              You are already a member of {p.teamName}. The invite stays unused.
            </Notice>
          )}
          <ErrorBanner error={accept.error} />
          <Button
            size="lg"
            className="w-full"
            busy={accept.isPending}
            onClick={() => accept.mutate()}
          >
            <UsersIcon />{' '}
            {p.alreadyMember ? 'Open the organization' : 'Accept and join'}
          </Button>
          {signedIn}
        </>
      )}
    </LinkShell>
  )
}
