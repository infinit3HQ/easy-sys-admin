// Cross-compiles esa-helper for the server architectures esa supports.
// The control plane uploads these to servers when it installs the helper.
import { execFileSync } from 'node:child_process'

const commit = (() => {
  try {
    return execFileSync('git', ['rev-parse', '--short', 'HEAD']).toString().trim()
  } catch {
    return 'none'
  }
})()
const version = process.env.ESA_VERSION ?? `dev-${commit}`
const pkg = 'github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/version'

for (const arch of ['amd64', 'arm64']) {
  execFileSync(
    'go',
    [
      'build',
      '-trimpath',
      '-ldflags',
      `-s -w -X ${pkg}.Version=${version} -X ${pkg}.Commit=${commit}`,
      '-o',
      `bin/esa-helper-linux-${arch}`,
      './cmd/esa-helper',
    ],
    { stdio: 'inherit', env: { ...process.env, GOOS: 'linux', GOARCH: arch, CGO_ENABLED: '0' } },
  )
  console.log(`built bin/esa-helper-linux-${arch} (${version})`)
}
