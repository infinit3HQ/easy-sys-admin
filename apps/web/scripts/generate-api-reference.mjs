import { readFileSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { parse } from 'yaml'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../../..')
const spec = parse(readFileSync(resolve(root, 'packages/api/openapi.yaml'), 'utf8'))
const methods = ['get', 'post', 'put', 'patch', 'delete']

const tagOrder = []
const tagText = new Map()
for (const tag of spec.tags ?? []) {
  const name = typeof tag === 'string' ? tag : tag.name
  tagOrder.push(name)
  const description = typeof tag === 'string' ? '' : (tag.description ?? '')
  tagText.set(name, description.replace(/\s+/g, ' ').trim())
}

/** @type {Map<string, Array<{ method: string, path: string, summary: string, auth: string }>>} */
const groups = new Map()
for (const [path, item] of Object.entries(spec.paths ?? {})) {
  for (const method of methods) {
    const op = item?.[method]
    if (!op) continue
    const tag = op.tags?.[0] ?? 'other'
    if (!groups.has(tag)) groups.set(tag, [])
    const auth = Array.isArray(op.security) && op.security.length === 0 ? 'Public' : 'Session or token'
    groups.get(tag).push({
      method: method.toUpperCase(),
      path,
      summary: String(op.summary ?? op.operationId ?? '').replace(/\|/g, '\\|'),
      auth,
    })
  }
}

const ordered = [
  ...tagOrder.filter((tag) => groups.has(tag)),
  ...[...groups.keys()].filter((tag) => !tagOrder.includes(tag)),
]

function cell(value) {
  return value.replace(/\|/g, '\\|')
}

const lines = [
  '---',
  'title: API reference',
  'description: HTTP API of the control plane, generated from packages/api/openapi.yaml.',
  '---',
  '',
  'The web app, the `esa` CLI, and the MCP endpoint all call this API. The base path is `/api/v1`. The contract file in the source tree is `packages/api/openapi.yaml`. This page is generated from that file. Do not edit it by hand. Run `pnpm --filter @esa/web generate:docs` after the contract changes.',
  '',
  'A browser sends the session cookie set by `/auth/callback`. The CLI and other clients send `Authorization: Bearer esa_...` with a token from Settings, API tokens. A token can do at most what its user can do, and only what its scopes allow (`read`, `logs:read`, `actions`, `admin`).',
  '',
  'Every error has the shape `{"error": {"code": "...", "message": "..."}}`. Codes include `bad_request` (400), `unauthorized` (401), `forbidden` (403), `not_found` (404), `conflict` (409), and `unavailable` (502).',
  '',
  'Paths marked Public do not need a session or a token.',
  '',
]

for (const tag of ordered) {
  const title = tag.replace(/-/g, ' ')
  lines.push(`## ${title}`, '')
  const about = tagText.get(tag)
  if (about) lines.push(about, '')
  lines.push('| Method | Path | Auth | Summary |', '| --- | --- | --- | --- |')
  for (const op of groups.get(tag)) {
    lines.push(`| ${cell(op.method)} | \`${cell(op.path)}\` | ${cell(op.auth)} | ${cell(op.summary)} |`)
  }
  lines.push('')
}

writeFileSync(resolve(root, 'apps/web/src/content/docs/api.md'), `${lines.join('\n')}\n`)
