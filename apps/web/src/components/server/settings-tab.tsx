import { useNavigate } from '@tanstack/react-router'
import {
  useMutation,
  useQuery,
  useQueryClient,
  useSuspenseQuery,
} from '@tanstack/react-query'
import { useState } from 'react'
import { Trash2Icon, WrenchIcon } from 'lucide-react'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { When } from '#/components/time'
import {
  Choice,
  ConfirmDialog,
  ErrorBanner,
  Field,
  KeyValue,
  Notice,
  Panel,
  Toggle,
} from '#/components/kit'
import { Button } from '#/components/ui/button'
import { Input } from '#/components/ui/input'
import { Textarea } from '#/components/ui/textarea'
import { useServerAccess } from '#/components/server/use-access'

export function SettingsTab({ serverId }: { serverId: number }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { data } = useSuspenseQuery(q.server(serverId))
  const s = data.server
  // Owner-only: a server another organization shares is changed by that organization.
  const { canManage: canAct, isOwn, ownerTeam } = useServerAccess(s)
  // Servers go only into this account's own groups; a shared server shows the group it is in.
  const groups = (useQuery(q.groups()).data?.items ?? []).filter(
    (g) => g.access === 'own' || g.id === s.groupId,
  )
  const [name, setName] = useState(s.name)
  const [note, setNote] = useState(s.note)
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
            note,
            protectedContainers: prot
              .split(',')
              .map((p) => p.trim())
              .filter(Boolean),
          },
        }),
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['server', serverId] })
      qc.invalidateQueries({ queryKey: ['overview'] })
    },
  })
  const remove = useMutation({
    mutationFn: () =>
      call(
        api().DELETE('/servers/{serverId}', {
          params: { path: { serverId }, query: { uninstall } },
        }),
      ),
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
          body: reinstallKey
            ? { login: { username: reinstallUser, privateKey: reinstallKey } }
            : {},
        }),
      ),
    onSuccess: () => {
      setReinstallKey('')
      return qc.invalidateQueries({ queryKey: ['server', serverId] })
    },
  })

  return (
    <div className="grid gap-6 xl:grid-cols-2">
      {!isOwn && (
        <Notice className="xl:col-span-2">
          {ownerTeam} owns this server. Only that organization can rename it,
          move it to another group, change protected containers, reinstall the
          helper, accept a new host key, or remove it.
        </Notice>
      )}
      <Panel title="Settings">
        <form
          onSubmit={(e) => {
            e.preventDefault()
            save.mutate()
          }}
        >
          <fieldset disabled={!canAct} className="space-y-4">
            <Field label="Name">
              <Input value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <Field
              label="Note"
              hint="Shown on the board. Kept here, not sent to the server. 200 characters."
            >
              <Textarea
                value={note}
                maxLength={200}
                rows={2}
                onChange={(e) => setNote(e.target.value)}
              />
            </Field>
            <Field
              label="Group"
              hint="The group decides when this server is cleaned and patched."
            >
              <Choice
                disabled={!canAct}
                value={groupId === null ? 'none' : String(groupId)}
                onChange={(v) => setGroupId(v === 'none' ? null : Number(v))}
                options={[
                  { value: 'none', label: 'No group' },
                  ...groups.map((g) => ({
                    value: String(g.id),
                    label: g.name,
                  })),
                ]}
              />
            </Field>
            <Field
              label="Protected containers"
              hint="Name prefixes, comma-separated. Empty uses traefik, authentik, portainer, crowdsec."
            >
              <Input
                className="font-mono"
                autoCapitalize="off"
                spellCheck={false}
                value={prot}
                onChange={(e) => setProt(e.target.value)}
                placeholder="traefik, authentik"
              />
            </Field>
            <Toggle
              checked={cp}
              onChange={setCp}
              disabled={!canAct}
              label="esa runs on this server"
              hint="Never rebooted by a schedule."
            />
            {canAct && (
              <Button
                type="submit"
                busy={save.isPending}
                className="w-full sm:w-auto"
              >
                Save
              </Button>
            )}
            <ErrorBanner error={save.error} />
            {save.isSuccess && <Notice tone="ok">Saved.</Notice>}
          </fieldset>
        </form>
      </Panel>

      <div className="min-w-0 space-y-6">
        <Panel title="Connection">
          <dl className="space-y-2">
            <KeyValue wrap k="SSH" v={`${s.username}@${s.host}:${s.port}`} />
            <KeyValue
              wrap
              k="Host key"
              v={s.hostKeyFingerprint || 'not accepted yet'}
            />
            <KeyValue
              k="Helper"
              v={`${s.helperVersion || '—'} · ${s.arch || '—'}`}
            />
            <KeyValue
              wrap
              k="Helper actions"
              tone={s.helperMissing?.length ? 'warn' : undefined}
              v={
                s.helperMissing?.length
                  ? `missing ${s.helperMissing.join(', ')}`
                  : s.helperCheckedAt
                    ? 'all'
                    : 'not asked yet'
              }
            />
            <KeyValue
              k="Stored login"
              v={s.hasStoredLogin ? 'yes (encrypted)' : 'no'}
            />
            <KeyValue k="Added" v={<When iso={s.createdAt} />} />
          </dl>
        </Panel>

        {canAct && (
          <Panel title="Update or reinstall the helper">
            <p className="mb-4 text-xs text-muted-foreground">
              Needs a sudo login again
              {s.hasStoredLogin
                ? ', or leave the key empty to use the stored one'
                : ''}
              . The esa user itself cannot replace the helper.
            </p>
            <div className="space-y-4">
              <Field label="User">
                <Input
                  className="font-mono"
                  autoCapitalize="off"
                  spellCheck={false}
                  value={reinstallUser}
                  onChange={(e) => setReinstallUser(e.target.value)}
                />
              </Field>
              <Field label="Private key">
                <Textarea
                  className="h-24 font-mono text-xs"
                  value={reinstallKey}
                  onChange={(e) => setReinstallKey(e.target.value)}
                  spellCheck={false}
                  autoComplete="off"
                />
              </Field>
              <Button
                variant="outline"
                onClick={() => reinstall.mutate()}
                busy={reinstall.isPending}
                disabled={!reinstallKey && !s.hasStoredLogin}
              >
                <WrenchIcon /> Reinstall helper
              </Button>
              <ErrorBanner error={reinstall.error} />
              {reinstall.isSuccess && (
                <Notice tone="ok">
                  Helper reinstalled: {reinstall.data.helperVersion}.
                </Notice>
              )}
            </div>
          </Panel>
        )}

        {canAct && (
          <Panel title="Remove" className="border-fail/30">
            <Toggle
              checked={uninstall}
              onChange={setUninstall}
              label="Also remove esa from the server"
              hint="Deletes the esa user, its sudo rule, and the helper."
            />
            <Button
              className="mt-3"
              variant="destructive"
              onClick={() => setDel(true)}
            >
              <Trash2Icon /> Remove server
            </Button>
          </Panel>
        )}
      </div>

      <ConfirmDialog
        open={del}
        title={`Remove ${s.name}`}
        danger
        confirmWord={s.name}
        body={
          uninstall
            ? 'esa removes its user and helper from the server, then forgets the server and its history.'
            : 'esa forgets the server and its history. The helper stays on the server.'
        }
        actionLabel="Remove"
        busy={remove.isPending}
        error={remove.error}
        onConfirm={() => remove.mutate()}
        onClose={() => setDel(false)}
      />
    </div>
  )
}
