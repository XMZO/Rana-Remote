const { test, expect } = require('@playwright/test');

test('login and audit export minimal loop', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('textbox').fill('admin');
  await page.locator('input[type="password"]').fill('password');
  await page.getByRole('button', { name: /login/i }).click();
  await expect(page.locator('#dashboard')).toHaveText(/dashboard/);
  await expect(page.locator('#audit-export')).toHaveText(/audit-export-ok/);
});
