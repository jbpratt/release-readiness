import assert from "node:assert/strict";
import { test } from "node:test";
import type { BuildAttempt } from "../api/types.ts";
import { summarizeBuilds } from "./buildAttempts.ts";

const attempt = (
	component: string,
	outcome: string,
	started_at: string,
): BuildAttempt => ({
	component,
	outcome,
	started_at,
	build_url: "",
});

test("summarizeBuilds", () => {
	const got = summarizeBuilds([
		// Newest first, as the API returns them.
		attempt("quay-quay-container", "pending", "2026-10-08T04:00:00Z"),
		attempt("quay-quay-container", "build_error", "2026-10-08T03:00:00Z"),
		attempt("quay-quay-container", "build_error", "2026-10-08T02:00:00Z"),
		attempt("quay-quay-container", "success", "2026-10-08T01:00:00Z"),
		attempt("quay-clair-container", "release_error", "2026-10-07T00:00:00Z"),
	]);
	assert.deepEqual(
		got.map(({ lastSuccess, ...c }) => ({
			...c,
			lastSuccess: lastSuccess?.started_at ?? null,
		})),
		[
			// No success in the covered span: unknown, not zero.
			{
				component: "quay-clair-container",
				attempts: 1,
				failed: 1,
				retries: 0,
				lastSuccess: null,
			},
			{
				component: "quay-quay-container",
				attempts: 4,
				failed: 2,
				retries: 2,
				lastSuccess: "2026-10-08T01:00:00Z",
			},
		],
	);
	assert.deepEqual(summarizeBuilds([]), []);
});
