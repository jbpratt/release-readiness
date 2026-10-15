import assert from "node:assert/strict";
import { test } from "node:test";
import type { FBCCandidate } from "../api/types.ts";
import type { Blocker } from "./blockers.ts";
import { candidateSummary, cellProblem, streamFbc } from "./fbcMatrix.ts";
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

const cell = (r: FbcRow) => ({
	row: r,
	stream: null,
	problem: cellProblem(r, true),
});

const item = (
	level: Blocker["level"],
	what: string,
	operator = "",
	ocps: string[] = [],
) => ({ level, what, operator, ocps }) as Blocker;

test("candidateSummary: all green counts its FBCs", () => {
	const cells = [
		cell(row("quay-operator", "4.14")),
		cell(row("quay-operator", "4.15")),
	];
	assert.deepEqual(candidateSummary(cells, []), {
		tone: "success",
		words: "2 of 2 FBCs",
		more: "",
		all: "2 of 2 FBCs good",
	});
});

test("candidateSummary: blockers lead, one per kind, warnings after", () => {
	const cells = [cell(row("quay-operator", "4.14"))];
	const bs = [
		item("warning", "Newer build not on stage"),
		item("blocker", "Not staged"),
		item("blocker", "Not staged"),
		item("info", "Note"),
	];
	assert.deepEqual(candidateSummary(cells, bs), {
		tone: "danger",
		words: "Not staged",
		more: "+1",
		all: "Not staged; Newer build not on stage",
	});
});

test("candidateSummary: a missing FBC is red, an info item is not green", () => {
	const cells = [
		cell(row("quay-operator", "4.12", { fbc: null, stage: null })),
		cell(row("quay-operator", "4.14")),
		cell(row("quay-operator", "4.15")),
	];
	const s = candidateSummary(cells, [
		item("info", "Note", "quay-operator", ["4.15"]),
	]);
	assert.equal(s.tone, "danger");
	assert.equal(s.words, "1 of 3 FBCs");
	assert.equal(
		candidateSummary(cells.slice(2), [
			item("info", "Note", "quay-operator", ["4.15"]),
		]).tone,
		"info",
	);
});

test("candidateSummary: a warning cell outranks a staging one", () => {
	const staging = cell(
		row("quay-operator", "4.14", {
			stage: { status: "running", text: "Staging" },
		}),
	);
	const unstaged = cell(row("quay-operator", "4.15", { stage: null }));
	assert.equal(candidateSummary([staging], []).tone, "running");
	assert.equal(candidateSummary([staging, unstaged], []).tone, "warning");
});
