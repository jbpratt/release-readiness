import { expect, test } from "@playwright/test";
import { checkAndScreenshot } from "./screenshot";

const isFixtureMode = process.env.E2E_MODE !== "live";

test.afterEach(({ page }, testInfo) => checkAndScreenshot(page, testInfo, "snapshots-list"));

const snapshotsAPI = /\/api\/v1\/snapshots\?/;

test.describe("fixture", () => {
	test.skip(!isFixtureMode, "fixture-only assertions");

	test("lists quay-3-18 and fbc-quay-3-18 snapshots newest first", async ({ page }) => {
		await page.goto("/releases/quay-v3.18.0/snapshots");

		await expect(page.locator("tbody tr td:first-child")).toHaveText([
			"fbc-quay-3-18-20261006-174355-000",
			"fbc-quay-3-18-20261006-174342-000",
			"fbc-ri-stage-quay-3-18-quay-operator-66vqv",
			"quay-3-18-20261006-170808-000",
			"quay-3-18-20261006-165007-000",
			"fbc-quay-3-18-20261006-151706-000",
		]);
		await expect(page.locator("tbody tr td:nth-child(2)")).toHaveText([
			"fbc-quay-3-18",
			"fbc-quay-3-18",
			"quay-3-18",
			"quay-3-18",
			"quay-3-18",
			"fbc-quay-3-18",
		]);
	});

	test("breadcrumbs navigate from release detail and back", async ({ page }) => {
		await page.goto("/releases/quay-v3.17.6");
		await page.getByRole("link", { name: "View all snapshots" }).click();
		await expect(page).toHaveURL(/\/releases\/quay-v3\.17\.6\/snapshots$/);
		await expect(page.getByRole("heading", { name: "Snapshots - Quay v3.17.6", level: 1 })).toBeVisible();

		await page.getByRole("link", { name: "Quay v3.17.6" }).click();
		await expect(page.getByRole("heading", { name: "Quay v3.17.6", level: 1 })).toBeVisible();
		await page.goBack();
		await page.getByRole("link", { name: "Releases", exact: true }).click();
		await expect(page.locator(".pf-v6-c-card", { hasText: "Quay v3.17.6" })).toBeVisible();
	});

	test("empty list shows the no-snapshots state", async ({ page }) => {
		await page.route(snapshotsAPI, (route) => route.fulfill({ json: [] }));
		await page.goto("/releases/quay-v3.18.0/snapshots");
		await expect(page.getByRole("heading", { name: "No snapshots" })).toBeVisible();
		await expect(page.getByText("No snapshots found for this release.")).toBeVisible();
	});

	for (const [status, text] of [
		[403, "403 Forbidden"],
		[500, "500 Internal Server Error"],
	] as const) {
		test(`${status} shows the error state`, async ({ page }) => {
			await page.route(snapshotsAPI, (route) => route.fulfill({ status, body: "error" }));
			await page.goto("/releases/quay-v3.18.0/snapshots");
			await expect(page.getByRole("heading", { name: "Error loading snapshots" })).toBeVisible();
			await expect(page.getByText(text)).toBeVisible();
		});
	}
});

// The first release card may have no live application yet, so ingestion is
// checked through the API and the page only has to load.
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
