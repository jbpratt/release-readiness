import assert from "node:assert/strict";
import { test } from "node:test";
import type { FBCCandidate } from "../api/types.ts";
import { cellProblem, streamFbc } from "./fbcMatrix.ts";
import type { FbcRow } from "./releaseDetail.ts";

const row = (operator: string, ocp: string, over: Partial<FbcRow> = {}) =>
	({
		operator,
		ocp,
		fbc: {
			ocp,
			digest: "sha256:f",
			pullspec: `quay.io/fbc@sha256:${ocp}`,
			nvr: `${operator}-fbc-3.18.1-20261013021425.ocp${ocp}`,
			build_url: "",
			head_bundle_nvr: "bundle",
			release: null,
			ci: null,
		},
		stage: { status: "done", text: "Staged Oct 13" },
		flags: [],
		runs: [],
		images: [],
		...over,
	}) satisfies FbcRow;

const failedRun = {
	state: "failure",
	started_at: "",
	prow_url: "",
	job: "j",
	runs: 1,
	equivalent: false,
};

test("cellProblem: a built and staged FBC has none", () => {
	assert.equal(cellProblem(row("quay-operator", "4.14"), true), null);
});

test("cellProblem: a missing FBC is not built, even with failed CI", () => {
	assert.equal(
		cellProblem(
			row("quay-operator", "4.12", {
				fbc: null,
				stage: null,
				runs: [failedRun],
			}),
			true,
		),
		"not built",
	);
});

test("cellProblem: a missing FBC the stream has is not staged", () => {
	const r = row("container-security-operator", "4.12", {
		fbc: null,
		stage: null,
	});
	const stream = {
		operators: [
			{
				operator: "container-security-operator",
				members: [row("container-security-operator", "4.12").fbc],
				conflicts: [],
			},
		],
	} as unknown as FBCCandidate;
	assert.equal(cellProblem(r, true, streamFbc(stream, r)), "not staged");
	const other = row("quay-operator", "4.12", { fbc: null, stage: null });
	assert.equal(streamFbc(stream, other), null);
	assert.equal(cellProblem(other, true, streamFbc(stream, other)), "not built");
});

test("cellProblem: a built FBC without a stage Release is not staged", () => {
	const r = row("quay-operator", "4.14", { stage: null });
	assert.equal(cellProblem(r, true), "not staged");
	// Without a stage shipment nothing is staged, so it does not count.
	assert.equal(cellProblem(r, false), null);
});

test("cellProblem: staging comes before CI", () => {
	assert.equal(
		cellProblem(
			row("quay-operator", "4.14", { stage: null, runs: [failedRun] }),
			true,
		),
		"not staged",
	);
	assert.equal(
		cellProblem(row("quay-operator", "4.14", { runs: [failedRun] }), true),
		"CI failed",
	);
});
