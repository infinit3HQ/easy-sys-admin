import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { ErrorBanner, Notice, Panel } from '#/components/kit'
import { When } from '#/components/time'
import { Button } from '#/components/ui/button'
import { Input } from '#/components/ui/input'

const tierLabel: Record<number, string> = {
  1: 'Known exploited',
  2: 'High or critical, fix available',
  3: 'Other',
}

/** The newest vulnerability scan, the SBOM download, and the import. */
export function ScanPanel({
  serverId,
  serverName,
  canImport,
}: {
  serverId: number
  serverName: string
  canImport: boolean
}) {
  const qc = useQueryClient()
  const r = useQuery(q.scan(serverId))
  const file = useRef<HTMLInputElement>(null)
  const [showAll, setShowAll] = useState(false)
  const [advisory, setAdvisory] = useState('')
  const [pkg, setPkg] = useState('')
  const [days, setDays] = useState('7')
  const [reason, setReason] = useState('')
  const sbom = useMutation({
    mutationFn: () =>
      call(
        api().GET('/servers/{serverId}/sbom', {
          params: { path: { serverId } },
        }),
      ),
    onSuccess: (data) => {
      const blob = new Blob([JSON.stringify(data, null, 2)], {
        type: 'application/json',
      })
      const a = document.createElement('a')
      a.href = URL.createObjectURL(blob)
      a.download = `${serverName}.cdx.json`
      a.click()
      URL.revokeObjectURL(a.href)
    },
  })
  const upload = useMutation({
    mutationFn: async (f: File) => {
      const body = slimGrype(JSON.parse(await f.text()))
      return call(
        api().POST('/servers/{serverId}/scan', {
          params: { path: { serverId } },
          body,
        }),
      )
    },
    onSuccess: (data) => qc.setQueryData(q.scan(serverId).queryKey, data),
  })
  const except = useMutation({
    mutationFn: () =>
      call(
        api().POST('/servers/{serverId}/vuln-exceptions', {
          params: { path: { serverId } },
          body: {
            advisory: advisory.trim(),
            package: pkg.trim(),
            until: new Date(
              Date.now() + Number(days) * 24 * 60 * 60 * 1000,
            ).toISOString(),
            reason: reason.trim(),
          },
        }),
      ),
    onSuccess: () => {
      setAdvisory('')
      setPkg('')
      setReason('')
      void qc.invalidateQueries({ queryKey: ['scan', serverId] })
    },
  })
  const d = r.data
  const matches = d?.matches ?? []
  const idle = matches.filter((m) => m.notRunningKernel).length
  const shown = showAll ? matches : matches.slice(0, 25)
  return (
    <Panel title="Vulnerability scan" className="xl:col-span-2">
      {d && (
        <Notice tone={d.stale ? 'warn' : 'info'} className="mb-3">
          {d.coverage}
        </Notice>
      )}
      {d?.imported && (
        <p className="mb-2 font-mono text-xs text-faint">
          {d.scanner} {d.scannerVersion} · database{' '}
          {d.dbBuilt ? <When iso={d.dbBuilt} /> : 'age unknown'} · scanned{' '}
          {d.importedAt ? <When iso={d.importedAt} /> : '?'} · {matches.length}{' '}
          matches
          {idle > 0 && (
            <>
              {' '}
              · {idle} from a kernel that is installed but not running
              {d.runningKernel ? ` (${d.runningKernel})` : ''}
            </>
          )}
        </p>
      )}
      {shown.length > 0 && (
        <ul className="space-y-1 font-mono text-xs">
          {shown.map((m) => (
            <li
              key={`${m.package}-${m.id}`}
              className={
                m.tier === 1
                  ? 'text-fail'
                  : m.tier === 2
                    ? 'text-warn'
                    : 'text-ink-2'
              }
            >
              {m.id} · {m.package} {m.version} · {m.severity}
              {m.fixedIn?.length
                ? ` · fixed in ${m.fixedIn.join(', ')}`
                : ` · ${m.fixState}`}
              <span className="text-faint">
                {' '}
                · {tierLabel[m.tier] ?? m.reason}
                {m.notRunningKernel ? ' · not the running kernel' : ''}
                {m.exceptedUntil ? ' · excepted, not a fix' : ''}
              </span>
              {m.exceptedUntil && (
                <>
                  {' '}
                  until <When iso={m.exceptedUntil} />
                </>
              )}
            </li>
          ))}
        </ul>
      )}
      {matches.length > 25 && (
        <button
          type="button"
          className="mt-2 text-xs text-amber"
          onClick={() => setShowAll(!showAll)}
        >
          {showAll ? 'Show fewer' : `Show all ${matches.length}`}
        </button>
      )}
      {canImport && d?.imported && (
        <form
          className="mt-3 flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            except.mutate()
          }}
        >
          <Input
            value={advisory}
            onChange={(e) => setAdvisory(e.target.value)}
            placeholder="CVE-..."
            aria-label="Advisory"
            className="h-8 w-36"
          />
          <Input
            value={pkg}
            onChange={(e) => setPkg(e.target.value)}
            placeholder="package"
            aria-label="Package"
            className="h-8 w-32"
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
            className="h-8 w-36"
          />
          <Button
            type="submit"
            size="sm"
            variant="outline"
            busy={except.isPending}
          >
            Except until then
          </Button>
        </form>
      )}
      <div className="mt-3 flex flex-wrap gap-2">
        <Button
          size="sm"
          variant="outline"
          busy={sbom.isPending}
          onClick={() => sbom.mutate()}
        >
          Download SBOM
        </Button>
        {canImport && (
          <>
            <input
              ref={file}
              type="file"
              accept="application/json,.json"
              className="hidden"
              onChange={(e) => {
                const f = e.target.files?.[0]
                if (f) upload.mutate(f)
                e.target.value = ''
              }}
            />
            <Button
              size="sm"
              variant="outline"
              busy={upload.isPending}
              onClick={() => file.current?.click()}
            >
              Import Grype report
            </Button>
          </>
        )}
      </div>
      <p className="mt-2 text-xs text-muted-foreground">
        Run a scanner on the SBOM wherever you like, for example{' '}
        <code>grype sbom:{serverName}.cdx.json -o json</code>, and import the
        result. esa does not send the SBOM anywhere.
      </p>
      <ErrorBanner
        error={r.error ?? sbom.error ?? upload.error ?? except.error}
        className="mt-2"
      />
    </Panel>
  )
}

type Obj = Record<string, unknown>

function pick(v: unknown, keys: string[]): Obj {
  const src = (v ?? {}) as Obj
  const out: Obj = {}
  for (const k of keys) if (k in src) out[k] = src[k]
  return out
}

/** Keeps only the fields esa reads, so a large Grype report fits the import limit. */
function slimGrype(r: Obj): Obj {
  const matches = Array.isArray(r.matches) ? (r.matches as Obj[]) : []
  return {
    descriptor: pick(r.descriptor, ['name', 'version', 'db']),
    matches: matches.map((m) => ({
      vulnerability: pick(m.vulnerability, [
        'id',
        'severity',
        'fix',
        'knownExploited',
        'epss',
      ]),
      relatedVulnerabilities: Array.isArray(m.relatedVulnerabilities)
        ? (m.relatedVulnerabilities as Obj[]).map((x) => pick(x, ['id']))
        : [],
      artifact: pick(m.artifact, ['name', 'version', 'type']),
    })),
  }
}
