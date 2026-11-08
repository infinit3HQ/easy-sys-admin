import { Link, createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { z } from 'zod'
import { q } from '#/lib/queries'
import { ErrorBanner, Field, Notice, cn } from '#/components/kit'
import { Button } from '#/components/ui/button'
import { Card, CardContent } from '#/components/ui/card'
import { Input } from '#/components/ui/input'

const errors: Record<string, string> = {
  state_mismatch:
    'The sign-in took too long or was opened in another tab. Try again.',
  denied: 'The identity provider did not let you in.',
  not_allowed:
    'This email is not on the list of people allowed to use this install.',
  provider_unavailable: 'The identity provider cannot be reached right now.',
  bad_password: 'Wrong email or password.',
  locked:
    'Too many failed attempts for this email. Wait 15 minutes and try again.',
  rate_limited:
    'Too many attempts from your network. Wait 15 minutes and try again.',
  busy: 'The server is busy. Try again in a moment.',
  email_taken: 'An account with this email already exists. Sign in instead.',
  use_provider:
    'This email already signs in with Google or another provider. Use that button. It is the same account.',
  email_unconfirmed:
    'Open the confirmation link we emailed you, then sign in. Google accounts skip this.',
  mail_failed: 'The email could not be sent. Try again in a minute.',
  confirm_invalid: 'This confirmation link is invalid or has expired.',
  reset_invalid:
    'This reset link is invalid or has expired. Ask for a new one.',
  signup_closed:
    'Sign-up is closed on this install. Ask the owner for an invite.',
  invalid_email: 'Enter a valid email address.',
  invalid_password:
    'Use a password of 10 to 200 characters that is not your email.',
  invalid_name: 'Use a name of at most 100 characters.',
  failed:
    'Sign-in failed. Try again, and check the control plane log if it keeps happening.',
}

export const Route = createFileRoute('/login')({
  validateSearch: z.object({
    error: z.string().optional(),
    signed_out: z.coerce.string().optional(),
    return_to: z.string().optional(),
    mode: z
      .enum(['signin', 'signup', 'forgot', 'reset', 'confirm', 'verify'])
      .optional(),
    email: z.string().max(320).optional(),
    token: z.string().max(200).optional(),
    sent: z.enum(['confirm', 'reset']).optional(),
    confirmed: z.coerce.string().optional(),
    reset: z.coerce.string().optional(),
  }),
  loader: ({ context }) => context.queryClient.ensureQueryData(q.authConfig()),
  head: () => ({
    meta: [
      { title: 'Sign in · easy-sys-admin' },
      { name: 'robots', content: 'noindex, nofollow' },
    ],
  }),
  component: Login,
})

function ProviderIcon({ id }: { id: string }) {
  if (id === 'google') {
    return (
      <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
        <path
          fill="#EA4335"
          d="M12 10.2v3.9h5.5c-.24 1.4-1.7 4.1-5.5 4.1-3.3 0-6-2.7-6-6.1s2.7-6.1 6-6.1c1.9 0 3.1.8 3.8 1.5l2.6-2.5C16.8 3.4 14.6 2.4 12 2.4 6.8 2.4 2.6 6.6 2.6 11.8s4.2 9.4 9.4 9.4c5.4 0 9-3.8 9-9.2 0-.6-.1-1.1-.2-1.6H12z"
        />
      </svg>
    )
  }
  return (
    <span className="size-4 rounded-sm border border-current" aria-hidden />
  )
}

function Login() {
  const {
    error,
    signed_out,
    return_to,
    mode = 'signin',
    email,
    token,
    sent,
    confirmed,
    reset,
  } = Route.useSearch()
  const { data: cfg } = useSuspenseQuery(q.authConfig())
  const returnTo =
    return_to?.startsWith('/') && !return_to.startsWith('//')
      ? return_to
      : '/app'
  const signup = mode === 'signup' && cfg.signup
  const forgot = mode === 'forgot' && cfg.mail
  const resetting = mode === 'reset' && cfg.mail && !!token
  const confirming = mode === 'confirm' && cfg.mail && !!token
  const verify = mode === 'verify' && cfg.mail

  return (
    <div className="grid min-h-dvh place-items-center px-4 py-10 sm:px-6">
      <div className="w-full max-w-sm animate-rise">
        <a href="/" className="mb-8 flex items-center gap-2.5">
          <img src="/favicon.svg" alt="" className="size-7" />
          <span className="font-display text-2xl font-bold tracking-wide uppercase">
            Easy Sys Admin
          </span>
        </a>
        <Card>
          <CardContent className="p-5 sm:p-6">
            <h1 className="font-display text-4xl font-bold uppercase">
              {confirming
                ? 'Confirm email'
                : verify
                  ? 'Check your email'
                  : resetting
                    ? 'New password'
                    : forgot
                      ? 'Reset password'
                      : signup
                        ? 'Create account'
                        : 'Sign in'}
            </h1>
            <p className="mt-2 text-sm text-ink-2">
              {confirming
                ? 'This confirms the address on your account.'
                : verify
                  ? sent === 'confirm'
                    ? 'We sent a confirmation link. Open it, then sign in. It works for 24 hours.'
                    : 'This address is not confirmed yet. We can send the link again.'
                  : resetting
                    ? 'Choose a password of at least 10 characters.'
                    : forgot
                      ? 'If this address has a password account, we will email a link.'
                      : signup
                        ? cfg.mail
                          ? 'We email a confirmation link. Open it, then sign in. Google skips this.'
                          : 'You get your own fleet. Add servers once you are in.'
                        : 'The board, groups, and runs for your fleet.'}
            </p>
            {error && (
              <ErrorBanner
                className="mt-4"
                error={errors[error] ?? errors.failed}
              />
            )}
            {signed_out && !error && (
              <Notice className="mt-4">You are signed out.</Notice>
            )}
            {sent === 'confirm' && !error && (
              <Notice className="mt-4">
                Check your email for a confirmation link. It works for 24 hours.
              </Notice>
            )}
            {sent === 'reset' && !error && (
              <Notice className="mt-4">
                If that address has a password account, a reset link is on its
                way. It works for 1 hour.
              </Notice>
            )}
            {confirmed && !error && (
              <Notice className="mt-4">Email confirmed. Sign in.</Notice>
            )}
            {reset && !error && (
              <Notice className="mt-4">
                Password updated. Sign in with the new one.
              </Notice>
            )}

            {confirming && (
              <form method="post" action="/auth/email/confirm" className="mt-6">
                <input type="hidden" name="token" value={token} />
                <Button type="submit" className="w-full">
                  Confirm email
                </Button>
              </form>
            )}

            {resetting && (
              <form
                method="post"
                action="/auth/password/reset"
                className="mt-6 space-y-4"
              >
                <input type="hidden" name="token" value={token} />
                <Field label="New password" hint="At least 10 characters.">
                  <Input
                    type="password"
                    name="password"
                    autoComplete="new-password"
                    minLength={10}
                    required
                  />
                </Field>
                <Button type="submit" className="w-full">
                  Save password
                </Button>
              </form>
            )}

            {forgot && (
              <form
                method="post"
                action="/auth/password/forgot"
                className="mt-6 space-y-4"
              >
                <Field label="Email">
                  <Input
                    type="email"
                    name="email"
                    autoComplete="email"
                    autoCapitalize="off"
                    required
                  />
                </Field>
                <Button type="submit" className="w-full">
                  Email reset link
                </Button>
              </form>
            )}

            {verify && (
              <form
                method="post"
                action="/auth/email/resend"
                className="mt-6 space-y-4"
              >
                <Field label="Email">
                  <Input
                    type="email"
                    name="email"
                    autoComplete="email"
                    autoCapitalize="off"
                    required
                    defaultValue={email}
                  />
                </Field>
                <Button type="submit" className="w-full">
                  Send the link again
                </Button>
              </form>
            )}

            {cfg.password &&
              !forgot &&
              !resetting &&
              !confirming &&
              !verify && (
                <form
                  method="post"
                  action={
                    signup ? '/auth/password/signup' : '/auth/password/login'
                  }
                  className="mt-6 space-y-4"
                >
                  <input type="hidden" name="return_to" value={returnTo} />
                  {signup && (
                    <Field label="Name">
                      <Input name="name" autoComplete="name" />
                    </Field>
                  )}
                  <Field label="Email">
                    <Input
                      type="email"
                      name="email"
                      autoComplete="email"
                      autoCapitalize="off"
                      required
                    />
                  </Field>
                  <Field
                    label="Password"
                    hint={signup ? 'At least 10 characters.' : undefined}
                  >
                    <Input
                      type="password"
                      name="password"
                      autoComplete={
                        signup ? 'new-password' : 'current-password'
                      }
                      minLength={signup ? 10 : undefined}
                      required
                    />
                  </Field>
                  <Button type="submit" size="lg" className="w-full">
                    {signup ? 'Create account' : 'Sign in'}
                  </Button>
                </form>
              )}

            {cfg.mail &&
              cfg.password &&
              !signup &&
              !forgot &&
              !resetting &&
              !confirming &&
              !verify && (
                <p className="mt-4 text-center text-sm">
                  <Link
                    to="/login"
                    search={{ mode: 'forgot', return_to }}
                    className="text-muted-foreground underline underline-offset-4"
                  >
                    Forgot password?
                  </Link>
                </p>
              )}

            {cfg.providers.length > 0 &&
              !forgot &&
              !resetting &&
              !confirming &&
              !verify && (
                <>
                  {cfg.password && (
                    <div className="my-5 flex items-center gap-3 font-mono text-[10px] tracking-widest text-faint uppercase">
                      <span className="h-px flex-1 bg-line" /> or{' '}
                      <span className="h-px flex-1 bg-line" />
                    </div>
                  )}
                  <div className={cn('space-y-2', !cfg.password && 'mt-6')}>
                    {cfg.providers.map((p) => (
                      <Button
                        key={p.id}
                        asChild
                        variant="outline"
                        size="lg"
                        className="w-full"
                      >
                        <a
                          href={`/auth/login/${p.id}?return_to=${encodeURIComponent(returnTo)}`}
                        >
                          <ProviderIcon id={p.id} />
                          {signup ? 'Sign up' : 'Continue'} with {p.name}
                        </a>
                      </Button>
                    ))}
                  </div>
                </>
              )}

            {cfg.dev && (
              <form
                method="post"
                action="/auth/dev-login"
                className="mt-5 border-t pt-5"
              >
                <input type="hidden" name="return_to" value={returnTo} />
                <Button
                  type="submit"
                  variant="ghost"
                  className="w-full border border-dashed border-amber/60 font-mono text-xs tracking-wider text-amber uppercase hover:bg-amber/10 hover:text-amber"
                >
                  Continue as the local dev user
                </Button>
              </form>
            )}

            {verify && (
              <p className="mt-5 text-center text-sm text-muted-foreground">
                <Link
                  to="/login"
                  search={{ return_to }}
                  className="text-amber underline underline-offset-4"
                >
                  Back to sign in
                </Link>
              </p>
            )}

            {!forgot && !resetting && !confirming && !verify && (
              <p className="mt-5 text-center text-xs leading-relaxed text-muted-foreground">
                By continuing you agree to the{' '}
                <Link
                  to="/terms"
                  className="text-ink underline underline-offset-4"
                >
                  Terms
                </Link>{' '}
                and{' '}
                <Link
                  to="/privacy"
                  className="text-ink underline underline-offset-4"
                >
                  Privacy Policy
                </Link>
                .
              </p>
            )}

            {cfg.signup && !verify && (
              <p className="mt-5 text-center text-sm text-muted-foreground">
                {signup ? 'Have an account? ' : 'New here? '}
                <Link
                  to="/login"
                  search={{ mode: signup ? 'signin' : 'signup', return_to }}
                  className="text-amber underline underline-offset-4"
                >
                  {signup ? 'Sign in' : 'Create an account'}
                </Link>
              </p>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
