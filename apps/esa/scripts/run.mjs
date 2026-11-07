// `node --run` starts the package script without the variables loaded by
// `--env-file`, so `go run` never sees `.env`. Run the command in this process.
import { spawn } from 'node:child_process'

const [command, ...args] = process.argv.slice(2)
if (!command) {
  console.error('usage: run.mjs <command> [args...]')
  process.exit(1)
}

const child = spawn(command, args, { stdio: 'inherit' })
child.on('exit', (code, signal) => {
  if (signal) process.kill(process.pid, signal)
  else process.exit(code ?? 1)
})
