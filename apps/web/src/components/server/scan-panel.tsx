import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { ErrorBanner, Notice, Panel } from '#/components/kit'
import { When } from '#/components/time'
import { Button } from '#/components/ui/button'

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
      const body = JSON.parse(await f.text()) as Record<string, unknown>
      return call(
        api().POST('/servers/{serverId}/scan', {
          params: { path: { serverId } },
          body,
        }),
      )
    },
    onSuccess: (data) => qc.setQueryData(q.scan(serverId).queryKey, data),
  })
  const d = r.data
  const matches = d?.matches ?? []
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
              </span>
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
        error={r.error ?? sbom.error ?? upload.error}
        className="mt-2"
      />
    </Panel>
  )
}
