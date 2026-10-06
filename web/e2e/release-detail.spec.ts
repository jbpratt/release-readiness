import { expect, test } from "@playwright/test";
import { checkAndScreenshot } from "./screenshot";

const isFixtureMode = process.env.E2E_MODE !== "live";

test.afterEach(({ page }, testInfo) => checkAndScreenshot(page, testInfo, "release-detail"));

test("fixture: shows release name, latest snapshot, components, and issues", async ({ page }) => {
	test.skip(!isFixtureMode, "fixture-only assertions");
	await page.goto("/releases/quay-v3.99.0");

	await expect(page.getByRole("heading", { name: "Quay v3.99.0", level: 1 })).toBeVisible();
	// ghi789 is the newest snapshot even without a Konflux test result.
	await expect(page.getByText("quay-v3-99-snapshot-ghi789")).toBeVisible();
	await expect(page.getByText("Components (1)")).toBeVisible();
	await expect(
		page.locator("table").first().locator("tbody tr td:first-child"),
	).toHaveText(["quay"]);
	await expect(page.getByText("Linked Issues (2)")).toBeVisible();
	await expect(page.getByRole("link", { name: /^PROJQUAY-910[12]$/ })).toHaveCount(2);
});

test("live: first release opens with a heading", async ({ page }) => {
	test.skip(isFixtureMode, "live-only smoke check");
	await page.goto("/");
	await page.locator(".pf-v6-c-card").first().click();
	await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
});
