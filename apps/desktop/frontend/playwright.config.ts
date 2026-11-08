import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  workers: 1,
  timeout: 60_000,
  expect: { timeout: 12_000 },
  use: { baseURL: process.env.OVPN_E2E_DESK_URL, actionTimeout: 12_000, viewport: { width: 1280, height: 900 } },
});
