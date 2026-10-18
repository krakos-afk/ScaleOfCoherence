import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: Object.fromEntries(['/auth', '/teams', '/items', '/workflows'].map((p) => [p, 'http://localhost:8080'])),
  },
})
