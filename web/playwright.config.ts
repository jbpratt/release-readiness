import { defineConfig, devices } from "@playwright/test";

// Workers re-evaluate this file; the env var pins one run dir per invocation.
process.env.E2E_RUN_ID ??= new Date().toISOString().replace(/[:.]/g, "-");
const artifactsDir = `e2e-artifacts/${process.env.E2E_RUN_ID}`;

export default defineConfig({
	testDir: "./e2e",
	fullyParallel: true,
	forbidOnly: !!process.env.CI,
	reporter: [["list"], ["html", { outputFolder: `${artifactsDir}/report`, open: "never" }]],
	outputDir: `${artifactsDir}/test-results`,
	use: {
		baseURL: process.env.E2E_BASE_URL,
		screenshot: "on",
		trace: "on",
	},
	projects: [
		{
			name: "chromium",
			use: { ...devices["Desktop Chrome"] },
		},
	],
});
