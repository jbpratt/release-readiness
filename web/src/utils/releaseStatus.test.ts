import assert from "node:assert/strict";
import { test } from "node:test";
import type { KonfluxRelease } from "../api/types.ts";
import { releaseStatus } from "./releaseStatus.ts";

const release = (r: Partial<KonfluxRelease>): KonfluxRelease => ({
	name: "r1",
	release_plan: "rp",
	released_status: "",
	released_reason: "",
	created_at: "2026-10-08T09:00:00Z",
	...r,
});

test("releaseStatus", () => {
	const status = (r: Partial<KonfluxRelease>) => {
		const { color, text } = releaseStatus(release(r));
		return [color, text];
	};
	assert.deepEqual(
		status({
			released_status: "False",
			released_reason: "Progressing",
			failed_task: "verify-conforma",
		}),
		["blue", "Release in progress"],
	);
	assert.deepEqual(status({}), ["blue", "Release in progress"]);
	assert.deepEqual(
		status({
			released_status: "False",
			released_reason: "Failed",
			failed_task: "verify-conforma",
		}),
		["red", "Release failed: Enterprise Contract policy"],
	);
	assert.deepEqual(
		status({
			released_status: "False",
			released_reason: "Failed",
			failed_task: "collect-data",
			failed_step: "x",
		}),
		["red", "Release failed at collect-data/x"],
	);
	assert.deepEqual(
		status({ released_status: "True", released_reason: "Succeeded" }),
		["green", "Released"],
	);
});
