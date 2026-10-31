import { Link, createFileRoute, useNavigate } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import type { Enrollment, ProbeResult } from '@esa/api'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Button, CopyBlock, ErrorBanner, Field, Notice, PageHeader, Panel, Toggle, cx } from '#/components/ui'
import { useRole } from '../route'

export const Route = createFileRoute('/app/servers/new')({
  head: () => ({ meta: [{ title: 'Add server · easy-sys-admin' }] }),
  component: AddServer,
})

type Method = 'login' | 'command'

function AddServer() {
  const [method, setMethod] = useState<Method>('login')
  return (
    <div className="max-w-3xl">
      <PageHeader kicker={<Link to="/app">Board</Link>} title="Add a server" />
      <div className="mb-6 grid grid-cols-2 gap-px overflow-hidden rounded-md border border-line bg-line">
        {(
          [
            ['login', 'One-time sudo login', 'Give esa a root or sudo login once. It installs the helper, then forgets the login.'],
            ['command', 'Paste a command', 'Get a one-time command to run on the server yourself. No login leaves your hands.'],
          ] as const
        ).map(([m, title, body]) => (
          <button
            key={m}
            onClick={() => setMethod(m)}
            className={cx('p-4 text-left transition-colors', method === m ? 'bg-panel-2' : 'bg-bg hover:bg-panel')}
          >
            <div className={cx('font-display text-xl font-semibold uppercase', method === m ? 'text-amber' : 'text-ink')}>{title}</div>
            <div className="mt-1 text-xs text-muted">{body}</div>
          </button>
        ))}
      </div>
      {method === 'login' ? <LoginFlow /> : <CommandFlow />}
    </div>
  )
}

function useGroups() {
  return useQuery(q.groups()).data?.items ?? []
}

function BasicFields({ v, set }: { v: Basic; set: (b: Basic) => void }) {
  const groups = useGroups()
  return (
    <div className="grid gap-4 sm:grid-cols-[1fr_2fr_100px]">
      <Field label="Name">
        <input className="input" value={v.name} onChange={(e) => set({ ...v, name: e.target.value })} placeholder="media-01" required />
      </Field>
      <Field label="IP or hostname">
        <input className="input font-mono" value={v.host} onChange={(e) => set({ ...v, host: e.target.value })} placeholder="203.0.113.10" required />
      </Field>
      <Field label="SSH port">
        <input className="input font-mono" type="number" value={v.port} onChange={(e) => set({ ...v, port: Number(e.target.value) })} />
      </Field>
      <Field label="Group">
        <select className="input" value={v.groupId ?? ''} onChange={(e) => set({ ...v, groupId: e.target.value ? Number(e.target.value) : null })}>
          <option value="">No group</option>
          {groups.map((g) => (
            <option key={g.id} value={g.id}>
              {g.name}
            </option>
          ))}
        </select>
      </Field>
      <div className="sm:col-span-2">
        <Toggle
          checked={v.hostsControlPlane}
          onChange={(c) => set({ ...v, hostsControlPlane: c })}
          label="esa itself runs on this server"
          hint="It is then never rebooted by a schedule."
        />
      </div>
    </div>
  )
}

interface Basic {
  name: string
  host: string
  port: number
  groupId: number | null
  hostsControlPlane: boolean
}

const emptyBasic: Basic = { name: '', host: '', port: 22, groupId: null, hostsControlPlane: false }

