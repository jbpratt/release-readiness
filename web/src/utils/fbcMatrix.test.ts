import assert from "node:assert/strict";
import { test } from "node:test";
import { type Cell, cellProblem, headline } from "./fbcMatrix.ts";
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

const cells = (rows: FbcRow[]): Cell[] =>
	rows.map((r) => ({ row: r, problem: cellProblem(r, true) }));

const OPS = [
	"quay-operator",
	"container-security-operator",
	"quay-bridge-operator",
];
const OCPS = "4.12 4.13 4.14 4.15 4.16 4.17 4.18 4.19 4.20 4.21 4.22".split(
	" ",
);
const all = OPS.flatMap((o) => OCPS.map((ocp) => row(o, ocp)));

test("headline: all green is the count alone", () => {
	assert.deepEqual(headline(cells(all), true), {
		gaps: [],
		count: "33 of 33 FBCs built and staged",
	});
});

test("headline: names the FBCs missing for an OCP version, then the count", () => {
	const missing = (r: FbcRow) =>
		r.operator !== "quay-operator" && r.ocp === "4.12";
	assert.deepEqual(
		headline(
			cells(
				all.map((r) => (missing(r) ? { ...r, fbc: null, stage: null } : r)),
			),
			true,
		),
		{
			gaps: [
				{
					status: "failed",
					text: "2 FBCs missing for OCP 4.12: container-security-operator, quay-bridge-operator",
				},
			],
			count: "31 of 33 FBCs built and staged",
		},
	);
});

const gapTexts = (rows: FbcRow[], staged = true) =>
	headline(
		rows.map((r) => ({ row: r, problem: cellProblem(r, staged) })),
		staged,
	).gaps.map((g) => g.text);

const unstaged = (pick: (r: FbcRow) => boolean) =>
	all.map((r) => (pick(r) ? { ...r, stage: null } : r));

test("headline: names the OCP versions of one operator's gaps", () => {
	assert.deepEqual(
		gapTexts(
			unstaged(
				(r) =>
					r.operator === "quay-operator" && ["4.12", "4.14"].includes(r.ocp),
			),
		),
		["2 FBCs not staged for quay-operator: OCP 4.12, 4.14"],
	);
});

test("headline: names each FBC when gaps span operators and OCP versions", () => {
	assert.deepEqual(
		gapTexts(
			unstaged(
				(r) =>
					(r.operator === "quay-operator" && r.ocp === "4.12") ||
					(r.operator === "container-security-operator" && r.ocp === "4.14"),
			),
		),
		[
			"2 FBCs not staged: quay-operator OCP 4.12, container-security-operator OCP 4.14",
		],
	);
});

test("headline: a gap in every FBC reads all of them", () => {
	assert.deepEqual(gapTexts(unstaged(() => true)), ["All 33 FBCs not staged"]);
	assert.deepEqual(gapTexts([row("quay-operator", "4.12", { stage: null })]), [
		"1 FBC not staged for OCP 4.12: quay-operator",
	]);
});

test("headline: without a stage shipment the count is of FBCs built", () => {
	assert.deepEqual(
		headline(
			unstaged(() => true).map((r) => ({
				row: r,
				problem: cellProblem(r, false),
			})),
			false,
		),
		{ gaps: [], count: "33 of 33 FBCs built" },
	);
});
