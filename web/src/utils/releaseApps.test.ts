import assert from "node:assert/strict";
import { test } from "node:test";
import type { KonfluxRelease, ReleaseSnapshot } from "../api/types.ts";
import { latestReleased, snapshotFilters } from "./releaseApps.ts";

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

test("latestReleased", () => {
	const release = (status: string, created_at: string) =>
		({ released_status: status, created_at }) as KonfluxRelease;
	const snap = (name: string, ...releases: KonfluxRelease[]) =>
		({ name, art_kind: "image", releases }) as ReleaseSnapshot;
	// Newest-first, as the API lists them: the newest Snapshot's Release
	// failed, and an older Snapshot was released after a newer one.
	const snapshots = [
		snap("c", release("False", "2026-10-08T00:00:00Z")),
		snap("b", release("True", "2026-10-06T00:00:00Z")),
		snap("a", release("True", "2026-10-07T00:00:00Z")),
		snap("none"),
	];
	assert.equal(latestReleased(snapshots)?.name, "a");
	assert.equal(latestReleased(snapshots.slice(0, 1)), undefined);
	// ART releases an fbc-ri Snapshot, a bundle's related images, without
	// staging it, so it is not an image build.
	const fbcRI = {
		name: "fbc-ri",
		releases: [release("True", "2026-10-09T00:00:00Z")],
	} as ReleaseSnapshot;
	assert.equal(latestReleased([fbcRI, ...snapshots])?.name, "a");
});
