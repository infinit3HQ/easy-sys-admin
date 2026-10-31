import { useQuery } from '@tanstack/react-query'
import { q } from '#/lib/queries'
import { Ago, Until, When } from '#/components/time'
import { ErrorBanner, Loading, Panel, cx } from '#/components/ui'

export function AccessTab({ serverId }: { serverId: number }) {
  const r = useQuery(q.access(serverId))
  if (r.isLoading) return <Loading />
  if (r.error || !r.data) return <ErrorBanner error={r.error} />
  const d = r.data
  const fpCount = new Map<string, number>()
  d.users.forEach((u) => u.authorizedKeys.forEach((k) => fpCount.set(k.fingerprint, (fpCount.get(k.fingerprint) ?? 0) + 1)))

  return (
    <div className="grid gap-6 xl:grid-cols-[2fr_1fr]">
      <Panel title="Users with a login shell">
        <div className="space-y-5">
          {d.users.map((u) => (
            <div key={u.name}>
              <div className="flex flex-wrap items-baseline gap-3">
                <span className="font-mono text-sm text-ink">{u.name}</span>
                <span className="font-mono text-[10px] text-faint">
                  uid {u.uid} · {u.shell} · {u.home}
                </span>
                {Object.entries(d.groups)
                  .filter(([, m]) => m.includes(u.name))
                  .map(([g]) => (
                    <span key={g} className={cx('rounded-sm px-1.5 font-mono text-[9px] uppercase', g === 'sudo' || g === 'docker' ? 'bg-warn text-bg' : 'bg-line-2 text-ink-2')}>
                      {g}
                    </span>
                  ))}
              </div>
              {u.authorizedKeys.length === 0 ? (
                <p className="mt-1 font-mono text-[11px] text-faint">no SSH keys</p>
              ) : (
                <ul className="mt-2 space-y-1">
                  {u.authorizedKeys.map((k) => (
                    <li key={k.fingerprint + k.comment} className="flex flex-wrap gap-x-3 font-mono text-[11px]">
                      <span className="text-muted">{k.type}</span>
                      <span className="text-ink-2">{k.fingerprint}</span>
                      <span className="text-faint">{k.comment}</span>
                      {(fpCount.get(k.fingerprint) ?? 0) > 1 && <span className="text-warn">same key on {fpCount.get(k.fingerprint)} users</span>}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          ))}
        </div>
      </Panel>
      <div className="space-y-6">
        <Panel title="SSH server">
          <dl className="space-y-2 font-mono text-xs">
            <Line k="Password login" v={d.passwordAuth ? 'allowed' : 'off'} bad={d.passwordAuth} />
            <Line k="Root login" v={d.permitRootLogin || 'unknown'} bad={d.permitRootLogin === 'yes'} />
            <Line k="Failed logins, 24h" v={String(d.failedSsh24h)} bad={d.failedSsh24h > 100} />
          </dl>
        </Panel>
        <Panel title="Recent logins">
          <ul className="space-y-1.5 font-mono text-[11px]">
            {d.logins.map((l, i) => (
              <li key={i} className="flex justify-between gap-3">
                <span className="text-ink-2">
                  {l.user} <span className="text-faint">from {l.from || 'console'}</span>
                </span>
                <span className="text-muted"><Ago iso={l.at} /></span>
              </li>
            ))}
          </ul>
        </Panel>
      </div>
    </div>
  )
}

export function SecurityTab({ serverId }: { serverId: number }) {
  const r = useQuery(q.security(serverId))
  if (r.isLoading) return <Loading />
  if (r.error || !r.data) return <ErrorBanner error={r.error} />
  const d = r.data
  const exposed = (a: string) => a === '0.0.0.0' || a === '::' || a === '*'
  return (
    <div className="grid gap-6 xl:grid-cols-[2fr_1fr]">
      <Panel title="Listening sockets">
        <table className="w-full text-left font-mono text-xs">
          <thead>
            <tr className="text-muted">
              <th className="pb-2 font-normal">Port</th>
              <th className="pb-2 font-normal">Proto</th>
              <th className="pb-2 font-normal">Address</th>
              <th className="pb-2 font-normal">Process</th>
            </tr>
          </thead>
          <tbody>
            {d.listening.map((l, i) => (
              <tr key={i} className="border-t border-line/60">
                <td className={cx('py-1.5', exposed(l.address) ? 'text-amber' : 'text-ink-2')}>{l.port}</td>
                <td className="py-1.5 text-muted">{l.proto}</td>
                <td className="py-1.5 text-ink-2">{exposed(l.address) ? `${l.address} (all interfaces)` : l.address}</td>
                <td className="py-1.5 text-ink">{l.process}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Panel>
      <Panel title="Protection">
        <dl className="space-y-2 font-mono text-xs">
          <Line k="Firewall" v={d.firewall} bad={d.firewall === 'none'} />
          <Line k="CrowdSec" v={d.crowdsec} bad={d.crowdsec === 'failed'} />
          <Line k="fail2ban" v={d.fail2ban} bad={d.fail2ban === 'failed'} />
          <Line k="unattended-upgrades" v={d.unattendedUpgrades ? 'on' : 'off'} bad={!d.unattendedUpgrades} />
        </dl>
      </Panel>
    </div>
  )
}

export function JobsTab({ serverId }: { serverId: number }) {
  const r = useQuery(q.jobs(serverId))
  if (r.isLoading) return <Loading />
  if (r.error || !r.data) return <ErrorBanner error={r.error} />
  const d = r.data
  return (
    <div className="space-y-6">
      <Panel title={`systemd timers · ${d.timers.length}`}>
        <table className="w-full text-left font-mono text-xs">
          <tbody>
            {d.timers.map((t) => (
              <tr key={t.unit} className="border-t border-line/60 first:border-0">
                <td className={cx('py-1.5', t.failed ? 'text-fail' : 'text-ink')}>{t.unit}</td>
                <td className="py-1.5 text-muted">{t.activates}</td>
                <td className="py-1.5 text-ink-2">next <Until iso={t.next} /></td>
                <td className="py-1.5 text-muted">last {t.last ? <When iso={t.last} /> : 'never'}</td>
                <td className={cx('py-1.5', t.result && t.result !== 'success' ? 'text-fail' : 'text-faint')}>{t.result}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Panel>
      <Panel title={`cron · ${d.cron.length}`}>
        <table className="w-full text-left font-mono text-xs">
          <tbody>
            {d.cron.map((c, i) => (
              <tr key={i} className="border-t border-line/60 align-top first:border-0">
                <td className="whitespace-nowrap py-1.5 pr-4 text-amber-2">{c.schedule}</td>
                <td className="py-1.5 pr-4 text-ink-2">{c.user}</td>
                <td className="break-all py-1.5 pr-4 text-ink">{c.command}</td>
                <td className="whitespace-nowrap py-1.5 text-faint">{c.source}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Panel>
    </div>
  )
}

function Line({ k, v, bad }: { k: string; v: string; bad?: boolean }) {
  return (
    <div className="flex justify-between gap-4">
      <dt className="text-muted">{k}</dt>
      <dd className={bad ? 'text-warn' : 'text-ink'}>{v}</dd>
    </div>
  )
}
