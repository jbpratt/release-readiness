import { expect, type Page, test } from "@playwright/test";
import { checkAndScreenshot } from "./screenshot";

const isFixtureMode = process.env.E2E_MODE !== "live";

test.afterEach(({ page }, testInfo) => checkAndScreenshot(page, testInfo, "releases-overview"));

const card = (page: Page, name: string) =>
	page.locator(".pf-v6-c-card", { hasText: name });

test.describe("fixture", () => {
	test.skip(!isFixtureMode, "fixture-only assertions");

	test("active and released cards with ticket and signal", async ({ page }) => {
		const overview = page.waitForResponse("**/api/v1/releases/overview");
		await page.goto("/");

		await expect(page.locator(".pf-v6-c-card:visible")).toHaveCount(2);
		await expect(card(page, "Quay v3.17.6").getByText("PROJQUAY-9017")).toBeVisible();
		await expect(card(page, "Quay v3.17.6").getByText("Open issues remain")).toBeVisible();
		await expect(card(page, "Quay v3.18.0").getByText("PROJQUAY-9018")).toBeVisible();

		await page.getByRole("button", { name: "Released (1)" }).click();
		await expect(card(page, "Quay v3.16.5").getByText("Released")).toBeVisible();

		// Cards do not render the snapshot name, so check the payload they were
		// built from: the newest non-fbc snapshot per release.
		const latest = Object.fromEntries(
			(await (await overview).json()).map(
				(ov: { release: { name: string }; snapshot?: { name: string } }) => [
					ov.release.name,
					ov.snapshot?.name,
				],
			),
		);
		expect(latest).toEqual({
			"quay-v3.16.5": "quay-3-16-20261005-225017-000",
			"quay-v3.17.6": "quay-3-17-20261006-114618-000",
			"quay-v3.18.0": "fbc-ri-stage-quay-3-18-quay-operator-66vqv",
		});
	});

	test("search and signal filters", async ({ page }) => {
		await page.goto("/");
		await page.getByPlaceholder("Filter releases...").fill("3.18");
		await expect(page.locator(".pf-v6-c-card")).toHaveCount(1);
		await expect(card(page, "Quay v3.18.0")).toBeVisible();
		await expect(page).toHaveURL(/q=3\.18/);

		await page.goto("/");
		const signals = page.getByRole("group", { name: "Signal filter" });
		await signals.getByRole("button", { name: "Red" }).click();
		await expect(page.locator(".pf-v6-c-card")).toHaveCount(0);
		await signals.getByRole("button", { name: "Yellow" }).click();
		await expect(page.locator(".pf-v6-c-card")).toHaveCount(2);
		await expect(page).toHaveURL(/signal=yellow/);
	});

	test("card opens release detail and breadcrumb returns", async ({ page }) => {
		await page.goto("/");
		await card(page, "Quay v3.17.6").click();
		await expect(page).toHaveURL(/\/releases\/quay-v3\.17\.6$/);
		await expect(page.getByRole("heading", { name: "Quay v3.17.6", level: 1 })).toBeVisible();

		await page.getByRole("link", { name: "Releases", exact: true }).click();
		await expect(page).toHaveURL(/\/$/);
		await card(page, "Quay v3.18.0").click();
		await expect(page.getByRole("heading", { name: "Quay v3.18.0", level: 1 })).toBeVisible();
		await page.goBack();
		await expect(card(page, "Quay v3.17.6")).toBeVisible();
	});

	test("empty list shows the no-releases state", async ({ page }) => {
		await page.route("**/api/v1/releases/overview", (route) => route.fulfill({ json: [] }));
		await page.goto("/");
		await expect(page.getByRole("heading", { name: "No releases found" })).toBeVisible();
	});

	// The page ignores fetch errors, so 403 and 500 fall through to the empty state.
	for (const status of [403, 500]) {
		test(`${status} shows the no-releases state`, async ({ page }) => {
			await page.route("**/api/v1/releases/overview", (route) =>
				route.fulfill({ status, body: "error" }),
			);
			await page.goto("/");
			await expect(page.getByRole("heading", { name: "No releases found" })).toBeVisible();
		});
	}
});

test("live: page loads with at least one release card", async ({ page }) => {
	test.skip(isFixtureMode, "live-only smoke check");
	await page.goto("/");
	await expect(page.locator(".pf-v6-c-card").first()).toBeVisible();
});
