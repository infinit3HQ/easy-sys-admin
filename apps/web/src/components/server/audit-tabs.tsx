import { useQuery } from '@tanstack/react-query'
import { q } from '#/lib/queries'
import { Ago, Until, When } from '#/components/time'
import {
  ErrorBanner,
  KeyValue,
  Loading,
  Notice,
  Panel,
  cn,
} from '#/components/kit'
import { Badge } from '#/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '#/components/ui/table'

export function AccessTab({ serverId }: { serverId: number }) {
  const r = useQuery(q.access(serverId))
  if (r.isLoading) return <Loading />
  if (r.error || !r.data) return <ErrorBanner error={r.error} />
  const d = r.data
  const fpCount = new Map<string, number>()
  d.users.forEach((u) =>
    u.authorizedKeys.forEach((k) =>
      fpCount.set(k.fingerprint, (fpCount.get(k.fingerprint) ?? 0) + 1),
    ),
  )

  return (
    <div className="grid gap-6 xl:grid-cols-[2fr_1fr]">
      {d.gaps.includes('helper') && (
        <Notice className="xl:col-span-2">
          This helper is too old for a complete access report. Reinstall it.
          What you see below is only what that helper could read.
        </Notice>
      )}
      <Panel title="Users with a login shell" flush>
        <ul className="divide-y divide-line/60">
          {d.users.map((u) => (
            <li key={u.name} className="px-4 py-3">
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                <span className="font-mono text-sm text-ink">{u.name}</span>
                {u.uid === 0 && u.name !== 'root' && (
                  <Badge variant="warn">uid 0</Badge>
                )}
                {u.password && (
                  <Badge
                    variant={
                      u.password === 'set' || u.password === 'none'
                        ? 'warn'
                        : 'secondary'
                    }
                  >
                    password {u.password}
                  </Badge>
                )}
                {Object.entries(d.groups)
                  .filter(([, m]) => m.includes(u.name))
                  .map(([g]) => (
                    <Badge
                      key={g}
                      variant={
                        g === 'sudo' || g === 'docker' ? 'warn' : 'secondary'
                      }
                    >
                      {g}
                    </Badge>
                  ))}
              </div>
              <div className="mt-0.5 font-mono text-[10px] break-all text-faint">
                uid {u.uid} · {u.shell} · {u.home}
              </div>
              {u.authorizedKeys.length === 0 ? (
                <p className="mt-1.5 font-mono text-[11px] text-faint">
                  no SSH keys
                </p>
              ) : (
                <ul className="mt-2 space-y-1.5">
                  {u.authorizedKeys.map((k) => (
                    <li
                      key={k.fingerprint + k.comment}
                      className="flex flex-wrap gap-x-3 font-mono text-[11px]"
                    >
                      <span className="text-muted-foreground">{k.type}</span>
                      <span className="break-all text-ink-2">
                        {k.fingerprint}
                      </span>
                      <span className="break-all text-faint">{k.comment}</span>
                      {k.from && (
                        <span className="break-all text-faint">
                          from {k.from}
                        </span>
                      )}
                      {k.hasCommand && (
                        <span className="text-faint">forced command</span>
                      )}
                      {u.uid === 0 && !k.from && !k.hasCommand && (
                        <span className="text-warn">no restriction</span>
                      )}
                      {(fpCount.get(k.fingerprint) ?? 0) > 1 && (
                        <span className="text-warn">
                          same key on {fpCount.get(k.fingerprint)} users
                        </span>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </li>
          ))}
        </ul>
      </Panel>
      <div className="min-w-0 space-y-6">
        <Panel title="SSH server">
          <dl className="space-y-2">
            <KeyValue
              k="Password login"
              v={d.passwordAuth ? 'allowed' : 'off'}
              tone={d.passwordAuth ? 'warn' : undefined}
            />
            <KeyValue
              k="Root login"
              v={d.permitRootLogin || 'unknown'}
              tone={d.permitRootLogin === 'yes' ? 'warn' : undefined}
            />
            <KeyValue
              k="Empty passwords"
              v={
                d.sshRead
                  ? d.permitEmptyPasswords
                    ? 'allowed'
                    : 'off'
                  : 'unknown'
              }
              tone={d.permitEmptyPasswords ? 'warn' : undefined}
            />
            <KeyValue
              k="Public key login"
              v={d.sshRead ? (d.pubkeyAuth ? 'on' : 'off') : 'unknown'}
              tone={d.sshRead && !d.pubkeyAuth ? 'warn' : undefined}
            />
            <KeyValue
              k="Keyboard interactive"
              v={d.sshRead ? (d.kbdInteractive ? 'on' : 'off') : 'unknown'}
              tone={d.kbdInteractive ? 'warn' : undefined}
            />
            <KeyValue
              k="X11 forwarding"
              v={d.sshRead ? (d.x11Forwarding ? 'on' : 'off') : 'unknown'}
              tone={d.x11Forwarding ? 'warn' : undefined}
            />
            <KeyValue
              k="TCP forwarding"
              v={d.sshRead ? d.allowTcpForwarding || 'unknown' : 'unknown'}
              tone={
                d.allowTcpForwarding && d.allowTcpForwarding !== 'no'
                  ? 'warn'
                  : undefined
              }
            />
            <KeyValue
              k="Listen"
              v={d.listenPorts.length ? d.listenPorts.join(', ') : 'unknown'}
            />
            <KeyValue
              k="Failed logins, 24h"
              v={String(d.failedSsh24h)}
              tone={d.failedSsh24h > 100 ? 'warn' : undefined}
            />
          </dl>
        </Panel>
        <Panel title="Sudo rules">
          {d.revision < 1 ? (
            <p className="text-sm text-muted-foreground">
              Reinstall the helper to read sudoers.
            </p>
          ) : d.sudoRules.length === 0 ? (
            <p className="text-sm text-muted-foreground">No sudo rules read.</p>
          ) : (
            <ul className="space-y-2">
              {d.sudoRules.map((rule, i) => (
                <li key={i} className="font-mono text-[11px] break-all">
                  <span className="text-ink">{rule.who}</span>
                  {rule.runAs && (
                    <span className="text-faint"> ({rule.runAs})</span>
                  )}{' '}
                  <span className="text-ink-2">{rule.commands}</span>
                  {rule.noPassword && (
                    <span className="text-warn"> NOPASSWD</span>
                  )}
                </li>
              ))}
            </ul>
          )}
          {d.sudoIncomplete && (
            <p className="mt-3 text-xs text-muted-foreground">
              Some sudoers includes were skipped. Only /etc/sudoers and
              /etc/sudoers.d are read.
            </p>
          )}
        </Panel>
        <Panel title="Recent logins">
          {d.logins.length === 0 ? (
            <p className="text-sm text-muted-foreground">No logins recorded.</p>
          ) : (
            <dl className="space-y-2">
              {d.logins.map((l, i) => (
                <KeyValue
                  key={i}
                  k={
                    <span className="text-ink-2">
                      {l.user}{' '}
                      <span className="text-faint">
                        from {l.from || 'console'}
                      </span>
                    </span>
                  }
                  v={<Ago iso={l.at} />}
                />
              ))}
            </dl>
          )}
        </Panel>
      </div>
    </div>
  )
}

function banText(
  state: string,
  jails: ReadonlyArray<{ name: string; banned: number }>,
) {
  if (state !== 'active' || jails.length === 0) return state
  const n = jails.reduce((sum, j) => sum + j.banned, 0)
  const hot = jails.filter((j) => j.banned > 0).map((j) => j.name)
  return hot.length
    ? `${state}, ${n} banned (${hot.join(', ')})`
    : `${state}, 0 banned`
}

export function SecurityTab({ serverId }: { serverId: number }) {
  const r = useQuery(q.security(serverId))
  if (r.isLoading) return <Loading />
  if (r.error || !r.data) return <ErrorBanner error={r.error} />
  const d = r.data
  const exposed = (a: string) => a === '0.0.0.0' || a === '::' || a === '*'
  return (
    <div className="grid gap-6 xl:grid-cols-[2fr_1fr]">
      {d.gaps.includes('helper') && (
        <Notice className="xl:col-span-2">
          This helper is too old for a complete security report. Reinstall it.
          An empty list here is not a clean result.
        </Notice>
      )}
      <Panel title="Listening sockets" flush>
        <p className="px-4 pt-3 text-xs text-muted-foreground">
          A socket on 0.0.0.0 or :: listens on all interfaces. That does not
          show whether a network can reach it.
        </p>
        <Table className="min-w-[480px] font-mono text-xs">
          <TableHeader>
            <TableRow>
              <TableHead className="pl-4">Port</TableHead>
              <TableHead>Proto</TableHead>
              <TableHead>Address</TableHead>
              <TableHead>Process</TableHead>
              <TableHead className="pr-4">User</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {d.listening.map((l, i) => (
              <TableRow key={i}>
                <TableCell
                  className={cn(
                    'py-2 pl-4',
                    exposed(l.address) ? 'text-amber' : 'text-ink-2',
                  )}
                >
                  {l.port}
                </TableCell>
                <TableCell className="py-2 text-muted-foreground">
                  {l.proto}
                </TableCell>
                <TableCell className="py-2 text-ink-2">
                  {exposed(l.address)
                    ? `${l.address} (all interfaces, reachability not verified)`
                    : l.address}
                  {l.ufw === 'unmentioned' && exposed(l.address) && (
                    <span className="text-warn"> not in ufw</span>
                  )}
                  {l.ufw === 'deny' && (
                    <span className="text-warn"> denied by ufw</span>
                  )}
                </TableCell>
                <TableCell className="py-2 text-ink">
                  {l.process}
                  {l.container ? ` · ${l.container}` : ''}
                </TableCell>
                <TableCell className="py-2 pr-4 text-muted-foreground">
                  {l.user}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Panel>
      <Panel title="Protection">
        <dl className="space-y-2">
          <KeyValue
            k="Firewall"
            v={d.firewall}
            tone={d.firewall === 'none' ? 'warn' : undefined}
          />
          {d.crowdsec !== 'not installed' && (
            <KeyValue
              k="CrowdSec"
              v={
                d.crowdsec === 'active'
                  ? `${d.crowdsec}, ${d.crowdSecBans} bans`
                  : d.crowdsec
              }
              tone={d.crowdsec === 'failed' ? 'warn' : undefined}
            />
          )}
          {d.fail2ban !== 'not installed' && (
            <KeyValue
              k="fail2ban"
              v={banText(d.fail2ban, d.fail2banJails)}
              tone={d.fail2ban === 'failed' ? 'warn' : undefined}
            />
          )}
          <KeyValue
            k="unattended-upgrades"
            v={d.unattendedUpgrades ? 'on' : 'off'}
            tone={!d.unattendedUpgrades ? 'warn' : undefined}
          />
        </dl>
        {d.firewallRules.length > 0 && (
          <ul className="mt-4 space-y-1 border-t border-line/60 pt-3 font-mono text-[11px] text-ink-2">
            {d.firewallRules.map((rule) => (
              <li key={rule} className="break-all">
                {rule}
              </li>
            ))}
          </ul>
        )}
        {d.revision < 1 && (
          <p className="mt-3 text-xs text-muted-foreground">
            Reinstall the helper for users, exposure, and firewall rules.
          </p>
        )}
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
      <Panel title={`systemd timers · ${d.timers.length}`} flush>
        <Table className="min-w-[640px] font-mono text-xs">
          <TableHeader>
            <TableRow>
              <TableHead className="pl-4">Timer</TableHead>
              <TableHead>Runs</TableHead>
              <TableHead>Next</TableHead>
              <TableHead>Last</TableHead>
              <TableHead className="pr-4">Result</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {d.timers.map((t) => (
              <TableRow key={t.unit}>
                <TableCell
                  className={cn(
                    'py-2 pl-4',
                    t.failed ? 'text-fail' : 'text-ink',
                  )}
                >
                  {t.unit}
                </TableCell>
                <TableCell className="py-2 text-muted-foreground">
                  {t.activates}
                </TableCell>
                <TableCell className="py-2 text-ink-2">
                  <Until iso={t.next} />
                </TableCell>
                <TableCell className="py-2 text-muted-foreground">
                  {t.last ? <When iso={t.last} /> : 'never'}
                </TableCell>
                <TableCell
                  className={cn(
                    'py-2 pr-4',
                    t.result && t.result !== 'success'
                      ? 'text-fail'
                      : 'text-faint',
                  )}
                >
                  {t.result}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Panel>
      <Panel title={`cron · ${d.cron.length}`} flush>
        {d.cron.length === 0 ? (
          <p className="p-4 text-sm text-muted-foreground">No cron entries.</p>
        ) : (
          <ul className="divide-y divide-line/60 font-mono text-xs">
            {d.cron.map((c, i) => (
              <li
                key={i}
                className="grid gap-x-4 gap-y-1 px-4 py-2.5 md:grid-cols-[160px_100px_minmax(0,1fr)_auto]"
              >
                <span className="text-amber-2">{c.schedule}</span>
                <span className="text-ink-2">{c.user}</span>
                <span className="break-all text-ink">
                  {c.command || (
                    <span className="text-faint">
                      hidden: needs admin and logs:read
                    </span>
                  )}
                </span>
                <span className="text-[10px] break-all text-faint md:text-xs">
                  {c.source}
                </span>
              </li>
            ))}
          </ul>
        )}
      </Panel>
    </div>
  )
}
