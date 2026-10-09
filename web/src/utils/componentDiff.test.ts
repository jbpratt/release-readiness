import assert from "node:assert/strict";
import { test } from "node:test";
import type { ArtBuild, SnapshotImage } from "../api/types.ts";
import { changedFields } from "./componentDiff.ts";

const image = (
	name: string,
	digest: string,
	git_sha: string,
	art?: Partial<ArtBuild>,
) =>
	({
		name,
		image: `quay.io/redhat-user-workloads/quay/${name}@sha256:${digest}`,
		git_sha,
		art: art ? (art as ArtBuild) : null,
	}) as SnapshotImage;

test("changedFields", () => {
	const released = [
		image("same", "a", "b1", { nvr: "same-1", upstream_sha: "u1" }),
		image("rebuilt", "a", "b1", { nvr: "rebuilt-1", upstream_sha: "u1" }),
		image("no-art", "a", "b1", { nvr: "no-art-1", upstream_sha: "u1" }),
		image("art-now", "a", "b1"),
		image("upstream", "a", "b1", { nvr: "upstream-1", upstream_sha: "u1" }),
	];
	const current = [
		image("same", "a", "b1", { nvr: "same-1", upstream_sha: "u1" }),
		image("rebuilt", "b", "b2", { nvr: "rebuilt-2", upstream_sha: "u1" }),
		image("no-art", "a", "b1"),
		image("added", "c", "b3"),
		image("art-now", "a", "b1", { nvr: "art-now-1", upstream_sha: "u1" }),
		image("upstream", "a", "b1", { nvr: "upstream-1", upstream_sha: "u2" }),
	];
	assert.deepEqual(
		changedFields(current, released),
		new Map([
			["rebuilt", ["nvr", "digest", "build"]],
			["added", ["digest", "build"]],
			["upstream", ["upstream"]],
		]),
	);
});
