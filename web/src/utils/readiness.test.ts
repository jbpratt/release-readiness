import assert from "node:assert/strict";
import { test } from "node:test";
import type {
	BuildTicket,
	KonfluxRelease,
	ReleaseVersion,
	StagedSnapshot,
} from "../api/types.ts";
import { dueText, readiness } from "./readiness.ts";

// West of UTC, a local date shows a midnight-UTC due date as the day before.
process.env.TZ = "America/New_York";

const released = (at: string) =>
	({
		released_status: "True",
		released_reason: "Succeeded",
		created_at: at,
		completion_time: at,
	}) as KonfluxRelease;
const staged = (name: string, at: string): StagedSnapshot => ({
	name,
	created_at: at,
	release: released(at),
});
// /staged's catalogs, null for an operator not given.
const catalogs = (byOperator: Record<string, StagedSnapshot> = {}) =>
	["quay-operator", "container-security-operator", "quay-bridge-operator"].map(
		(operator) => ({ operator, staged: byOperator[operator] ?? null }),
	);
const tickets = (n: number, fix_version: string, status: string) =>
	Array.from({ length: n }, () => ({ fix_version, status }) as BuildTicket);
const marks = (stages: { mark: string }[]) =>
	stages.map((s) => s.mark).join("");
const now = Date.parse("2026-10-09T21:50:00Z");

const operator = staged(
	"quay-stage-3-18-1-fbc-20261007222921",
	"2026-10-07T22:32:26Z",
);
const cso = staged(
	"quay-stage-3-18-1-fbc-20261007222929",
	"2026-10-07T22:43:14Z",
);
const v3181 = {
	release: {
		name: "quay-v3.18.1",
		released: false,
		due_date: "2026-08-20T00:00:00Z",
	} as ReleaseVersion,
	staged: {
		stream_staged: true,
		staged_image: staged(
			"quay-stage-3-18-1-image-20261007200243",
			"2026-10-07T20:14:57Z",
		),
		catalogs: catalogs({
			"quay-operator": operator,
			"container-security-operator": cso,
			"quay-bridge-operator": staged(
				"quay-stage-3-18-1-fbc-20261007222918",
				"2026-10-07T22:32:14Z",
			),
		}),
	},
	// The .z tickets a build commit names count too.
	tickets: [
		...tickets(42, "quay-v3.18.1", "MODIFIED"),
		...tickets(19, "quay-v3.18.1", "ON_QA"),
		...tickets(7, "quay-v3.18.1", "Release Pending"),
		...tickets(11, "quay-v3.18.z", "MODIFIED"),
		...tickets(1, "quay-v3.18.z", "ON_QA"),
		...tickets(1, "quay-v3.18.z", "ASSIGNED"),
		...tickets(1, "quay-v3.18.z", "Closed"),
	],
	shipped: false,
	jiraEnabled: true,
	now,
};

const v3175 = {
	release: {
		name: "quay-v3.17.5",
		released: false,
		due_date: "2026-08-27T00:00:00Z",
	} as ReleaseVersion,
	staged: { stream_staged: true, staged_image: null, catalogs: catalogs() },
	tickets: [
		...tickets(52, "quay-v3.17.5", "Closed"),
		...tickets(1, "quay-v3.17.5", "MODIFIED"),
		...tickets(1, "quay-v3.17.5", "New"),
	],
	// In the Red Hat catalog; Jira has not released it.
	shipped: true,
	jiraEnabled: true,
	now,
};

test("3.18.1: the .z tickets count, and Tickets blocks", () => {
	const r = readiness(v3181);
	assert.equal(marks(r.stages), "vvx");
	assert.deepEqual(
		r.stages.map((s) => s.state),
		["Staged", "3 of 3 staged", "74 of 82 not verified"],
	);
	assert.deepEqual(r.verdict, {
		color: "red",
		text: "Not ready: 74 tickets not verified",
		message: "Next: QE verifies them; they are in the list below.",
	});
	assert.equal(dueText(v3181.release.due_date), "Due Aug 20, 2026");
});

test("3.17.6: Tickets block at 15 of 18 while two catalogs wait", () => {
	const r = readiness({
		...v3181,
		release: {
			name: "quay-v3.17.6",
			released: false,
			due_date: "2026-09-17T00:00:00Z",
		} as ReleaseVersion,
		staged: {
			stream_staged: true,
			staged_image: staged(
				"quay-stage-3-17-6-image-20261008082443",
				"2026-10-08T08:36:45Z",
			),
			catalogs: catalogs({
				"quay-operator": staged(
					"quay-stage-3-17-6-fbc-20261008084029",
					"2026-10-08T09:25:03Z",
				),
			}),
		},
		tickets: [
			...tickets(14, "quay-v3.17.z", "MODIFIED"),
			...tickets(1, "quay-v3.17.z", "ON_QA"),
			...tickets(3, "quay-v3.17.z", "Closed"),
		],
	});
	assert.equal(marks(r.stages), "vox");
	assert.deepEqual(
		[r.stages[1].state, r.stages[1].detail],
		[
			"1 of 3 staged",
			"Not staged: container-security-operator, quay-bridge-operator",
		],
	);
	assert.deepEqual(
		r.stages[1].operators?.map((s) => s.mark),
		["v", "o", "o"],
	);
	assert.equal(r.stages[2].state, "15 of 18 not verified");
	assert.equal(r.verdict.text, "Not ready: 15 tickets not verified");
});

