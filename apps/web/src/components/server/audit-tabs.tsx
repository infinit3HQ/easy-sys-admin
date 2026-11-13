import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, call } from '#/lib/api'
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
import { Button } from '#/components/ui/button'
import { Input } from '#/components/ui/input'
import { useServerAccess } from '#/components/server/use-access'
import { ScanPanel } from '#/components/server/scan-panel'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '#/components/ui/table'

/** The helper report revision the sections below need (proto.ReportRevision). An older helper leaves them empty, which is not a clean result. */
const HELPER_REPORT_REVISION = 2

export function AccessTab({ serverId }: { serverId: number }) {
  const r = useQuery(q.access(serverId))
  const detail = useQuery(q.server(serverId))
  const { canManage } = useServerAccess(
    detail.data?.server ?? {
      access: 'view',
      yourRole: 'viewer',
      ownerTeam: '',
    },
  )
  const approve = useMutation({
    mutationFn: () =>
      call(
        api().POST('/servers/{serverId}/key-baseline', {
          params: { path: { serverId } },
        }),
      ),
  })
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
      {canManage && (
        <div className="xl:col-span-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-xs text-muted-foreground">
            Approving stores the fingerprints recorded now. Later checks report
            a key that appears, disappears, or changes its restriction. A
            missing key is not proof that access was removed.
          </p>
          <Button
            size="sm"
            variant="outline"
            busy={approve.isPending}
            onClick={() => approve.mutate()}
          >
            {approve.isSuccess ? 'Approved' : 'Approve these keys'}
          </Button>
        </div>
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
      <KeyExceptions serverId={serverId} canEdit={canManage} />
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
          {d.revision < HELPER_REPORT_REVISION ? (
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

function DatasetPolicies({ canEdit }: { canEdit: boolean }) {
  const qc = useQueryClient()
  const r = useQuery(q.datasets())
  const [name, setName] = useState('')
  const [hours, setHours] = useState('24')
  const create = useMutation({
    mutationFn: () =>
      call(
        api().POST('/datasets', {
          body: { name: name.trim(), maxAgeHours: Number(hours) },
        }),
      ),
    onSuccess: async () => {
      setName('')
      await qc.invalidateQueries({ queryKey: ['datasets'] })
      await qc.invalidateQueries({ queryKey: ['recovery'] })
    },
  })
  const remove = useMutation({
    mutationFn: (id: number) =>
      call(
        api().DELETE('/datasets/{datasetId}', {
          params: { path: { datasetId: id } },
        }),
      ),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['datasets'] })
      await qc.invalidateQueries({ queryKey: ['recovery'] })
    },
  })
  const items = r.data?.items ?? []
  return (
    <div className="mt-3 border-t border-line pt-3">
      {items.length > 0 && (
        <ul className="mb-2 space-y-1 font-mono text-xs">
          {items.map((d) => (
            <li key={d.id} className="flex items-center justify-between gap-2">
              <span>
                {d.name}: {d.maxAgeHours}h
              </span>
              {canEdit && (
                <button
                  type="button"
                  className="text-faint hover:text-fail"
                  onClick={() => remove.mutate(d.id)}
                >
                  remove
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {canEdit && (
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            create.mutate()
          }}
        >
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="postgres"
            aria-label="Dataset name"
            className="h-8 w-36"
          />
          <Input
            value={hours}
            onChange={(e) => setHours(e.target.value)}
            inputMode="numeric"
            aria-label="Maximum age in hours"
            className="h-8 w-20"
          />
          <Button
            type="submit"
            size="sm"
            variant="outline"
            busy={create.isPending}
          >
            Add policy
          </Button>
        </form>
      )}
      <ErrorBanner
        error={r.error ?? create.error ?? remove.error}
        className="mt-2"
      />
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
  const detail = useQuery(q.server(serverId))
  const { canManage } = useServerAccess(
    detail.data?.server ?? {
      access: 'view',
      ownerTeam: null,
      yourRole: 'viewer',
    },
  )
  const r = useQuery(q.security(serverId))
  const recovery = useQuery(q.recovery(serverId))
  const advisories = useQuery(q.advisories(serverId))
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
      <Panel title="Recovery" className="xl:col-span-2">
        <p className="text-xs text-muted-foreground">
          {recovery.data?.statement ??
            'Esa reads result files the operator wrote. It does not run a backup or a restore.'}
        </p>
        {recovery.data && recovery.data.gaps.length > 0 ? (
          <p className="mt-2 font-mono text-xs text-warn">
            {recovery.data.gaps.join(', ')}
          </p>
        ) : null}
        <ul className="mt-2 space-y-1 font-mono text-xs">
          {recovery.data?.sources.map((s) => (
            <li key={`${s.tool}-${s.dataset}`}>
              {s.tool} {s.dataset}: backup {s.backupState}
              {s.freshness ? ` (${s.freshness})` : ''}
              {s.policy ? `, policy ${s.policy}` : ''}, check {s.checkKind}{' '}
              {s.checkState}, restore {s.restoreState}, smoke {s.smokeState}
              {s.note ? `. ${s.note}` : ''}
            </li>
          ))}
        </ul>
        <p className="mt-2 text-xs text-muted-foreground">
          A dataset with no policy is not current. within means the backup time
          is inside the account limit. It is not proof the data can be restored.
        </p>
        <DatasetPolicies canEdit={canManage} />
      </Panel>
      <ScanPanel
        serverId={serverId}
        serverName={detail.data?.server.name ?? 'server'}
        canImport={canManage}
      />
      {advisories.data && (
        <Panel title="Advisories" className="xl:col-span-2">
          <p className="text-xs text-muted-foreground">
            {advisories.data.coverage}
          </p>
          <p className="mt-2 font-mono text-xs text-faint">
            {advisories.data.distro} {advisories.data.release} ·{' '}
            {advisories.data.packages} packages · feed {advisories.data.feed}
          </p>
          <ul className="mt-2 space-y-1 font-mono text-xs">
            {advisories.data.matches.map((m) => (
              <li key={`${m.id}-${m.package}`}>
                {m.id} {m.package} {m.version} · fixed {m.fixed || 'unknown'} ·{' '}
                {m.severity}
              </li>
            ))}
          </ul>
        </Panel>
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
                  {l.ufw === 'unknown' && (
                    <span className="text-warn">
                      {' '}
                      protection unknown (Docker may bypass ufw)
                    </span>
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
          {d.firewallDefaultIncoming && (
            <KeyValue
              k="Incoming by default"
              v={d.firewallDefaultIncoming}
              tone={d.firewallDefaultIncoming === 'allow' ? 'warn' : undefined}
            />
          )}
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
        {d.revision < HELPER_REPORT_REVISION && (
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

function KeyExceptions({
  serverId,
  canEdit,
}: {
  serverId: number
  canEdit: boolean
}) {
  const qc = useQueryClient()
  const r = useQuery(q.keyExceptions(serverId))
  const [user, setUser] = useState('')
  const [fingerprint, setFingerprint] = useState('')
  const [days, setDays] = useState('7')
  const [reason, setReason] = useState('')
  const create = useMutation({
    mutationFn: () =>
      call(
        api().POST('/servers/{serverId}/key-exceptions', {
          params: { path: { serverId } },
          body: {
            user: user.trim(),
            fingerprint: fingerprint.trim(),
            until: new Date(
              Date.now() + Number(days) * 24 * 60 * 60 * 1000,
            ).toISOString(),
            reason: reason.trim(),
          },
        }),
      ),
    onSuccess: async () => {
      setFingerprint('')
      setReason('')
      await qc.invalidateQueries({ queryKey: ['key-exceptions', serverId] })
    },
  })
  const remove = useMutation({
    mutationFn: (id: number) =>
      call(
        api().DELETE('/servers/{serverId}/key-exceptions/{exceptionId}', {
          params: { path: { serverId, exceptionId: id } },
        }),
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: ['key-exceptions', serverId] }),
  })
  const items = r.data?.items ?? []
  return (
    <Panel title="Expected until a date" className="xl:col-span-2">
      <p className="text-xs text-muted-foreground">
        An extra fingerprint on one login stays quiet until the date. The key
        stays on the server. This does not mean access was removed.
      </p>
      {items.length > 0 && (
        <ul className="mt-2 space-y-1 font-mono text-xs">
          {items.map((e) => (
            <li key={e.id} className="flex flex-wrap items-baseline gap-x-3">
              <span className="text-ink">{e.user}</span>
              <span className="break-all text-ink-2">{e.fingerprint}</span>
              <span className="text-faint">
                until <When iso={e.until} />
              </span>
              <span className="text-faint">{e.reason}</span>
              {canEdit && (
                <button
                  type="button"
                  className="text-faint hover:text-fail"
                  onClick={() => remove.mutate(e.id)}
                >
                  remove
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {canEdit && (
        <form
          className="mt-3 flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            create.mutate()
          }}
        >
          <Input
            value={user}
            onChange={(e) => setUser(e.target.value)}
            placeholder="user"
            aria-label="Login name"
            className="h-8 w-28"
          />
          <Input
            value={fingerprint}
            onChange={(e) => setFingerprint(e.target.value)}
            placeholder="SHA256:..."
            aria-label="Fingerprint"
            className="h-8 min-w-48 flex-1"
          />
          <Input
            value={days}
            onChange={(e) => setDays(e.target.value)}
            inputMode="numeric"
            aria-label="Days"
            className="h-8 w-16"
          />
          <Input
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="why"
            aria-label="Reason"
            className="h-8 w-40"
          />
          <Button
            type="submit"
            size="sm"
            variant="outline"
            busy={create.isPending}
          >
            Expect until then
          </Button>
        </form>
      )}
      <ErrorBanner
        error={r.error ?? create.error ?? remove.error}
        className="mt-2"
      />
    </Panel>
  )
}
