import { expect, test } from "@playwright/test";
import { checkAndScreenshot } from "./screenshot";

const isFixtureMode = process.env.E2E_MODE !== "live";

test.afterEach(({ page }, testInfo) => checkAndScreenshot(page, testInfo, "releases-overview"));

test("fixture: shows the quay-v3.99.0 release card with its ticket", async ({ page }) => {
	test.skip(!isFixtureMode, "fixture-only assertions");
	await page.goto("/");
	const card = page.locator(".pf-v6-c-card", { hasText: "Quay v3.99.0" });
	await expect(card).toBeVisible();
	await expect(card.getByText("PROJQUAY-9001")).toBeVisible();
});

test("live: page loads with at least one release card", async ({ page }) => {
	test.skip(isFixtureMode, "live-only smoke check");
	await page.goto("/");
	await expect(page.locator(".pf-v6-c-card").first()).toBeVisible();
});
