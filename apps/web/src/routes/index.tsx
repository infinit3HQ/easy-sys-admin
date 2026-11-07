import { createServerFn } from '@tanstack/react-start'
import { Link, createFileRoute } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { ArrowRightIcon } from 'lucide-react'
import { CopyBlock, StatusDot, cn } from '#/components/kit'
import {
  column,
  productName,
  showSelfHost,
  SiteFrame,
} from '#/components/site-frame'
import { siteOrigin } from '#/lib/origin'
import { Button } from '#/components/ui/button'
import type { ServerStatus } from '@esa/api'

const loadPublicPolicy = createServerFn({ method: 'GET' }).handler(
  async () => {
    // Same variables as the control plane. Unset means a self-hosted install:
    // full caps, and sign-up closed except for the first account.
    const betaLimits = process.env.ESA_BETA_LIMITS === 'true'
    let signup = ['1', 'true', 'yes', 'on'].includes(
      (process.env.ESA_SIGNUP ?? '').toLowerCase(),
    )
    try {
      const origin = process.env.ESA_API_URL ?? 'http://127.0.0.1:8080'
      const res = await fetch(new URL('/api/v1/auth/config', origin), {
        signal: AbortSignal.timeout(2000),
      })
      if (res.ok) {
        const body = (await res.json()) as { signup?: boolean }
        if (typeof body.signup === 'boolean') signup = body.signup
      }
    } catch {
      // The control plane is the source of truth when it answers. An allow
      // list there turns ESA_SIGNUP off, which this env value cannot see.
    }
    return { betaLimits, signup }
  },
)

const purpose =
  'Easy Sys Admin is a web app for people who administer their own Linux servers. It shows every server on one board, installs package updates, and cleans disks on a schedule. You can read this page without signing in.'

export const Route = createFileRoute('/')({
  ssr: true,
  loader: () => loadPublicPolicy(),
  head: () => {
    const origin = siteOrigin()
    return {
      meta: [
        { title: `${productName}: one board for your Linux servers` },
        { name: 'description', content: purpose },
        { name: 'robots', content: 'index, follow' },
        { property: 'og:title', content: productName },
        { property: 'og:description', content: purpose },
        { property: 'og:type', content: 'website' },
        { property: 'og:url', content: origin ? `${origin}/` : '/' },
      ],
      links: [{ rel: 'canonical', href: origin ? `${origin}/` : '/' }],
    }
  },
  component: Landing,
})

const demo: Array<{
  name: string
  group: string
  status: ServerStatus
  cpu: number
  disk: number
  note: string
}> = [
  {
    name: 'media-01',
    group: 'production',
    status: 'ok',
    cpu: 12,
    disk: 41,
    note: 'freed 47.2 GiB build cache',
  },
  {
    name: 'builder-02',
    group: 'builders',
    status: 'warning',
    cpu: 64,
    disk: 88,
    note: 'disk / at 88%',
  },
  {
    name: 'edge-01',
    group: 'production',
    status: 'ok',
    cpu: 4,
    disk: 22,
    note: '3 security updates installed',
  },
  {
    name: 'client-a',
    group: 'client A',
    status: 'failing',
    cpu: 9,
    disk: 97,
    note: 'disk /var at 97%',
  },
  {
    name: 'lab-pi',
    group: 'home',
    status: 'unreachable',
    cpu: 0,
    disk: 0,
    note: 'no answer for 4m',
  },
  {
    name: 'db-01',
    group: 'production',
    status: 'ok',
    cpu: 23,
    disk: 57,
    note: 'reboot scheduled sun 04:00',
  },
]

function Landing() {
  return (
    <SiteFrame>
      <Hero />
      <Beta />
      <Watches />
      <Touch />
      <Agents />
      <Install />
    </SiteFrame>
  )
}

