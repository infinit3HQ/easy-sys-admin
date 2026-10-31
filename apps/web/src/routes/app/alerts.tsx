import { Link, createFileRoute } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import type { AlertRule, ChannelConfig, ChannelKind } from '@esa/api'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Ago, When } from '#/components/time'
import { Button, Empty, ErrorBanner, Field, Notice, PageHeader, Panel, Severity, Toggle, cx } from '#/components/ui'
import { useRole } from './route'

export const Route = createFileRoute('/app/alerts')({
  loader: ({ context }) => context.queryClient.ensureQueryData(q.alerts(false)),
  head: () => ({ meta: [{ title: 'Alerts · easy-sys-admin' }] }),
  component: Alerts,
})

function Alerts() {
  const [all, setAll] = useState(false)
  const alerts = useQuery(q.alerts(all))
  const items = alerts.data?.items ?? []
  return (
    <div className="space-y-8">
      <PageHeader kicker="Signals" title="Alerts">
        <Toggle checked={all} onChange={setAll} label="Include resolved" />
      </PageHeader>
      {items.length === 0 ? (
        <Empty title={all ? 'No alerts recorded' : 'Nothing is wrong'}>Rules and channels are below.</Empty>
      ) : (
        <div className="panel divide-y divide-line">
          {items.map((a) => (
            <div key={a.id} className={cx('flex flex-wrap items-center gap-4 px-4 py-3', a.resolvedAt && 'opacity-50')}>
              <Severity severity={a.severity} />
              <div className="min-w-0 flex-1">
                <div className="text-sm text-ink">{a.message}</div>
                <div className="font-mono text-[10px] uppercase tracking-wider text-faint">
                  {a.kind}
                  {a.serverId && (
                    <>
                      {' · '}
                      <Link to="/app/servers/$serverId" params={{ serverId: a.serverId }} className="hover:text-amber">
                        {a.serverName}
                      </Link>
                    </>
                  )}
                </div>
              </div>
              <div className="text-right font-mono text-[11px] text-muted">
                {a.resolvedAt ? <>resolved <Ago iso={a.resolvedAt} /></> : a.firedAt ? <>firing since <Ago iso={a.firedAt} /></> : <>pending since <Ago iso={a.pendingSince} /></>}
              </div>
            </div>
          ))}
        </div>
      )}
      <div className="grid gap-6 xl:grid-cols-2">
        <Rules />
        <div className="space-y-6">
          <Channels />
          <Certs />
        </div>
      </div>
    </div>
  )
}

const ruleText: Record<AlertRule['kind'], { title: string; unit?: string }> = {
  disk: { title: 'Disk use above', unit: '%' },
  inode: { title: 'Inode use above', unit: '%' },
  memory: { title: 'Memory use above', unit: '%' },
  unreachable: { title: 'Server not answering' },
  failed_unit: { title: 'A systemd unit failed' },
  container: { title: 'Container unhealthy or restarting' },
  reboot_required: { title: 'Reboot required' },
  cert_expiry: { title: 'Certificate expires within', unit: 'days' },
  run_failed: { title: 'A maintenance run failed' },
  host_key: { title: 'Host key changed' },
}

function Rules() {
  const rules = useSuspenseQuery(q.alertRules())
  return (
    <Panel title="Rules">
      <div className="divide-y divide-line">
        {rules.data.items.map((r) => (
          <RuleRow key={r.id} rule={r} />
        ))}
      </div>
    </Panel>
  )
}

