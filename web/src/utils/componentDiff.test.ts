import assert from "node:assert/strict";
import { test } from "node:test";
import type { ArtBuild, SnapshotImage } from "../api/types.ts";
import { changedFields } from "./componentDiff.ts";

const image = (name: string, digest: string, art?: Partial<ArtBuild>) =>
	({
		name,
		image: `quay.io/redhat-user-workloads/quay/${name}@sha256:${digest}`,
		art: art ? (art as ArtBuild) : null,
	}) as SnapshotImage;

test("changedFields", () => {
	const released = [
		image("same", "a", { nvr: "same-1", upstream_sha: "u1" }),
		image("rebuilt", "a", { nvr: "rebuilt-1", upstream_sha: "u1" }),
		image("no-art", "a", { nvr: "no-art-1", upstream_sha: "u1" }),
		image("art-now", "a"),
		image("upstream", "a", { nvr: "upstream-1", upstream_sha: "u1" }),
	];
	const current = [
		image("same", "a", { nvr: "same-1", upstream_sha: "u1" }),
		image("rebuilt", "b", { nvr: "rebuilt-2", upstream_sha: "u1" }),
		image("no-art", "a"),
		image("added", "c"),
		image("art-now", "a", { nvr: "art-now-1", upstream_sha: "u1" }),
		image("upstream", "a", { nvr: "upstream-1", upstream_sha: "u2" }),
	];
	assert.deepEqual(
		changedFields(current, released),
		new Map([
			["rebuilt", ["nvr", "digest"]],
			["added", ["digest"]],
			["upstream", ["upstream"]],
		]),
	);
});