function Hero() {
  return (
    <section className="relative pt-12 pb-16 sm:pt-16 sm:pb-20">
      <div className={column}>
        <div className="grid items-end gap-10 lg:grid-cols-[minmax(0,1.05fr)_minmax(20rem,0.95fr)] lg:gap-12">
          <div className="animate-rise">
            <p className="label mb-6 !text-amber">Easy Sys Admin · public beta</p>
            <h1 className="font-display text-[clamp(3.25rem,14vw,8.5rem)] leading-[0.82] font-extrabold tracking-tight uppercase lg:text-[clamp(3.5rem,9vw,8.5rem)]">
              One board
              <br />
              for the
              <br />
              <span className="text-amber">whole fleet.</span>
            </h1>
          </div>
          <Board />
        </div>
        <div className="mt-10 sm:mt-12">
          <p className="text-base leading-relaxed text-ink-2 sm:text-lg">
            {purpose} See every server on one page, refreshed each minute. Put
            servers in groups, choose when their disks get cleaned and their
            packages patched, and stop logging in to each machine. The board
            reaches each server over SSH. Nothing from Easy Sys Admin listens
            on those servers.
          </p>
          <div className="mt-8 flex flex-wrap items-center justify-end gap-3">
            <Link
              to="/privacy"
              className="mr-auto font-mono text-xs text-ink-2 hover:text-ink"
            >
              Privacy policy
            </Link>
            {showSelfHost && (
              <Button asChild size="lg" variant="outline">
                <a href="#install">Self-host it</a>
              </Button>
            )}
            <Button asChild size="lg">
              <Link to="/app">
                Open the board <ArrowRightIcon />
              </Link>
            </Button>
          </div>
        </div>
      </div>
    </section>
  )
}

