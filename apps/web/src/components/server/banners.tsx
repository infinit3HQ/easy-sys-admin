import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { KeyRoundIcon, ShieldAlertIcon, TerminalIcon } from 'lucide-react'
import type { Enrollment, Server } from '@esa/api'
import { api, call } from '#/lib/api'
import { CopyBlock, ErrorBanner } from '#/components/kit'
import { Button } from '#/components/ui/button'

export function HostKeyAlert({ server }: { server: Server }) {
  const qc = useQueryClient()
  const m = useMutation({
    mutationFn: () =>
      call(
        api().POST('/servers/{serverId}/host-key', {
          params: { path: { serverId: server.id } },
          body: { fingerprint: server.presentedFingerprint },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['server', server.id] }),
  })
  return (
    <section role="alert" className="mb-6 space-y-4 rounded-md border border-fail bg-fail/10 p-4 sm:p-5">
      <h2 className="flex items-start gap-2 font-display text-2xl leading-none font-bold text-fail uppercase">
        <ShieldAlertIcon className="mt-0.5 size-5 shrink-0" aria-hidden />
        Host key changed · all work paused
      </h2>
      <p className="text-sm text-ink-2">
        The server presented a different SSH host key than the one you accepted. That happens after a reinstall, and also when someone is intercepting the
        connection. Nothing runs on this server until you accept the new key.
      </p>
      <dl className="grid gap-x-4 gap-y-1 font-mono text-xs sm:grid-cols-[120px_1fr]">
        <dt className="text-muted-foreground">Accepted</dt>
        <dd className="mb-2 break-all text-ink-2 sm:mb-0">{server.hostKeyFingerprint}</dd>
        <dt className="text-muted-foreground">Presented</dt>
        <dd className="break-all text-fail">{server.presentedFingerprint}</dd>
      </dl>
      <p className="text-sm text-ink-2">Check the new key on the server console before you accept it:</p>
      <CopyBlock value="ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub" />
      <Button variant="destructive" onClick={() => m.mutate()} busy={m.isPending} className="w-full sm:w-auto">
        <KeyRoundIcon /> I checked it, accept the new key
      </Button>
      <ErrorBanner error={m.error} />
    </section>
  )
}

export function PendingEnrollment({ server }: { server: Server }) {
  const [enrollment, setEnrollment] = useState<Enrollment | null>(null)
  const m = useMutation({
    mutationFn: () =>
      call(
        api().POST('/servers/{serverId}/enrollment', {
          params: { path: { serverId: server.id } },
        }),
      ),
    onSuccess: setEnrollment,
  })
  return (
    <section className="mb-6 space-y-4 rounded-md border border-amber/50 bg-amber/5 p-4 sm:p-5">
      <div>
        <h2 className="font-display text-2xl leading-none font-bold text-amber uppercase">Waiting for enrollment</h2>
        <p className="mt-2 text-sm text-ink-2">Run the enroll command on the server. If it expired, get a new one.</p>
      </div>
      {enrollment ? (
        <CopyBlock value={enrollment.command} />
      ) : (
        <Button variant="outline" onClick={() => m.mutate()} busy={m.isPending}>
          <TerminalIcon /> New enroll command
        </Button>
      )}
      <ErrorBanner error={m.error} />
    </section>
  )
}
