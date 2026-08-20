import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import { resolve } from 'path'

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'happy-dom',
    setupFiles: ['./src/test/setup.ts'],
    globals: true,
    // Memory optimization for CI environments
    pool: 'forks',
    poolOptions: {
      forks: {
        singleFork: true,
        isolate: false,
      },
    },
    // Reduce parallel execution to prevent memory pressure
    maxWorkers: 1,
    minWorkers: 1,
    // Set reasonable timeouts
    testTimeout: 10000,
    hookTimeout: 10000,
    // Exclude E2E tests from unit tests
    exclude: [
      '**/node_modules/**',
      '**/dist/**',
      '**/cypress/**',
      '**/e2e/**',
      '**/.{idea,git,cache,output,temp}/**',
      '**/{karma,rollup,webpack,vite,vitest,jest,ava,babel,nyc,cypress,tsup,build}.config.*'
    ],
  },
  resolve: {
    alias: {
      '@': resolve(__dirname, './src'),
    },
  },
})