test("shipped: the verdict says Shipped and no stage blocks", () => {
	const r = readiness(v3175);
	assert.equal(marks(r.stages), "ooo");
	assert.deepEqual(
		r.stages.map((s) => s.state),
		["No record", "No record", "2 of 54 not verified"],
	);
	assert.deepEqual(r.verdict, {
		color: "green",
		text: "Shipped",
		message: "In the Red Hat catalog; not released in Jira",
	});
	const jira = readiness({
		...v3175,
		release: { ...v3175.release, released: true },
		shipped: false,
	});
	assert.deepEqual(
		[jira.verdict.text, jira.verdict.message],
		["Shipped", "Released in Jira"],
	);
});

test("Past due date: late and not shipped only", () => {
	assert.equal(readiness(v3181).pastDue, "Past due date · 51 days late");
	assert.equal(readiness(v3175).pastDue, undefined);
	const early = Date.parse("2026-08-19T12:00:00Z");
	assert.equal(readiness({ ...v3181, now: early }).pastDue, undefined);
	const dueDay = Date.parse("2026-08-20T15:00:00Z");
	assert.equal(
		readiness({ ...v3181, now: dueDay }).pastDue,
		"Past due date · 1 day late",
	);
});

test("failed staging: one catalog blocks until the version ships", () => {
	const input = {
		...v3181,
		staged: {
			...v3181.staged,
			catalogs: catalogs({
				"quay-operator": operator,
				"container-security-operator": {
					...cso,
					release: {
						released_status: "False",
						released_reason: "Failed",
						created_at: "2026-10-07T22:43:14Z",
					} as KonfluxRelease,
				},
			}),
		},
	};
	const r = readiness(input);
	assert.equal(marks(r.stages), "vxx");
	assert.deepEqual(
		[r.stages[1].state, r.stages[1].detail],
		[
			"1 of 3 staged",
			"Staging failed: container-security-operator; Not staged: quay-bridge-operator",
		],
	);
	assert.deepEqual(r.verdict, {
		color: "red",
		text: "Not ready: the container-security-operator catalog failed to stage",
		message: "Next: Ask ART to restage it.",
	});
	assert.equal(marks(readiness({ ...input, shipped: true }).stages), "voo");
});

test("staging: the build waits on ART until its Release succeeds", () => {
	const staging = {
		...v3181.staged.staged_image,
		release: {
			released_status: "False",
			released_reason: "Progressing",
			created_at: "2026-10-09T13:39:00Z",
		} as KonfluxRelease,
	};
	const input = {
		...v3181,
		staged: { ...v3181.staged, staged_image: staging },
		tickets: tickets(3, "quay-v3.18.1", "Closed"),
	};
	const r = readiness(input);
	assert.equal(marks(r.stages), "ovv");
	assert.equal(r.stages[0].state, "Staging");
	assert.deepEqual(r.verdict, {
		color: "blue",
		text: "Next: ART stages the build",
		message: "ART is staging it, since Oct 9 13:39Z",
	});
	const built = readiness({
		...input,
		staged: { ...input.staged, staged_image: { ...staging, release: null } },
	});
	assert.deepEqual(
		[built.stages[0].state, built.stages[0].detail],
		["Not staged", "ART built it but has not staged it yet"],
	);
});

test("no Shipped stage, shipped or not", () => {
	for (const input of [v3181, v3175])
		assert.deepEqual(
			readiness(input).stages.map((s) => s.label),
			["Build", "Catalog", "Tickets"],
		);
});

test("not started: ART has staged nothing", () => {
	const r = readiness({
		...v3181,
		release: { name: "quay-v3.18.2", released: false } as ReleaseVersion,
		staged: { stream_staged: true, staged_image: null, catalogs: catalogs() },
		tickets: [],
	});
	assert.equal(marks(r.stages), "oov");
	assert.deepEqual(
		r.stages.slice(0, 2).map((s) => [s.state, s.detail]),
		[
			["Not staged", "ART has not staged it yet"],
			["Not staged", "ART has not staged it yet"],
		],
	);
	assert.deepEqual(r.verdict, {
		color: "blue",
		text: "Next: ART stages the build",
		message: "ART has not staged it yet",
	});
	assert.equal(r.pastDue, undefined);
});

test("a stream ART does not stage: Tickets only", () => {
	const r = readiness({
		...v3181,
		release: { name: "quay-v3.16.8", released: false } as ReleaseVersion,
		staged: { stream_staged: false, staged_image: null, catalogs: catalogs() },
		tickets: tickets(3, "quay-v3.16.8", "Closed"),
	});
	assert.deepEqual(
		r.stages.map((s) => [s.label, s.mark, s.state]),
		[["Tickets", "v", "All 3 verified"]],
	);
	assert.deepEqual(r.verdict, {
		color: "blue",
		text: "Next: the production release",
		message: "Not in the Red Hat catalog; not released in Jira",
	});
});

test("Release Pending counts as verified", () => {
	const r = readiness({
		...v3181,
		tickets: [
			...tickets(2, "quay-v3.18.1", "Release Pending"),
			...tickets(1, "quay-v3.18.z", "Closed"),
		],
	});
	assert.deepEqual(
		[r.stages[2].mark, r.stages[2].state],
		["v", "All 3 verified"],
	);
	assert.equal(r.verdict.text, "Next: the production release");
});

test("Jira not configured: no tickets is never verified", () => {
	const r = readiness({ ...v3181, tickets: [], jiraEnabled: false });
	assert.deepEqual(
		[r.stages[2].mark, r.stages[2].state],
		["o", "Jira not configured"],
	);
	assert.equal(r.verdict.text, "Next: a Jira sync");
	assert.equal(readiness({ ...v3181, tickets: [] }).stages[2].mark, "v");
});
