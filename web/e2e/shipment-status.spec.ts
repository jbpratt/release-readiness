import { expect, test, type Page } from "@playwright/test";

const version = "quay-v3.18.1";
const mrURL = "https://gitlab.example/merge_requests/948";

const config = {
	jira_base_url: "https://jira.example",
	jira_project: "PROJQUAY",
	jira_enabled: true,
	konflux_ui_url: "",
	konflux_namespace: "",
};

const candidate = {
	shipped: true,
	candidate: null,
	reason: "",
	fbc_candidate: null,
	fbc_stream: null,
	verdict: { state: "shipped", items: [] },
	reading: { read: 0, failed: 0, pending: 0, history_days: 0 },
};

async function mockRelease(
	page: Page,
	mr: { state: "opened" | "merged"; stage: "prod_released" | "shipped" },
) {
	await page.route("**/api/v1/config", (route) => route.fulfill({ json: config }));
	await page.route("**/api/v1/sync-status", (route) =>
		route.fulfill({ json: { problems: [], sources: [] } }),
	);
	await page.route(`**/api/v1/releases/${version}`, (route) =>
		route.fulfill({
			json: {
				name: version,
				released: true,
				shipment_mr: {
					iid: 948,
					web_url: mrURL,
					state: mr.state,
					draft: false,
					stage: mr.stage,
					pipeline_failed: false,
					needs_approval: false,
					source: "pointer",
				},
			},
		}),
	);
	await page.route(`**/api/v1/releases/${version}/issues`, (route) =>
		route.fulfill({ json: [] }),
	);
	await page.route(`**/api/v1/releases/${version}/candidate`, (route) =>
		route.fulfill({ json: candidate }),
	);
	await page.route(`**/api/v1/releases/${version}/ci`, (route) =>
		route.fulfill({ json: [] }),
	);
}

test("links an open shipment MR from Open in on a shipped page", async ({
	page,
}, testInfo) => {
	await mockRelease(page, { state: "opened", stage: "prod_released" });

	await page.goto(`/releases/${version}`);

	await page.getByRole("button", { name: "Open in" }).click();
	await expect(page.locator(`a[href="${mrURL}"]`)).toHaveText("Shipment MR");
	await expect(page.getByText("Production release succeeded")).toHaveCount(0);
	await page.screenshot({
		path: testInfo.outputPath("open-shipment-mr.png"),
		fullPage: true,
	});
});

test("links a merged shipment MR from Open in on a shipped page", async ({
	page,
}, testInfo) => {
	await mockRelease(page, { state: "merged", stage: "shipped" });

	await page.goto(`/releases/${version}`);

	await page.getByRole("button", { name: "Open in" }).click();
	await expect(page.locator(`a[href="${mrURL}"]`)).toHaveText("Shipment MR");
	await page.screenshot({
		path: testInfo.outputPath("merged-shipment-mr.png"),
		fullPage: true,
	});
});
