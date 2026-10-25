import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  testMatch: ['base-path.spec.ts'],
  timeout: 30_000,
  fullyParallel: false,
  workers: 1,
  reporter: [['list']],
  use: {
    baseURL: 'http://localhost:8083',
    trace: 'on-first-retry',
    actionTimeout: 10_000,
  },
  webServer: {
    command: 'go run ../cmd/freedius --config ../e2e/fixtures/test-config.yaml --no-export-hint --ui-path /freedius',
    url: 'http://localhost:8083/freedius/health',
    reuseExistingServer: false,
    timeout: 30_000,
    stdout: 'pipe',
    stderr: 'pipe',
  },
});
