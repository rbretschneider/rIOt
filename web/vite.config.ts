import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

const isDemo = process.env.VITE_DEMO === 'true'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: isDemo ? '/rIOt/' : '/',
  resolve: isDemo ? {
    alias: [
      { find: /(.*)\/api\/client$/, replacement: '$1/api/demo-client' },
      { find: /(.*)\/api\/settings$/, replacement: '$1/api/demo-settings' },
      { find: /(.*)\/contexts\/WebSocketProvider$/, replacement: '$1/contexts/DemoWebSocketProvider' },
    ],
  } : undefined,
  server: {
    proxy: {
      '/api': 'http://localhost:7331',
      '/ws': {
        target: 'ws://localhost:7331',
        ws: true,
      },
    },
  },
})
