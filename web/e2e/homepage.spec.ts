import { test, expect } from '@playwright/test'

test('homepage loads', async ({ page }) => {
  await page.goto('/')

  // Verify the page title
  await expect(page).toHaveTitle('TKiller')

  // Verify the root element exists
  const root = page.locator('#root')
  await expect(root).toBeAttached()
})