function RuleRow({ rule }: { rule: AlertRule }) {
  const qc = useQueryClient()
  const { canAct } = useRole()
  const [r, setR] = useState(rule)
  const dirty = r.enabled !== rule.enabled || r.threshold !== rule.threshold || r.forMinutes !== rule.forMinutes
  const save = useMutation({
    mutationFn: () => call(api().PUT('/alerts/rules/{kind}', { params: { path: { kind: r.kind } }, body: { enabled: r.enabled, threshold: r.threshold, forMinutes: r.forMinutes } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['alert-rules'] }),
  })
  const t = ruleText[r.kind]
  return (
    <div className="flex flex-wrap items-center gap-3 py-2.5">
      <Toggle checked={r.enabled} onChange={(enabled) => setR({ ...r, enabled })} label={t.title} />
      {t.unit && (
        <input
          className="input w-20 !py-1 font-mono"
          type="number"
          value={r.threshold}
          disabled={!canAct}
          onChange={(e) => setR({ ...r, threshold: Number(e.target.value) })}
        />
      )}
      {t.unit && <span className="font-mono text-xs text-muted">{t.unit}</span>}
      <span className="ml-auto flex items-center gap-2 font-mono text-[11px] text-muted">
        for
        <input
          className="input w-20 !py-1 font-mono"
          type="number"
          value={r.forMinutes}
          disabled={!canAct}
          onChange={(e) => setR({ ...r, forMinutes: Number(e.target.value) })}
        />
        min
      </span>
      {canAct && dirty && (
        <Button size="sm" variant="primary" onClick={() => save.mutate()} busy={save.isPending}>
          Save
        </Button>
      )}
      {save.error && <div className="w-full"><ErrorBanner error={save.error} /></div>}
    </div>
  )
}

const kindFields: Record<ChannelKind, Array<{ key: keyof ChannelConfig; label: string; placeholder?: string; secret?: boolean }>> = {
  ntfy: [
    { key: 'url', label: 'Topic URL', placeholder: 'https://ntfy.sh/my-servers' },
    { key: 'token', label: 'Access token (optional)', secret: true },
  ],
  webhook: [
    { key: 'url', label: 'URL', placeholder: 'https://hooks.example.com/esa' },
    { key: 'secret', label: 'Signing secret (optional)', secret: true },
  ],
  discord: [{ key: 'url', label: 'Webhook URL', placeholder: 'https://discord.com/api/webhooks/…', secret: true }],
  slack: [{ key: 'url', label: 'Webhook URL', placeholder: 'https://hooks.slack.com/services/…', secret: true }],
  email: [
    { key: 'smtpHost', label: 'SMTP host', placeholder: 'smtp.example.com' },
    { key: 'smtpPort', label: 'SMTP port', placeholder: '587' },
    { key: 'username', label: 'Username' },
    { key: 'password', label: 'Password', secret: true },
    { key: 'from', label: 'From', placeholder: 'esa@example.com' },
    { key: 'to', label: 'To (comma-separated)', placeholder: 'you@example.com' },
  ],
}

function Channels() {
  const qc = useQueryClient()
  const { isOwner, canAct } = useRole()
  const channels = useQuery(q.channels())
  const [kind, setKind] = useState<ChannelKind>('ntfy')
  const [name, setName] = useState('')
  const [cfg, setCfg] = useState<Record<string, string>>({})
  const [tested, setTested] = useState<Record<number, string>>({})

  const create = useMutation({
    mutationFn: () => {
      const config: ChannelConfig = { ...cfg, smtpPort: cfg.smtpPort ? Number(cfg.smtpPort) : undefined, to: cfg.to ? cfg.to.split(',').map((t) => t.trim()) : undefined }
      return call(api().POST('/channels', { body: { name, kind, config } }))
    },
    onSuccess: () => {
      setName('')
      setCfg({})
      return qc.invalidateQueries({ queryKey: ['channels'] })
    },
  })
  const test = useMutation({
    mutationFn: (id: number) => call(api().POST('/channels/{channelId}/test', { params: { path: { channelId: id } } })),
    onSuccess: (_d, id) => setTested((t) => ({ ...t, [id]: 'sent' })),
    onError: (e, id) => setTested((t) => ({ ...t, [id]: e.message })),
  })
  const remove = useMutation({
    mutationFn: (id: number) => call(api().DELETE('/channels/{channelId}', { params: { path: { channelId: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['channels'] }),
  })

  return (
    <Panel title="Notification channels">
      <div className="space-y-2">
        {(channels.data?.items ?? []).length === 0 && <p className="text-sm text-muted">No channels. Alerts show here only.</p>}
        {(channels.data?.items ?? []).map((c) => (
          <div key={c.id} className="flex flex-wrap items-center gap-3 rounded border border-line px-3 py-2">
            <span className="font-mono text-xs uppercase text-amber">{c.kind}</span>
            <span className="text-sm text-ink">{c.name}</span>
            <span className="font-mono text-[11px] text-muted">{c.target}</span>
            <span className="ml-auto font-mono text-[10px] text-faint">{c.lastError ? <span className="text-fail">{c.lastError}</span> : c.lastSentAt ? <>sent <When iso={c.lastSentAt} /></> : ''}</span>
            {canAct && (
              <Button size="sm" variant="ghost" onClick={() => test.mutate(c.id)} busy={test.isPending && test.variables === c.id}>
                test
              </Button>
            )}
            {isOwner && (
              <Button size="sm" variant="ghost" onClick={() => remove.mutate(c.id)}>
                remove
              </Button>
            )}
            {tested[c.id] && <span className={cx('w-full font-mono text-[11px]', tested[c.id] === 'sent' ? 'text-ok' : 'text-fail')}>{tested[c.id]}</span>}
          </div>
        ))}
      </div>
      {isOwner ? (
        <form
          className="mt-5 space-y-3 border-t border-line pt-4"
          onSubmit={(e) => {
            e.preventDefault()
            create.mutate()
          }}
        >
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Kind">
              <select className="input" value={kind} onChange={(e) => (setKind(e.target.value as ChannelKind), setCfg({}))}>
                {(Object.keys(kindFields) as Array<ChannelKind>).map((k) => (
                  <option key={k}>{k}</option>
                ))}
              </select>
            </Field>
            <Field label="Name">
              <input className="input" value={name} onChange={(e) => setName(e.target.value)} placeholder="phone" required />
            </Field>
            {kindFields[kind].map((f) => (
              <Field key={f.key} label={f.label}>
                <input
                  className="input font-mono"
                  type={f.secret ? 'password' : 'text'}
                  autoComplete="off"
                  placeholder={f.placeholder}
                  value={cfg[f.key] ?? ''}
                  onChange={(e) => setCfg({ ...cfg, [f.key]: e.target.value })}
                />
              </Field>
            ))}
          </div>
          <ErrorBanner error={create.error} />
          <Button type="submit" variant="primary" busy={create.isPending}>
            Add channel
          </Button>
        </form>
      ) : (
        <p className="mt-4 text-xs text-muted">Only account owners can add channels, because they hold credentials.</p>
      )}
    </Panel>
  )
}

function Certs() {
  const qc = useQueryClient()
  const { canAct } = useRole()
  const certs = useQuery(q.certs())
  const [host, setHost] = useState('')
  const add = useMutation({
    mutationFn: () => call(api().POST('/certs', { body: { hostname: host, port: 443 } })),
    onSuccess: () => {
      setHost('')
      return qc.invalidateQueries({ queryKey: ['certs'] })
    },
  })
  const del = useMutation({
    mutationFn: (id: number) => call(api().DELETE('/certs/{certId}', { params: { path: { certId: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['certs'] }),
  })
  return (
    <Panel title="TLS certificates">
      <div className="space-y-1.5">
        {(certs.data?.items ?? []).map((c) => (
          <div key={c.id} className="flex items-center gap-3 font-mono text-xs">
            <span className="text-ink">
              {c.hostname}
              {c.port !== 443 && `:${c.port}`}
            </span>
            <span className="text-faint">{c.issuer}</span>
            <span className={cx('ml-auto', c.lastError ? 'text-fail' : (c.daysLeft ?? 99) <= 14 ? 'text-warn' : 'text-ok')}>
              {c.lastError ? c.lastError : `${c.daysLeft} days`}
            </span>
            {canAct && (
              <button className="text-muted hover:text-fail" onClick={() => del.mutate(c.id)} aria-label={`Stop checking ${c.hostname}`}>
                ×
              </button>
            )}
          </div>
        ))}
        {(certs.data?.items ?? []).length === 0 && <p className="text-sm text-muted">Add the hostnames your servers serve. esa checks them twice a day.</p>}
      </div>
      {canAct && (
        <form
          className="mt-4 flex gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            add.mutate()
          }}
        >
          <input className="input font-mono" value={host} onChange={(e) => setHost(e.target.value)} placeholder="app.example.com" />
          <Button type="submit" busy={add.isPending} disabled={!host}>
            Add
          </Button>
        </form>
      )}
      <div className="mt-2">
        <ErrorBanner error={add.error} />
      </div>
      {add.isSuccess && <div className="mt-2"><Notice tone="ok">Checked {add.data.hostname}.</Notice></div>}
    </Panel>
  )
}