function LoginFlow() {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { isOwner } = useRole()
  const [basic, setBasic] = useState<Basic>(emptyBasic)
  const [probe, setProbe] = useState<ProbeResult | null>(null)
  const [accepted, setAccepted] = useState(false)
  const [username, setUsername] = useState('root')
  const [authKind, setAuthKind] = useState<'key' | 'password'>('key')
  const [secret, setSecret] = useState('')
  const [passphrase, setPassphrase] = useState('')
  const [keepLogin, setKeepLogin] = useState(false)

  const probeM = useMutation({
    mutationFn: () => call(api().POST('/servers/probe', { body: { host: basic.host, port: basic.port } })),
    onSuccess: (r) => {
      setProbe(r)
      setAccepted(false)
    },
  })
  const addM = useMutation({
    mutationFn: () =>
      call(
        api().POST('/servers', {
          body: {
            name: basic.name,
            host: basic.host,
            port: basic.port,
            groupId: basic.groupId,
            hostsControlPlane: basic.hostsControlPlane,
            hostKey: probe?.hostKey ?? '',
            keepLogin,
            login:
              authKind === 'key'
                ? { username, privateKey: secret, passphrase: passphrase || undefined }
                : { username, password: secret },
          },
        }),
      ),
    onSuccess: async (s) => {
      setSecret('')
      await qc.invalidateQueries({ queryKey: ['overview'] })
      await navigate({ to: '/app/servers/$serverId', params: { serverId: s.id } })
    },
  })

  return (
    <div className="space-y-6">
      <Panel title="1 · Where is it">
        <BasicFields v={basic} set={(b) => (setBasic(b), setProbe(null))} />
        <div className="mt-4 flex items-center gap-3">
          <Button onClick={() => probeM.mutate()} busy={probeM.isPending} disabled={!basic.host}>
            Check host key
          </Button>
          <span className="text-xs text-muted">Reads the server&apos;s SSH host key. Nothing is sent to the server yet.</span>
        </div>
        <div className="mt-3">
          <ErrorBanner error={probeM.error} />
        </div>
      </Panel>

      {probe && (
        <Panel title="2 · Is this your server">
          <p className="text-sm text-ink-2">The server presented this {probe.keyType} key. Compare it with the output of this command on the server:</p>
          <CopyBlock className="mt-3" value="ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub" />
          <div className="mt-4 rounded border border-amber/40 bg-amber/5 p-4 font-mono text-sm text-amber-2 break-all">{probe.fingerprint}</div>
          <div className="mt-3">
            <Toggle checked={accepted} onChange={setAccepted} label="The fingerprints match" hint="esa will refuse to connect if this key ever changes." />
          </div>
        </Panel>
      )}

      {probe && accepted && (
        <Panel title="3 · One-time login">
          <div className="grid gap-4 sm:grid-cols-[1fr_2fr]">
            <Field label="User" hint="root, or a user that can use sudo">
              <input className="input font-mono" value={username} onChange={(e) => setUsername(e.target.value)} />
            </Field>
            <Field label="Log in with">
              <div className="flex gap-2">
                {(['key', 'password'] as const).map((k) => (
                  <button
                    key={k}
                    type="button"
                    onClick={() => setAuthKind(k)}
                    className={cx('rounded border px-3 py-2 text-sm', authKind === k ? 'border-amber text-amber' : 'border-line-2 text-ink-2')}
                  >
                    {k === 'key' ? 'Private key' : 'Password'}
                  </button>
                ))}
              </div>
            </Field>
          </div>
          <div className="mt-4 space-y-4">
            {authKind === 'key' ? (
              <>
                <Field label="Private key" hint="Paste the whole key, including the BEGIN and END lines.">
                  <textarea
                    className="input h-32 font-mono text-xs"
                    value={secret}
                    onChange={(e) => setSecret(e.target.value)}
                    spellCheck={false}
                    autoComplete="off"
                    placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                  />
                </Field>
                <Field label="Passphrase" hint="Only if the key is encrypted.">
                  <input className="input" type="password" value={passphrase} onChange={(e) => setPassphrase(e.target.value)} autoComplete="off" />
                </Field>
              </>
            ) : (
              <Field label="Password" hint="Also used once for sudo when the user is not root.">
                <input className="input" type="password" value={secret} onChange={(e) => setSecret(e.target.value)} autoComplete="off" />
              </Field>
            )}
            {isOwner && (
              <Toggle
                checked={keepLogin}
                onChange={setKeepLogin}
                label="Keep this login (encrypted) for helper updates"
                hint="Off is safer: esa then holds only its own key, which can run nothing but the helper."
              />
            )}
          </div>
          <div className="mt-5 flex items-center gap-3">
            <Button variant="primary" onClick={() => addM.mutate()} busy={addM.isPending} disabled={!basic.name || !secret}>
              Install and add
            </Button>
            <span className="text-xs text-muted">Creates the esa user, installs the helper, allows one sudo command.</span>
          </div>
          <div className="mt-3">
            <ErrorBanner error={addM.error} />
          </div>
        </Panel>
      )}
    </div>
  )
}

function CommandFlow() {
  const qc = useQueryClient()
  const [basic, setBasic] = useState<Basic>(emptyBasic)
  const [enrollment, setEnrollment] = useState<Enrollment | null>(null)
  const m = useMutation({
    mutationFn: () =>
      call(
        api().POST('/servers/enrollments', {
          body: { name: basic.name, host: basic.host, port: basic.port, groupId: basic.groupId, hostsControlPlane: basic.hostsControlPlane },
        }),
      ),
    onSuccess: async (e) => {
      setEnrollment(e)
      await qc.invalidateQueries({ queryKey: ['overview'] })
    },
  })

  if (enrollment) {
    return (
      <Panel title="Run this on the server">
        <p className="text-sm text-ink-2">
          As a user with sudo, on <span className="font-mono text-ink">{enrollment.server.host}</span>. The command works once and expires at{' '}
          {new Date(enrollment.expiresAt).toLocaleTimeString()}.
        </p>
        <CopyBlock className="mt-4" value={enrollment.command} />
        <Notice>
          The script creates a system user <span className="font-mono">esa</span>, installs the helper, allows exactly one sudo command, and reports
          the server&apos;s host key back over HTTPS. Read it first with <span className="font-mono">curl -fsSL {enrollment.scriptUrl}</span>.
        </Notice>
        <div className="mt-4">
          <Link to="/app/servers/$serverId" params={{ serverId: enrollment.server.id }} className="text-sm text-amber underline underline-offset-4">
            Open the server page
          </Link>
        </div>
      </Panel>
    )
  }

  return (
    <Panel title="Where is it">
      <BasicFields v={basic} set={setBasic} />
      <div className="mt-5">
        <Button variant="primary" onClick={() => m.mutate()} busy={m.isPending} disabled={!basic.name || !basic.host}>
          Get the command
        </Button>
      </div>
      <div className="mt-3">
        <ErrorBanner error={m.error} />
      </div>
    </Panel>
  )
}
