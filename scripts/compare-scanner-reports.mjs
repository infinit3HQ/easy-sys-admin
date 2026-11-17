import { readFileSync } from 'node:fs'

const [grypePath, trivyPath] = process.argv.slice(2)
if (!grypePath || !trivyPath) {
  throw new Error(
    'usage: node scripts/compare-scanner-reports.mjs grype.json trivy.json',
  )
}
const grype = JSON.parse(readFileSync(grypePath, 'utf8'))
const trivy = JSON.parse(readFileSync(trivyPath, 'utf8'))
function counts(pairs) {
  const unique = new Set(pairs.map(([pkg, id]) => `${pkg}\0${id}`))
  const byPackage = {}
  for (const key of unique) {
    const pkg = key.split('\0')[0]
    byPackage[pkg] = (byPackage[pkg] ?? 0) + 1
  }
  return { matches: unique.size, byPackage }
}
const result = {
  fixture: 'Same SBOM input. Counts do not measure scanner accuracy.',
  grype: {
    version: grype.descriptor?.version,
    databaseBuilt:
      grype.descriptor?.db?.status?.built ?? grype.descriptor?.db?.built,
    ...counts(grype.matches.map((m) => [m.artifact.name, m.vulnerability.id])),
  },
  trivy: {
    version: 'Read with trivy --version',
    ...counts(
      (trivy.Results ?? []).flatMap((r) =>
        (r.Vulnerabilities ?? []).map((v) => [v.PkgName, v.VulnerabilityID]),
      ),
    ),
  },
}
console.log(JSON.stringify(result, null, 2))
