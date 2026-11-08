import { expect, test } from '@playwright/test';

test('an operator browses real inventory and checks only eligible identities', async ({ page }) => {
  await page.goto('/');
  const nav = page.getByRole('navigation');
  await nav.getByRole('button', { name: 'Users', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Open user alice', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Open user expired-51' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Load more' }).click();
  await expect(page.getByRole('button', { name: 'Open user expired-51' })).toBeVisible();
  await page.getByRole('button', { name: 'Open user alice', exact: true }).click();
  await expect(page.getByRole('dialog').getByText('Nodes using agent quota')).toBeVisible();
  await page.getByRole('dialog').getByRole('button', { name: 'Close' }).click();

  await nav.getByRole('button', { name: 'Checks', exact: true }).click();
  const server = page.getByLabel('Choose a server');
  const user = page.getByRole('combobox', { name: 'User', exact: true });
  await server.selectOption({ label: 'Primary fixture' });
  await expect(user.locator('option:not([value=""])')).toHaveText(['alice']);
  await user.selectOption({ label: 'alice' });
  await server.selectOption({ label: 'Backup fixture' });
  await expect(user.locator('option:not([value=""])')).toHaveText(['bob']);
  await user.selectOption({ label: 'bob' });
  await page.getByRole('button', { name: 'Run HTTPS check' }).click();
  await expect(page.getByText('Failed', { exact: true })).toBeVisible({ timeout: 35_000 });
});

test('a schedule is saved paused, requires consent, and cannot run without it', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('navigation').getByRole('button', { name: 'Monitoring' }).click();
  await page.getByRole('button', { name: 'New policy' }).click();
  await page.getByLabel('Policy name').fill('Limited fixture');
  await page.getByRole('combobox', { name: 'Server', exact: true }).selectOption({ label: 'Primary fixture' });
  await page.getByRole('combobox', { name: 'User', exact: true }).selectOption({ label: 'alice' });
  await page.getByRole('button', { name: 'Add target' }).click();
  await page.getByLabel('Checks per target/source per UTC day').fill('1');
  await expect(page.getByRole('button', { name: 'Enable schedule' })).toBeDisabled();
  const policy = page.getByRole('row', { name: /Limited fixture/ });
  await page.getByRole('button', { name: 'Save paused' }).click();
  await expect(policy.getByText('Paused')).toBeVisible();

  await page.reload();
  await page.getByRole('navigation').getByRole('button', { name: 'Monitoring' }).click();
  await expect(policy.getByText('Paused')).toBeVisible();
  await expect(policy.getByRole('button', { name: 'Enable', exact: true })).toBeDisabled();
  await expect(policy.getByRole('button', { name: 'Run once' })).toBeDisabled();
  await policy.getByRole('button', { name: 'Edit', exact: true }).click();
  await page.getByRole('checkbox', { name: /I authorize checks/ }).check();
  await page.getByRole('button', { name: 'Save paused' }).click();
  await policy.getByRole('button', { name: 'Run once' }).click();
  await expect(page.getByRole('row', { name: /Native Xray · Manual/ }).getByText('Completed', { exact: true })).toBeVisible({ timeout: 35_000 });
  await policy.getByRole('button', { name: 'Remove policy Limited fixture' }).click();
  await expect(policy).toHaveCount(0);
});

test('a review becomes stale if access changes outside Desk', async ({ page, request }) => {
  await page.goto('/');
  await page.getByRole('navigation').getByRole('button', { name: 'Users', exact: true }).click();
  await page.getByRole('button', { name: 'Open user alice', exact: true }).click();
  await page.getByRole('button', { name: 'Review access change' }).click();
  const review = page.getByRole('dialog');
  await expect(review.getByRole('heading', { name: 'Disable access · alice' })).toBeVisible();
  await expect(review.getByRole('button', { name: 'Recheck snapshot' })).toBeVisible();
  await review.getByRole('button', { name: 'Recheck snapshot' }).click();
  await expect(review.getByText(/Local snapshot unchanged/)).toBeVisible();
  expect((await request.post('/fixture/disable-alice')).status()).toBe(204);
  await review.getByRole('button', { name: 'Recheck snapshot' }).click();
  await expect(review.getByRole('button', { name: 'Rebuild plan' })).toBeVisible();
});

test('a new user plan previews every node without adding an identity', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('navigation').getByRole('button', { name: 'Users', exact: true }).click();
  await page.getByRole('button', { name: 'Preview new user' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('textbox', { name: 'Username' }).fill('preview-only');
  await dialog.getByRole('button', { name: 'Review plan' }).click();
  await expect(dialog.getByText('Enabled servers affected: 2')).toBeVisible();
  await expect(dialog.getByText('Review only · nothing applied')).toBeVisible();
  await dialog.getByRole('button', { name: 'Close' }).click();
  await page.getByRole('button', { name: 'Refresh inventory' }).click();
  await expect(page.getByRole('button', { name: 'Open user alice' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Open user preview-only' })).toHaveCount(0);
});

test('settings persist and an uninstalled Knife cannot run diagnostics', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Appearance').selectOption({ label: 'Light' });
  await dialog.getByLabel('Language').selectOption('ru');
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await page.getByRole('button', { name: 'Настройки', exact: true }).click();
  await dialog.getByLabel('Язык').selectOption('en');
  await dialog.getByLabel('Close', { exact: true }).click();
  await page.getByRole('navigation').getByRole('button', { name: 'Integrations' }).click();
  for (const name of ['Xray Checker', 'Xray Knife']) {
    const card = page.getByRole('article').filter({ has: page.getByRole('heading', { name, exact: true }) });
    await expect(card.getByText('Not installed', { exact: true })).toBeVisible();
  }
  await expect(page.getByRole('button', { name: 'Run diagnostic', exact: true })).toBeDisabled();
  await expect(page.getByRole('row', { name: /· Diagnostic/ })).toHaveCount(0);
});
