import assert from "node:assert/strict";
import { test } from "node:test";
import { snapshotFilters } from "./releaseApps.ts";

test("snapshotFilters", () => {
	assert.deepEqual(
		snapshotFilters("quay-3-18").map((f) => [f.key, f.application]),
		[
			["all", ""],
			["quay", "quay-3-18"],
			["fbc", "fbc-quay-3-18"],
			["base", "quay-images-base"],
		],
	);
	assert.deepEqual(
		snapshotFilters("omr-2-0").map((f) => f.key),
		["all"],
	);
	assert.deepEqual(
		snapshotFilters(undefined).map((f) => f.key),
		["all"],
	);
});
