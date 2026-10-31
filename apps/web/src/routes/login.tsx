import { Link, createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { z } from 'zod'
import { q } from '#/lib/queries'
import { Field, cx } from '#/components/ui'

const errors: Record<string, string> = {
  state_mismatch: 'The sign-in took too long or was opened in another tab. Try again.',
  denied: 'The identity provider did not let you in.',
  not_allowed: 'This email is not on the list of people allowed to use this install.',
  provider_unavailable: 'The identity provider cannot be reached right now.',
  bad_password: 'Wrong email or password.',
  locked: 'Too many failed attempts for this email. Wait 15 minutes and try again.',
  email_taken: 'An account with this email already exists. Sign in instead.',
  signup_closed: 'Sign-up is closed on this install. Ask the owner for an invite.',
  invalid_email: 'Enter a valid email address.',
  invalid_password: 'Use a password of 10 to 200 characters that is not your email.',
  invalid_name: 'Use a name of at most 100 characters.',
  failed: 'Sign-in failed. Try again, and check the control plane log if it keeps happening.',
}

export const Route = createFileRoute('/login')({
  validateSearch: z.object({
    error: z.string().optional(),
    signed_out: z.coerce.string().optional(),
    return_to: z.string().optional(),
    mode: z.enum(['signin', 'signup']).optional(),
  }),
  loader: ({ context }) => context.queryClient.ensureQueryData(q.authConfig()),
  head: () => ({ meta: [{ title: 'Sign in · easy-sys-admin' }] }),
  component: Login,
})

function ProviderIcon({ id }: { id: string }) {
  if (id === 'google') {
    return (
      <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
        <path fill="#EA4335" d="M12 10.2v3.9h5.5c-.24 1.4-1.7 4.1-5.5 4.1-3.3 0-6-2.7-6-6.1s2.7-6.1 6-6.1c1.9 0 3.1.8 3.8 1.5l2.6-2.5C16.8 3.4 14.6 2.4 12 2.4 6.8 2.4 2.6 6.6 2.6 11.8s4.2 9.4 9.4 9.4c5.4 0 9-3.8 9-9.2 0-.6-.1-1.1-.2-1.6H12z" />
      </svg>
    )
  }
  return <span className="size-4 rounded-sm border border-current" aria-hidden />
}

function Login() {
  const { error, signed_out, return_to, mode = 'signin' } = Route.useSearch()
  const { data: cfg } = useSuspenseQuery(q.authConfig())
  const returnTo = return_to?.startsWith('/') && !return_to.startsWith('//') ? return_to : '/app'
  const signup = mode === 'signup' && cfg.signup

  return (
    <div className="grid min-h-screen place-items-center px-6 py-12">
      <div className="w-full max-w-sm animate-rise">
        <a href="/" className="mb-10 flex items-center gap-2.5">
          <img src="/favicon.svg" alt="" className="size-7" />
          <span className="font-display text-2xl font-bold uppercase tracking-wide">easy-sys-admin</span>
        </a>
        <div className="panel p-6">
          <h1 className="font-display text-4xl font-bold uppercase">{signup ? 'Create account' : 'Sign in'}</h1>
          <p className="mt-2 text-sm text-ink-2">{signup ? 'You get your own fleet. Add servers once you are in.' : 'The board, groups, and runs for your fleet.'}</p>
          {error && <p className="mt-4 rounded border border-fail/40 bg-fail/10 px-3 py-2 text-sm text-fail">{errors[error] ?? errors.failed}</p>}
          {signed_out && !error && <p className="mt-4 rounded border border-line-2 px-3 py-2 text-sm text-ink-2">You are signed out.</p>}

          {cfg.password && (
            <form method="post" action={signup ? '/auth/password/signup' : '/auth/password/login'} className="mt-6 space-y-3">
              <input type="hidden" name="return_to" value={returnTo} />
              {signup && (
                <Field label="Name">
                  <input className="input" name="name" autoComplete="name" />
                </Field>
              )}
              <Field label="Email">
                <input className="input" type="email" name="email" autoComplete="email" required />
              </Field>
              <Field label="Password" hint={signup ? 'At least 10 characters.' : undefined}>
                <input className="input" type="password" name="password" autoComplete={signup ? 'new-password' : 'current-password'} minLength={signup ? 10 : undefined} required />
              </Field>
              <button className="flex w-full items-center justify-center rounded bg-amber px-4 py-3 font-semibold text-bg hover:bg-amber-2">
                {signup ? 'Create account' : 'Sign in'}
              </button>
            </form>
          )}

          {cfg.providers.length > 0 && (
            <>
              {cfg.password && (
                <div className="my-5 flex items-center gap-3 font-mono text-[10px] uppercase tracking-widest text-faint">
                  <span className="h-px flex-1 bg-line" /> or <span className="h-px flex-1 bg-line" />
                </div>
              )}
              <div className={cx('space-y-2', !cfg.password && 'mt-6')}>
                {cfg.providers.map((p) => (
                  <a
                    key={p.id}
                    href={`/auth/login/${p.id}?return_to=${encodeURIComponent(returnTo)}`}
                    className="flex w-full items-center justify-center gap-2.5 rounded border border-line-2 px-4 py-2.5 text-sm text-ink hover:border-ink-2 hover:bg-panel-2"
                  >
                    <ProviderIcon id={p.id} />
                    {signup ? 'Sign up' : 'Continue'} with {p.name}
                  </a>
                ))}
              </div>
            </>
          )}

          {cfg.dev && (
            <form method="post" action="/auth/dev-login" className="mt-5 border-t border-line pt-5">
              <input type="hidden" name="return_to" value={returnTo} />
              <button className="w-full rounded border border-dashed border-amber/60 px-4 py-2.5 font-mono text-xs uppercase tracking-wider text-amber hover:bg-amber/10">
                Continue as the local dev user
              </button>
            </form>
          )}

          {cfg.signup && (
            <p className="mt-5 text-center text-sm text-muted">
              {signup ? 'Have an account? ' : 'New here? '}
              <Link to="/login" search={{ mode: signup ? 'signin' : 'signup', return_to }} className="text-amber underline underline-offset-4">
                {signup ? 'Sign in' : 'Create an account'}
              </Link>
            </p>
          )}
        </div>
      </div>
    </div>
  )
}
