import assert from "node:assert/strict";
import { test } from "node:test";
import type {
	DashboardConfig,
	ReleaseCandidate,
	VerdictItem,
} from "../api/types.ts";
import { blockers, grouped, splitBlockers, where } from "./blockers.ts";

const item = (i: Partial<VerdictItem>): VerdictItem => ({
	level: "blocker",
	operator: "",
	owner: "",
	code: "",
	text: "",
	...i,
});

const candidate = (items: VerdictItem[]) =>
	({
		verdict: { state: "not_ready", items },
		fbc_candidate: null,
	}) as unknown as ReleaseCandidate;

const of = (items: VerdictItem[]) =>
	blockers(candidate(items), "quay-v3.17.6", "quay-3-17", undefined);

test("grouped joins one code and operator's OCPs and links", () => {
	const op = "container-security-operator";
	const gs = grouped(
		of([
			item({ code: "missing_ocp", operator: op, text: `${op} OCP 4.17: x` }),
			item({ code: "missing_ocp", operator: op, text: `${op} OCP 4.21: x` }),
			item({ code: "missing_ocp", operator: "quay-operator", text: "y" }),
		]),
	);
	assert.equal(gs.length, 2);
	assert.equal(gs[0].count, 2);
	assert.deepEqual(gs[0].ocps, ["4.17", "4.21"]);
	assert.equal(gs[0].links.length, 1);
	assert.equal(gs[0].what, "OCP versions not staged");
	assert.equal(where(gs[0]), `${op} · OCP 4.17, 4.21`);
	assert.equal(gs[0].links[0].href, `/releases/quay-v3.17.6?operator=${op}`);
	assert.equal(where(gs[1]), "quay-operator");
});

test("an unknown code falls back to the item's text, with no action", () => {
	const [b] = of([item({ code: "new_code", text: "Something new" })]);
	assert.equal(b.what, "Something new");
	assert.equal(b.action, "");
	assert.equal(where(b), "Whole release");
});

test("splitBlockers keeps blockers apart from warnings and notes", () => {
	const { blocking, rest } = splitBlockers(
		of([
			item({ code: "not_staged" }),
			item({ level: "warning", code: "stream_ahead" }),
			item({ level: "info", code: "unknown_fbc" }),
		]),
	);
	assert.deepEqual(
		blocking.map((b) => b.code),
		["not_staged"],
	);
	assert.deepEqual(
		rest.map((b) => b.code),
		["stream_ahead", "unknown_fbc"],
	);
});

const withOperator = (o: object, items: VerdictItem[]) =>
	blockers(
		{
			verdict: { state: "not_ready", items },
			fbc_candidate: { operators: [{ conflicts: [], ...o }] },
		} as unknown as ReleaseCandidate,
		"quay-v3.17.6",
		"quay-3-17",
		{ konflux_ui_url: "https://k", konflux_namespace: "ns" } as DashboardConfig,
	);

test("a stage release failed on another task per OCP stays its own group", () => {
	const op = "quay-operator";
	const gs = grouped(
		withOperator(
			{
				operator: op,
				members: ["4.17", "4.21"].map((ocp) => ({
					ocp,
					release: { name: `rel-${ocp}` },
				})),
			},
			[
				["4.17", "verify-conforma"],
				["4.21", "apply-mapping"],
			].map(([ocp, task]) =>
				item({
					code: "stage_release_failed",
					operator: op,
					text: `${op} OCP ${ocp}: stage release failed (${task})`,
				}),
			),
		),
	);
	assert.deepEqual(
		gs.map((g) => [g.why, where(g), g.links.map((l) => [l.text, l.href])]),
		[
			[
				"Enterprise Contract policy failed",
				`${op} · OCP 4.17`,
				[
					[
						"Release OCP 4.17",
						"https://k/ns/ns/applications/quay-3-17/releases/rel-4.17",
					],
				],
			],
			[
				"Mapping config failed",
				`${op} · OCP 4.21`,
				[
					[
						"Release OCP 4.21",
						"https://k/ns/ns/applications/quay-3-17/releases/rel-4.21",
					],
				],
			],
		],
	);
});

test("mixed builds names the OCPs to rebuild and the head bundle", () => {
	const op = "quay-operator";
	const [b] = withOperator(
		{
			operator: op,
			members: [],
			head_bundle_nvr: "quay-operator-bundle-v3.17.6-202610071230.p0.el9",
		},
		[
			item({
				code: "mixed_builds",
				operator: op,
				text: `${op} OCP 4.17, 4.18: on another bundle`,
			}),
		],
	);
	assert.equal(b.action, "Rebuild OCP 4.17, 4.18 on the Oct 7 12:30 bundle");
});
