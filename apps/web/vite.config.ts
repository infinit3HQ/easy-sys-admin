import { defineConfig } from 'vite'
import { devtools } from '@tanstack/devtools-vite'
import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import viteReact from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { nitro } from 'nitro/vite'

export default defineConfig({
  resolve: { tsconfigPaths: true },
  server: { port: Number(process.env.WEB_PORT ?? 3000), strictPort: true },
  plugins: [devtools(), nitro(), tailwindcss(), tanstackStart(), viteReact()],
})
