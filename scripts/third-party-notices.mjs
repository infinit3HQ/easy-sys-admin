// Lists npm and Go module licenses and fails on licenses the project will not ship.
// Forbidden: GPL-2.0-only (not GPL-2.0-or-later), SSPL, BUSL/BSL, Elastic License,
// Commons Clause, non-commercial terms, and a dependency with no license.
// An OR expression passes when one side is allowed. An AND expression fails when any side is forbidden.

import { execFileSync } from 'node:child_process'
import { writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const ownNpm = new Set(['easy-sys-admin'])
const ownGoPrefix = 'github.com/nirajdilshan/easy-sys-admin'

const forbiddenAtom = [
  /^GPL-2\.0-only$/i,
  /^GPL-2\.0$/i,
  /^SSPL/i,
  /^BUSL/i,
  /^BSL-1\./i,
  /^Elastic-2/i,
  /^ELv2$/i,
  /Elastic ?License/i,
  /Commons[- ]Clause/i,
  /non-?commercial/i,
  /^CC-BY-NC/i,
  /^PolyForm-Noncommercial/i,
  /^UNLICENSED$/i,
  /^UNKNOWN$/i,
  /^NOASSERTION$/i,
  /^NONE$/i,
]

function splitTop(input, word) {
  const parts = []
  let depth = 0
  let start = 0
  const upper = input.toUpperCase()
  const needle = ` ${word} `
  for (let i = 0; i < input.length; i++) {
    const ch = input[i]
    if (ch === '(') depth++
    else if (ch === ')') depth = Math.max(0, depth - 1)
    else if (depth === 0 && upper.startsWith(needle, i)) {
      parts.push(input.slice(start, i).trim())
      i += needle.length - 1
      start = i + 1
    }
  }
  parts.push(input.slice(start).trim())
  return parts.filter(Boolean)
}

/** @returns {{ ok: boolean, reason: string }} */
function checkLicense(expression) {
  const text = String(expression ?? '').trim()
  if (!text) return { ok: false, reason: 'unlicensed' }
  const ors = splitTop(text, 'OR')
  if (ors.length > 1) {
    const results = ors.map(checkLicense)
    if (results.some((item) => item.ok)) return { ok: true, reason: '' }
    return {
      ok: false,
      reason: results.map((item) => item.reason).join(' OR '),
    }
  }
  const ands = splitTop(text, 'AND')
  if (ands.length > 1) {
    for (const part of ands) {
      const result = checkLicense(part)
      if (!result.ok) return result
    }
    return { ok: true, reason: '' }
  }
  const withParts = splitTop(text, 'WITH')
  if (withParts.length > 1) {
    for (const part of withParts) {
      const result = checkLicense(part)
      if (!result.ok) return result
    }
    return { ok: true, reason: '' }
  }
  const atom = text.replace(/^\(+|\)+$/g, '').trim()
  if (!atom) return { ok: false, reason: 'unlicensed' }
  if (atom !== text) return checkLicense(atom)
  for (const pattern of forbiddenAtom) {
    if (pattern.test(atom)) return { ok: false, reason: atom }
  }
  return { ok: true, reason: '' }
}

/** @param {string} text */
function jsonObjects(text) {
  /** @type {Array<Record<string, unknown>>} */
  const items = []
  let i = 0
  while (i < text.length) {
    while (i < text.length && /\s/.test(text[i] ?? '')) i++
    if (i >= text.length) break
    if (text[i] !== '{') throw new Error('expected a JSON object from go list')
    let depth = 0
    let inString = false
    let escaped = false
    const start = i
    for (; i < text.length; i++) {
      const ch = text[i]
      if (inString) {
        if (escaped) escaped = false
        else if (ch === '\\') escaped = true
        else if (ch === '"') inString = false
        continue
      }
      if (ch === '"') inString = true
      else if (ch === '{') depth++
      else if (ch === '}') {
        depth--
        if (depth === 0) {
          items.push(JSON.parse(text.slice(start, i + 1)))
          i++
          break
        }
      }
    }
  }
  return items
}

function npmPackages() {
  const raw = execFileSync(
    'pnpm',
    ['licenses', 'list', '--recursive', '--json'],
    {
      cwd: root,
      encoding: 'utf8',
      maxBuffer: 64 * 1024 * 1024,
    },
  )
  const parsed = JSON.parse(raw)
  /** @type {Array<{ name: string, version: string, license: string }>} */
  const rows = []
  const push = (name, version, license) => {
    if (!name) return
    rows.push({
      name,
      version: version || '',
      license: license || '',
    })
  }
  if (Array.isArray(parsed)) {
    for (const item of parsed) {
      push(item.name, item.versions?.[0] ?? item.version, item.license)
    }
  } else {
    for (const [license, items] of Object.entries(parsed)) {
      for (const item of items) {
        const versions = item.versions ?? [item.version].filter(Boolean)
        if (versions.length === 0) push(item.name, '', item.license ?? license)
        for (const version of versions)
          push(item.name, version, item.license ?? license)
      }
    }
  }
  const seen = new Set()
  return rows.filter((row) => {
    const key = `${row.name}@${row.version}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

function goModules() {
  const binDir = resolve(root, 'node_modules/.bin')
  // Install the checker outside the Go module so go.mod is not edited.
  // Install outside the workspace so go.work.sum is not rewritten.
  execFileSync('go', ['install', 'github.com/google/go-licenses@v1.6.0'], {
    cwd: '/tmp',
    env: { ...process.env, GO111MODULE: 'on', GOBIN: binDir },
    stdio: 'inherit',
  })
  let report = ''
  try {
    report = execFileSync(resolve(binDir, 'go-licenses'), ['report', './...'], {
      cwd: resolve(root, 'apps/esa'),
      encoding: 'utf8',
      maxBuffer: 64 * 1024 * 1024,
      stdio: ['ignore', 'pipe', 'pipe'],
    })
  } catch (error) {
    const stdout = error.stdout?.toString() ?? ''
    const stderr = error.stderr?.toString() ?? ''
    if (!stdout.trim()) {
      throw new Error(`go-licenses failed:\n${stderr}`)
    }
    process.stderr.write(stderr)
    report = stdout
  }
  /** @type {Array<{ path: string, version: string }>} */
  const modules = []
  const listed = execFileSync('go', ['list', '-m', '-json', 'all'], {
    cwd: resolve(root, 'apps/esa'),
    encoding: 'utf8',
  })
  for (const item of jsonObjects(listed)) {
    if (item.Path)
      modules.push({ path: item.Path, version: item.Version ?? '' })
  }
  modules.sort((a, b) => b.path.length - a.path.length)
  const versionOf = (pkg) => {
    const match = modules.find(
      (mod) => pkg === mod.path || pkg.startsWith(`${mod.path}/`),
    )
    return match?.version ?? ''
  }
  /** @type {Array<{ name: string, version: string, license: string }>} */
  const rows = []
  const seen = new Set()
  for (const line of report.split('\n')) {
    if (!line.trim()) continue
    const [name, , license] = line.split(',').map((part) => part.trim())
    if (!name || name.startsWith(ownGoPrefix) || seen.has(name)) continue
    seen.add(name)
    rows.push({ name, version: versionOf(name), license: license || '' })
  }
  return rows
}

function section(title, rows) {
  const lines = [
    `## ${title}`,
    '',
    '| Package | Version | License |',
    '| --- | --- | --- |',
  ]
  const sorted = [...rows].sort(
    (a, b) =>
      a.name.localeCompare(b.name) || a.version.localeCompare(b.version),
  )
  for (const row of sorted) {
    const license = row.license.replace(/\|/g, '\\|') || '(none)'
    lines.push(`| ${row.name} | ${row.version} | ${license} |`)
  }
  lines.push('')
  return lines
}

const npm = npmPackages().filter(
  (row) => !ownNpm.has(row.name) && !row.name.startsWith('@esa/'),
)
const go = goModules()
const violations = []
for (const [kind, rows] of [
  ['npm', npm],
  ['Go', go],
]) {
  for (const row of rows) {
    if (row.name.startsWith(ownGoPrefix)) continue
    const result = checkLicense(row.license)
    if (!result.ok) violations.push({ kind, ...row, reason: result.reason })
  }
}

const body = [
  '# Third-party notices',
  '',
  'Generated by `node scripts/third-party-notices.mjs`. Do not edit this file by hand.',
  '',
  'The project itself is AGPL-3.0-only, copyright 2026 Niraj Dilshan and contributors. This file lists other packages it builds with.',
  '',
  ...section('npm packages', npm),
  ...section('Go modules', go),
]
if (violations.length > 0) {
  body.push('## License check failures', '')
  for (const item of violations) {
    body.push(
      `- ${item.kind}: ${item.name}@${item.version || '?'} license ${item.license || '(none)'} (${item.reason})`,
    )
  }
  body.push('')
}
writeFileSync(resolve(root, 'THIRD_PARTY_NOTICES.md'), `${body.join('\n')}\n`)

if (violations.length > 0) {
  console.error(`\n${violations.length} forbidden or unlicensed dependencies:`)
  for (const item of violations) {
    console.error(
      `  ${item.kind}: ${item.name}@${item.version || '?'} ${item.license || '(none)'}`,
    )
  }
  process.exit(1)
}
console.log(
  `Wrote THIRD_PARTY_NOTICES.md (${npm.length} npm packages, ${go.length} Go modules).`,
)
