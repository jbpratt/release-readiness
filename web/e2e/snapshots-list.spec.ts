import { expect, test } from "@playwright/test";
import { checkAndScreenshot } from "./screenshot";

const isFixtureMode = process.env.E2E_MODE !== "live";

test.afterEach(({ page }, testInfo) => checkAndScreenshot(page, testInfo, "snapshots-list"));

test("fixture: lists every ingested snapshot", async ({ page }) => {
	test.skip(!isFixtureMode, "fixture-only assertions");
	await page.goto("/releases/quay-v3.99.0/snapshots");

	// ghi789 has no Konflux test result and is still ingested.
	await expect(page.locator("tbody tr td:first-child")).toHaveText([
		"quay-v3-99-snapshot-ghi789",
		"quay-v3-99-snapshot-def456",
		"quay-v3-99-snapshot-abc123",
	]);
});

// Rows on this page need a JIRA release mapped to a live application, which
// only exists with JIRA_TOKEN set, so the row check goes through the API.
test("live: snapshots page loads and live snapshots were ingested", async ({ page, request }) => {
	test.skip(isFixtureMode, "live-only smoke check");
	const res = await request.get("/api/v1/snapshots");
	expect(res.ok()).toBe(true);
	expect((await res.json()).length).toBeGreaterThan(0);

	await page.goto("/");
	await page.locator(".pf-v6-c-card").first().click();
	await page.getByRole("link", { name: "View all snapshots" }).click();
	await expect(page.getByRole("heading", { name: /^Snapshots/, level: 1 })).toBeVisible();
});
