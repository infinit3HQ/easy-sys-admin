// Validate deployment isolation without connecting to a Docker daemon.
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'

const base = Object.fromEntries(
  Object.entries(process.env).filter(
    ([key]) => !key.startsWith('ESA_') && key !== 'POSTGRES_PASSWORD',
  ),
)
Object.assign(base, {
  ESA_HOST: 'test.example.com',
  ESA_PUBLIC_URL: 'https://test.example.com',
  ESA_MASTER_KEY: `${'A'.repeat(43)}=`,
  POSTGRES_PASSWORD: 'compose-validation-only',
})
for (const channel of ['source', 'images', 'nightly']) {
  const env = { ...base }
  const args = [
    'compose',
    '--env-file',
    '/dev/null',
    '-f',
    'deploy/compose.yaml',
  ]
  if (channel !== 'source') {
    args.push('-f', 'deploy/compose.images.yaml')
    env.ESA_CONTROL_PLANE_IMAGE = `ghcr.io/example/control@sha256:${'a'.repeat(64)}`
    env.ESA_WEB_IMAGE = `ghcr.io/example/web@sha256:${'b'.repeat(64)}`
  }
  if (channel === 'nightly') {
    args.push('-f', 'deploy/compose.nightly.yaml')
    Object.assign(env, {
      ESA_STACK_NAME: 'esa-nightly',
      ESA_ROUTER: 'esa-nightly',
      ESA_HOST: 'nightly.example.com',
    })
  }
  const config = JSON.parse(
    execFileSync('docker', [...args, 'config', '--format', 'json'], {
      env,
      encoding: 'utf8',
    }),
  )
  for (const service of Object.values(config.services))
    assert.equal(service.ports?.length ?? 0, 0)
  for (const name of ['esa', 'web'])
    assert.equal('build' in config.services[name], channel === 'source')
  assert.equal(config.networks.database.internal, true)
  assert.equal('proxy' in config.services.esa.networks, false)
  assert.equal('proxy' in config.services.postgres.networks, false)
  if (channel === 'nightly') {
    assert.equal(config.name, 'esa-nightly')
    assert.equal(config.volumes.postgres.name, 'esa-nightly_postgres')
    assert.equal(config.networks.database.name, 'esa-nightly_database')
    assert.equal(config.networks.internal.name, 'esa-nightly_internal')
    for (const service of Object.values(config.services)) {
      assert.equal(Number(service.mem_limit), 512 * 1024 * 1024)
      assert.equal(Number(service.cpus), 0.75)
      assert.equal(Number(service.pids_limit), 256)
    }
    assert.equal(
      config.services.web.labels['traefik.http.routers.esa-nightly.rule'],
      'Host(`nightly.example.com`)',
    )
  }
  console.log(
    `${channel}: build mode, private ports, networks, volumes, and resource limits verified`,
  )
}
