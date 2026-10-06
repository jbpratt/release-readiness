import { expect, test } from "@playwright/test";
import { checkAndScreenshot } from "./screenshot";

const isFixtureMode = process.env.E2E_MODE !== "live";

test.afterEach(({ page }, testInfo) => checkAndScreenshot(page, testInfo, "release-detail"));

test.describe("fixture", () => {
	test.skip(!isFixtureMode, "fixture-only assertions");

	// The newest snapshot of each release's quay-3-NN app; fbc-quay-3-NN
	// snapshots are newer but never shown as the latest here.
	const releases = [
		{ name: "quay-v3.16.5", title: "Quay v3.16.5", snapshot: "quay-3-16-20261005-225017-000", issues: 3 },
		{ name: "quay-v3.17.6", title: "Quay v3.17.6", snapshot: "quay-3-17-20261006-114618-000", issues: 4 },
		{ name: "quay-v3.18.0", title: "Quay v3.18.0", snapshot: "fbc-ri-stage-quay-3-18-quay-operator-66vqv", issues: 4 },
	];
	for (const r of releases) {
		test(`${r.name}: latest snapshot and issue count`, async ({ page }) => {
			await page.goto(`/releases/${r.name}`);
			await expect(page.getByRole("heading", { name: r.title, level: 1 })).toBeVisible();
			const latest = page.locator(".pf-v6-c-card", { hasText: "Latest Snapshot" });
			await expect(latest.getByText(r.snapshot)).toBeVisible();
			await expect(page.getByText(`Linked Issues (${r.issues})`)).toBeVisible();
		});
	}

	test("quay-v3.17.6: components and issue details", async ({ page }) => {
		await page.goto("/releases/quay-v3.17.6");

		await expect(page.getByText("Components (11)")).toBeVisible();
		const componentCells = page.locator("table").first().locator("tbody tr td:first-child");
		await expect(componentCells).toHaveCount(11);
		await expect(componentCells.first()).toHaveText("quay-3-17-base-rhel9");
		await expect(componentCells.last()).toHaveText("quay-3-17-quay-quay");
		await expect(componentCells.filter({ hasText: /^quay-3-17-quay-operator$/ })).toHaveCount(1);

		await expect(page.getByRole("link", { name: /^PROJQUAY-917[1-4]$/ })).toHaveCount(4);
		const row = page.getByRole("row", { name: /PROJQUAY-9171/ });
		await expect(row.getByRole("gridcell").nth(1)).toHaveText("Bug");
		await expect(row.getByText("Blocker")).toBeVisible();
		await expect(row.getByText("In Progress")).toBeVisible();
	});

	test("empty snapshot and issues hide their cards", async ({ page }) => {
		await page.route("**/api/v1/releases/quay-v3.17.6/snapshot", (route) =>
			route.fulfill({ status: 404, json: { error: "no snapshots" } }),
		);
		await page.route("**/api/v1/releases/quay-v3.17.6/issues", (route) => route.fulfill({ json: [] }));
		await page.goto("/releases/quay-v3.17.6");
		await expect(page.getByRole("heading", { name: "Quay v3.17.6", level: 1 })).toBeVisible();
		await expect(page.getByText("Release Status")).toBeVisible();
		await expect(page.getByText("Latest Snapshot")).toHaveCount(0);
		await expect(page.getByText(/^Linked Issues/)).toHaveCount(0);
	});

	// Any release fetch failure renders as not found.
	for (const status of [403, 500]) {
		test(`${status} on the release shows release not found`, async ({ page }) => {
			await page.route("**/api/v1/releases/quay-v3.17.6", (route) => route.fulfill({ status, body: "error" }));
			await page.goto("/releases/quay-v3.17.6");
			await expect(page.getByRole("heading", { name: "Release not found" })).toBeVisible();
			await expect(page.getByText('No data found for release "quay-v3.17.6".')).toBeVisible();
		});
	}
});

// Guards the release-to-Konflux application join: a live active release must
// show its latest snapshot with components, not just load. Active releases
// with no Konflux application yet are skipped; if none has a snapshot, the join broke.
test("live: active release shows a snapshot with components", async ({ page, request }) => {
	test.skip(isFixtureMode, "live-only smoke check");
	const overviews: { release: { name: string; released: boolean }; snapshot?: unknown }[] = await (
		await request.get("/api/v1/releases/overview")
	).json();
	const active = overviews.find(
		(ov) => !ov.release.released && ov.release.name.startsWith("quay-v") && ov.snapshot,
	);
	expect(active, "an active quay release with a Konflux snapshot").toBeDefined();

	await page.goto(`/releases/${active?.release.name}`);
	const latest = page.locator(".pf-v6-c-card", { hasText: "Latest Snapshot" });
	await expect(latest).toBeVisible();
	await expect(latest.getByText(/^Components \([1-9]\d*\)$/)).toBeVisible();
	await expect(latest.locator("tbody tr td:first-child").first()).not.toBeEmpty();
});
