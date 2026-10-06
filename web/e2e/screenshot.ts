import { expect, type Page, type TestInfo } from "@playwright/test";

export async function checkAndScreenshot(page: Page, testInfo: TestInfo, name: string) {
	if (testInfo.status === "skipped") return;
	await expect(page.getByText("Something went wrong")).toHaveCount(0);
	// PatternFly scrolls an inner element, so fullPage alone stops at the viewport.
	const height = await page.evaluate(() =>
		Math.max(
			...Array.from(
				document.querySelectorAll("*"),
				(el) => el.getBoundingClientRect().top + el.scrollHeight,
			),
		),
	);
	await page.setViewportSize({ width: page.viewportSize()?.width ?? 1280, height: Math.ceil(height) });
	await page.screenshot({ path: testInfo.outputPath(`${name}.png`), fullPage: true });
}
