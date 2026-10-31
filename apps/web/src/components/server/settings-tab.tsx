import { useNavigate } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import type { ReactNode } from 'react'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { When } from '#/components/time'
import { Button, ConfirmDialog, ErrorBanner, Field, Notice, Panel, Toggle } from '#/components/ui'
import { useRole } from '#/routes/app/route'

export function SettingsTab({ serverId }: { serverId: number }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { canAct } = useRole()
  const { data } = useSuspenseQuery(q.server(serverId))
  const s = data.server
  const groups = useQuery(q.groups()).data?.items ?? []
  const [name, setName] = useState(s.name)
  const [groupId, setGroupId] = useState<number | null>(s.groupId ?? null)
  const [cp, setCp] = useState(s.hostsControlPlane)
  const [prot, setProt] = useState(s.protectedContainers.join(', '))
  const [del, setDel] = useState(false)
  const [uninstall, setUninstall] = useState(true)
  const [reinstallUser, setReinstallUser] = useState('root')
  const [reinstallKey, setReinstallKey] = useState('')

  const save = useMutation({
    mutationFn: () =>
      call(
        api().PATCH('/servers/{serverId}', {
          params: { path: { serverId } },
          body: {
            name,
            groupId: groupId ?? undefined,
            clearGroup: groupId === null,
            hostsControlPlane: cp,
            protectedContainers: prot.split(',').map((p) => p.trim()).filter(Boolean),
          },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['server', serverId] }),
  })
  const remove = useMutation({
    mutationFn: () => call(api().DELETE('/servers/{serverId}', { params: { path: { serverId }, query: { uninstall } } })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['overview'] })
      await navigate({ to: '/app' })
    },
  })
  const reinstall = useMutation({
    mutationFn: () =>
      call(
        api().POST('/servers/{serverId}/helper', {
          params: { path: { serverId } },
          body: reinstallKey ? { login: { username: reinstallUser, privateKey: reinstallKey } } : {},
        }),
      ),
    onSuccess: () => {
      setReinstallKey('')
      return qc.invalidateQueries({ queryKey: ['server', serverId] })
    },
  })

  return (
    <div className="grid gap-6 xl:grid-cols-2">
      <Panel title="Settings">
        <fieldset disabled={!canAct} className="space-y-4">
          <Field label="Name">
            <input className="input" value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label="Group" hint="The group decides when this server is cleaned and patched.">
            <select className="input" value={groupId ?? ''} onChange={(e) => setGroupId(e.target.value ? Number(e.target.value) : null)}>
              <option value="">No group</option>
              {groups.map((g) => (
                <option key={g.id} value={g.id}>
                  {g.name}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Protected containers" hint="Name prefixes, comma-separated. Empty uses traefik, authentik, portainer, crowdsec.">
            <input className="input font-mono" value={prot} onChange={(e) => setProt(e.target.value)} placeholder="traefik, authentik" />
          </Field>
          <Toggle checked={cp} onChange={setCp} label="esa runs on this server" hint="Never rebooted by a schedule." />
          <Button variant="primary" onClick={() => save.mutate()} busy={save.isPending}>
            Save
          </Button>
          <ErrorBanner error={save.error} />
          {save.isSuccess && <Notice tone="ok">Saved.</Notice>}
        </fieldset>
      </Panel>

      <div className="space-y-6">
        <Panel title="Connection">
          <dl className="space-y-2 font-mono text-xs">
            <Row k="SSH" v={`${s.username}@${s.host}:${s.port}`} />
            <Row k="Host key" v={s.hostKeyFingerprint || 'not accepted yet'} />
            <Row k="Helper" v={`${s.helperVersion || '—'} · ${s.arch || '—'}`} />
            <Row k="Stored login" v={s.hasStoredLogin ? 'yes (encrypted)' : 'no'} />
            <Row k="Added" v={<When iso={s.createdAt} />} />
          </dl>
        </Panel>

        {canAct && (
          <Panel title="Update or reinstall the helper">
            <p className="mb-3 text-xs text-muted">
              Needs a sudo login again{s.hasStoredLogin ? ', or leave the key empty to use the stored one' : ''}. The esa user itself cannot replace the helper.
            </p>
            <div className="space-y-3">
              <Field label="User">
                <input className="input font-mono" value={reinstallUser} onChange={(e) => setReinstallUser(e.target.value)} />
              </Field>
              <Field label="Private key">
                <textarea className="input h-24 font-mono text-xs" value={reinstallKey} onChange={(e) => setReinstallKey(e.target.value)} spellCheck={false} />
              </Field>
              <Button onClick={() => reinstall.mutate()} busy={reinstall.isPending} disabled={!reinstallKey && !s.hasStoredLogin}>
                Reinstall helper
              </Button>
              <ErrorBanner error={reinstall.error} />
              {reinstall.isSuccess && <Notice tone="ok">Helper reinstalled: {reinstall.data.helperVersion}.</Notice>}
            </div>
          </Panel>
        )}

        {canAct && (
          <Panel title="Remove">
            <Toggle checked={uninstall} onChange={setUninstall} label="Also remove esa from the server" hint="Deletes the esa user, its sudo rule, and the helper." />
            <Button className="mt-3" variant="danger" onClick={() => setDel(true)}>
              Remove server
            </Button>
          </Panel>
        )}
      </div>

      <ConfirmDialog
        open={del}
        title={`Remove ${s.name}`}
        danger
        confirmWord={s.name}
        body={uninstall ? 'esa removes its user and helper from the server, then forgets the server and its history.' : 'esa forgets the server and its history. The helper stays on the server.'}
        actionLabel="Remove"
        busy={remove.isPending}
        error={remove.error}
        onConfirm={() => remove.mutate()}
        onClose={() => setDel(false)}
      />
    </div>
  )
}

function Row({ k, v }: { k: string; v: ReactNode }) {
  return (
    <div className="flex justify-between gap-4">
      <dt className="text-muted">{k}</dt>
      <dd className="break-all text-right text-ink-2">{v}</dd>
    </div>
  )
}
