import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { z } from 'zod'
import { q } from '#/lib/queries'

const errors: Record<string, string> = {
  state_mismatch: 'The sign-in took too long or was opened in another tab. Try again.',
  denied: 'The identity provider did not let you in.',
  not_allowed: 'Your account is not on the list of people allowed to use this install.',
  provider_unavailable: 'The identity provider cannot be reached right now.',
  failed: 'Sign-in failed. Try again, and check the control plane log if it keeps happening.',
}

export const Route = createFileRoute('/login')({
  validateSearch: z.object({
    error: z.string().optional(),
    signed_out: z.coerce.string().optional(),
    return_to: z.string().optional(),
  }),
  loader: ({ context }) => context.queryClient.ensureQueryData(q.authConfig()),
  head: () => ({ meta: [{ title: 'Sign in · easy-sys-admin' }] }),
  component: Login,
})

function Login() {
  const { error, signed_out, return_to } = Route.useSearch()
  const { data: cfg } = useSuspenseQuery(q.authConfig())
  const returnTo = return_to?.startsWith('/') && !return_to.startsWith('//') ? return_to : '/app'

  return (
    <div className="grid min-h-screen place-items-center px-6">
      <div className="w-full max-w-sm animate-rise">
        <a href="/" className="mb-10 flex items-center gap-2.5">
          <img src="/favicon.svg" alt="" className="size-7" />
          <span className="font-display text-2xl font-bold uppercase tracking-wide">easy-sys-admin</span>
        </a>
        <div className="panel p-6">
          <h1 className="font-display text-4xl font-bold uppercase">Sign in</h1>
          <p className="mt-2 text-sm text-ink-2">The board, groups, and runs for your fleet.</p>
          {error && <p className="mt-4 rounded border border-fail/40 bg-fail/10 px-3 py-2 text-sm text-fail">{errors[error] ?? errors.failed}</p>}
          {signed_out && !error && <p className="mt-4 rounded border border-line-2 px-3 py-2 text-sm text-ink-2">You are signed out.</p>}
          {cfg.mode === 'oidc' ? (
            <a
              href={`/auth/login?return_to=${encodeURIComponent(returnTo)}`}
              className="mt-6 flex w-full items-center justify-center rounded bg-amber px-4 py-3 font-semibold text-bg hover:bg-amber-2"
            >
              Continue with {cfg.providerName}
            </a>
          ) : (
            <form method="post" action="/auth/dev-login" className="mt-6">
              <input type="hidden" name="return_to" value={returnTo} />
              <button className="flex w-full items-center justify-center rounded bg-amber px-4 py-3 font-semibold text-bg hover:bg-amber-2">
                Continue as the local dev user
              </button>
              <p className="mt-3 font-mono text-[11px] text-muted">Dev mode: only allowed when esa listens on localhost.</p>
            </form>
          )}
        </div>
      </div>
    </div>
  )
}