/** The hero's departure board: demo rows that flip in one after another. */
function Board() {
  const [tick, setTick] = useState(0)
  useEffect(() => {
    const t = setInterval(() => setTick((x) => x + 1), 2200)
    return () => clearInterval(t)
  }, [])
  return (
    <div
      className="panel animate-rise overflow-hidden shadow-[0_40px_120px_-40px_rgb(255_176_0/0.25)]"
      style={{ animationDelay: '120ms' }}
    >
      <div className="flex items-center justify-between border-b px-4 py-2.5">
        <span className="label">Fleet · demo data</span>
        <span className="flex items-center gap-2 font-mono text-[10px] text-muted-foreground">
          <span className="size-1.5 animate-blink rounded-full bg-ok" /> live
        </span>
      </div>
      <div className="divide-y divide-line">
        {demo.map((s, i) => {
          const wobble =
            s.status === 'unreachable'
              ? 0
              : Math.round(Math.sin((tick + i) * 1.7) * 4)
          return (
            <div
              key={s.name}
              className="grid animate-rise grid-cols-[auto_1fr_auto] items-center gap-3 px-4 py-3 sm:gap-4"
              style={{ animationDelay: `${200 + i * 90}ms` }}
            >
              <StatusDot status={s.status} />
              <div className="min-w-0">
                <div className="flex items-baseline gap-2">
                  <span className="font-mono text-sm text-ink">{s.name}</span>
                  <span className="hidden font-mono text-[10px] tracking-wider text-faint uppercase sm:inline">
                    {s.group}
                  </span>
                </div>
                <div className="truncate font-mono text-[11px] text-muted-foreground">
                  {s.note}
                </div>
              </div>
              <div className="w-24 space-y-1.5 sm:w-28">
                <MiniBar label="cpu" value={Math.max(0, s.cpu + wobble)} />
                <MiniBar label="disk" value={s.disk} />
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function MiniBar({ label, value }: { label: string; value: number }) {
  const c = value >= 95 ? 'bg-fail' : value >= 85 ? 'bg-warn' : 'bg-ink-2'
  return (
    <div className="flex items-center gap-2">
      <span className="w-7 font-mono text-[9px] uppercase text-faint">
        {label}
      </span>
      <div className="h-1 flex-1 bg-line">
        <div
          className={cn('h-full transition-[width] duration-1000', c)}
          style={{ width: `${value}%` }}
        />
      </div>
    </div>
  )
}

const watches = [
  [
    'Status',
    'CPU, memory, load, every real disk and its inodes, uptime, network. Polled each minute, kept 90 days.',
  ],
  [
    'Updates',
    'Pending packages split into security and the rest. Upgrades run as their own systemd job, so a dropped SSH session cannot kill apt halfway.',
  ],
  [
    'Cleanups',
    'Idle Docker build cache and stale VS Code server copies, on the group schedule. A server running a build is skipped.',
  ],
  [
    'Services',
    'Every systemd unit, failed ones first. Start, stop, restart. sshd and Docker cannot be stopped from here.',
  ],
  [
    'Containers',
    'State, health, restarts, and whether the registry has a newer image. Pull and recreate one compose service at a time.',
  ],
  [
    'Logs',
    'Journal per unit and container output, filtered by priority and text.',
  ],
  [
    'Access',
    'Who has a login shell, which SSH keys they carry, who is in sudo and docker, recent logins, failed attempts.',
  ],
  [
    'Security',
    'Listening ports and their processes, firewall, CrowdSec and fail2ban, unattended-upgrades.',
  ],
  [
    'Alerts',
    'Disk, memory, unreachable, failed units, unhealthy containers, expiring certificates, failed runs. To ntfy, Discord, Slack, a webhook, or email.',
  ],
]

/** "01", set in the heading's own face and size so it sits on the same baseline. */
function StepNumber({ n }: { n: number }) {
  return (
    <span
      aria-hidden
      className="font-display text-2xl font-semibold text-amber/70 tabular-nums"
    >
      {String(n).padStart(2, '0')}
    </span>
  )
}

function facts(policy: {
  betaLimits: boolean
  signup: boolean
}): Array<[string, string]> {
  return [
    [
      'Price',
      'The public beta is free. There is no charge to sign in or to add a server.',
    ],
    [
      'Limit',
      policy.betaLimits
        ? 'You get one personal space and one organization. Each can hold 10 servers, 20 in total.'
        : 'One account can hold 500 servers. One person can own 20 organizations besides their personal space.',
    ],
    [
      'Sign-up',
      policy.signup
        ? 'Sign-up is open. Create an account with Google, or with an email and a password.'
        : 'Sign-up is closed. The first account can be created while no one has signed up yet.',
    ],
    [
      'Where data sits',
      'Account details, server hostnames, metrics, and encrypted logins stay in the database for this website. They are not sold.',
    ],
  ]
}

function Beta() {
  const policy = Route.useLoaderData()
  return (
    <section id="beta" className="scroll-mt-14 border-t py-16 sm:py-20">
      <div className={column}>
        <h2 className="font-display text-5xl leading-[0.9] font-bold uppercase sm:text-6xl">
          This beta
        </h2>
        <dl className="mt-10 grid gap-px overflow-hidden rounded-md border bg-line sm:grid-cols-2">
          {facts(policy).map(([title, body]) => (
            <div key={title} className="bg-bg p-5 sm:p-6">
              <dt className="font-display text-2xl font-semibold tracking-wide uppercase">
                {title}
              </dt>
              <dd className="mt-3 text-sm leading-relaxed text-ink-2">
                {body}
              </dd>
            </div>
          ))}
        </dl>
      </div>
    </section>
  )
}

function Watches() {
  return (
    <section id="watch" className="scroll-mt-14 border-t py-16 sm:py-20">
      <div className={column}>
        <h2 className="font-display text-5xl leading-[0.9] font-bold uppercase sm:text-6xl">
          What it watches
        </h2>
        <div className="mt-10 grid gap-px overflow-hidden rounded-md border bg-line sm:mt-12 sm:grid-cols-2 lg:grid-cols-3">
          {watches.map(([title, body], i) => (
            <div key={title} className="bg-bg p-5 sm:p-6">
              <div className="flex items-baseline gap-3">
                <StepNumber n={i + 1} />
                <h3 className="font-display text-2xl font-semibold tracking-wide uppercase">
                  {title}
                </h3>
              </div>
              <p className="mt-3 text-sm leading-relaxed text-ink-2">{body}</p>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}

const rules = [
  'Never deletes Docker volumes.',
  'Never removes an image a container uses, never runs system prune.',
  'Never restarts Docker or the edge containers from a cleanup.',
  'Stops everything for a server whose host key changed.',
  'Never shows a password or private key again after you save it.',
  'Never reboots the server it runs on from a schedule.',
]

function Touch() {
  return (
    <section id="safety" className="scroll-mt-14 border-t py-16 sm:py-20">
      <div
        className={cn(
          column,
          'grid items-start gap-10 xl:grid-cols-[minmax(0,1fr)_minmax(0,max-content)] xl:gap-x-16',
        )}
      >
        <div>
          <h2 className="font-display text-5xl leading-[0.9] font-bold uppercase sm:text-6xl">
            One binary.
            <br />
            One sudo line.
          </h2>
          <p className="mt-6 max-w-lg leading-relaxed text-ink-2">
            Each server gets a small helper and a system user that may run only
            that helper. The control plane logs in with its own key over SSH,
            pins the host key it saw the first time, and refuses to talk to a
            machine that presents a different one. No agent listens on your
            servers.
          </p>
          <CopyBlock
            className="mt-8"
            value={'esa ALL=(root) NOPASSWD: /usr/local/lib/esa/esa-helper *'}
          />
        </div>
        <div className="panel h-fit w-full max-w-lg min-w-0 p-5 sm:p-6 xl:w-fit xl:justify-self-end">
          <div className="label mb-4">Rules with no switch</div>
          <ul className="space-y-2.5">
            {rules.map((r) => (
              <li key={r} className="flex gap-3 text-sm leading-snug text-ink">
                <span className="mt-1.5 size-1.5 shrink-0 bg-amber" />
                {r}
              </li>
            ))}
          </ul>
        </div>
      </div>
    </section>
  )
}

function Agents() {
  const snippet = `{
  "mcpServers": {
    "esa": {
      "type": "http",
      "url": "${siteOrigin()}/mcp",
      "headers": { "Authorization": "Bearer esa_..." }
    }
  }
}`
  return (
    <section id="agents" className="scroll-mt-14 border-t py-16 sm:py-20">
      <div
        className={cn(
          column,
          'flex flex-col gap-10 xl:flex-row xl:items-center xl:justify-between xl:gap-16',
        )}
      >
        <div className="max-w-xl min-w-0">
          <h2 className="font-display text-5xl leading-[0.9] font-bold uppercase sm:text-6xl">
            Agents read the fleet
          </h2>
          <p className="mt-6 leading-relaxed text-ink-2">
            The MCP endpoint gives an agent the same view you have: fleet
            summary, per-server status, updates, services, containers, security
            and access reports, run history, alerts. It uses an API token you
            create, so it can do what that token&apos;s scopes allow and nothing
            more. Every call lands in the activity log, marked MCP.
          </p>
          <p className="mt-4 font-mono text-xs leading-relaxed text-muted-foreground">
            &quot;Which servers are low on disk?&quot; · &quot;What failed last
            night?&quot; · &quot;Any containers with newer images?&quot;
          </p>
        </div>
        <CopyBlock value={snippet} />
      </div>
    </section>
  )
}

function Install() {
  const policy = Route.useLoaderData()
  const steps: Array<[string, string]> = showSelfHost
    ? [
        [
          'Start',
          'cp deploy/.env.example deploy/.env\n# set ESA_MASTER_KEY, the database password, OIDC\ndocker compose -f deploy/compose.yaml up -d',
        ],
        [
          'Add a server',
          '# in the board: Add server, check the host key,\n# then paste the one-time command on the server:\ncurl -fsSL https://esa.example.com/enroll/enr_… | sudo sh',
        ],
        [
          'Use the CLI',
          'esa login https://esa.example.com --token esa_…\nesa status\nesa runs start clean --group production',
        ],
      ]
    : [
        [
          'Sign in',
          policy.signup
            ? 'Create an account or sign in, then open the board.'
            : 'Sign in, then open the board.',
        ],
        [
          'Add a server',
          'In the board: Add server, check the host key,\nthen paste the one-time command it gives you.',
        ],
        [
          'Set a group',
          'Put the server in a group.\nChoose when it cleans disks and installs updates.',
        ],
      ]
  return (
    <section id="install" className="scroll-mt-14 border-t py-16 sm:py-20">
      {/* One step per row. The commands run to about 60 characters, so three
          narrow columns would hide half of each line. */}
      <div className={column}>
        <h2 className="font-display text-5xl leading-[0.9] font-bold uppercase sm:text-6xl">
          {showSelfHost ? 'Run it' : 'Start'}
        </h2>
        <ol className="mt-10 divide-y divide-line-2 border-y border-line-2">
          {steps.map(([t, code], i) => (
            <li
              key={t}
              className="grid min-w-0 items-center gap-3 py-6 md:grid-cols-[13rem_minmax(0,1fr)] md:gap-8"
            >
              <div className="flex items-baseline gap-3 md:pt-2">
                <StepNumber n={i + 1} />
                <h3 className="font-display text-2xl font-semibold tracking-wide uppercase">
                  {t}
                </h3>
              </div>
              {showSelfHost ? (
                <CopyBlock className="lg:w-full" value={code} />
              ) : (
                <p className="max-w-xl text-sm leading-relaxed whitespace-pre-line text-ink-2">
                  {code}
                </p>
              )}
            </li>
          ))}
        </ol>
      </div>
    </section>
  )
}
