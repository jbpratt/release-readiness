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
const tickets = (n: number, fix_version: string, status: string) =>
	Array.from({ length: n }, () => ({ fix_version, status }) as BuildTicket);
const marks = (stages: { mark: string }[]) =>
	stages.map((s) => s.mark).join("");
const now = Date.parse("2026-10-09T21:50:00Z");

const v3181 = {
	release: {
		name: "quay-v3.18.1",
		released: false,
		due_date: "2026-08-20T00:00:00Z",
	} as ReleaseVersion,
	build: staged(
		"quay-stage-3-18-1-image-20261007200243",
		"2026-10-07T20:14:57Z",
	),
	catalogs: {
		"quay-operator": staged(
			"quay-stage-3-18-1-fbc-20261007222921",
			"2026-10-07T22:32:26Z",
		),
		"container-security-operator": staged(
			"quay-stage-3-18-1-fbc-20261007222929",
			"2026-10-07T22:43:14Z",
		),
		"quay-bridge-operator": staged(
			"quay-stage-3-18-1-fbc-20261007222918",
			"2026-10-07T22:32:14Z",
		),
	},
	// Q5-A: the .z tickets a build commit names count too.
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
	showShipped: true,
	now,
};

const v3175 = {
	release: {
		name: "quay-v3.17.5",
		released: false,
		due_date: "2026-08-27T00:00:00Z",
	} as ReleaseVersion,
	build: null,
	catalogs: {},
	tickets: [
		...tickets(52, "quay-v3.17.5", "Closed"),
		...tickets(1, "quay-v3.17.5", "MODIFIED"),
		...tickets(1, "quay-v3.17.5", "New"),
	],
	// In the Red Hat catalog; Jira has not released it.
	shipped: true,
	showShipped: true,
	now,
};

test("3.18.1: the .z tickets count, and Tickets blocks", () => {
	const r = readiness(v3181);
	assert.equal(marks(r.stages), "vvxo");
	assert.deepEqual(
		r.stages.map((s) => [s.state, s.detail]),
		[
			["Staged", "Oct 7 20:14Z"],
			["3 of 3 staged", "Newest Oct 7 22:43Z"],
			["74 of 82 not verified", "53 MODIFIED, 20 ON_QA, 1 ASSIGNED"],
			["Not shipped", "Not in the Red Hat catalog; not released in Jira"],
		],
	);
	assert.deepEqual(r.stages[2].verified, [8, 82]);
	assert.deepEqual(r.verdict, {
		color: "red",
		text: "Not ready: 74 tickets not verified",
	});
	assert.equal(
		r.message,
		"Next: QE verifies them; they are in the list below.",
	);
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
		build: staged(
			"quay-stage-3-17-6-image-20261008082443",
			"2026-10-08T08:36:45Z",
		),
		catalogs: {
			"quay-operator": staged(
				"quay-stage-3-17-6-fbc-20261008084029",
				"2026-10-08T09:25:03Z",
			),
		},
		tickets: [
			...tickets(14, "quay-v3.17.z", "MODIFIED"),
			...tickets(1, "quay-v3.17.z", "ON_QA"),
			...tickets(3, "quay-v3.17.z", "Closed"),
		],
	});
	assert.equal(marks(r.stages), "voxo");
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
	assert.equal(r.focus.label, "Tickets");
	assert.equal(r.verdict.text, "Not ready: 15 tickets not verified");
});

test("shipped: the verdict says Shipped and no stage blocks", () => {
	const r = readiness(v3175);
	assert.equal(marks(r.stages), "ooov");
	assert.deepEqual(
		r.stages.map((s) => s.state),
		["No record", "No record", "2 of 54 not verified", "Shipped"],
	);
	assert.deepEqual(r.verdict, { color: "green", text: "Shipped" });
	assert.equal(r.message, "In the Red Hat catalog; not released in Jira");
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
		catalogs: {
			"quay-operator": v3181.catalogs["quay-operator"],
			"container-security-operator": {
				...v3181.catalogs["container-security-operator"],
				release: {
					released_status: "False",
					released_reason: "Failed",
					created_at: "2026-10-07T22:43:14Z",
				} as KonfluxRelease,
			},
		},
	};
	const r = readiness(input);
	assert.equal(marks(r.stages), "vxxo");
	assert.deepEqual(
		[r.stages[1].state, r.stages[1].detail],
		[
			"1 of 3 staged",
			"Staging failed: container-security-operator; Not staged: quay-bridge-operator",
		],
	);
	assert.equal(
		r.verdict.text,
		"Not ready: the container-security-operator catalog failed to stage",
	);
	assert.equal(r.message, "Next: Ask ART to restage it.");
	assert.equal(marks(readiness({ ...input, shipped: true }).stages), "voov");
});

test("q4 none: no Shipped stage, and the verdict still says Shipped", () => {
	const r = readiness({ ...v3181, showShipped: false });
	assert.deepEqual(
		r.stages.map((s) => s.label),
		["Build", "Catalog", "Tickets"],
	);
	assert.equal(r.verdict.text, "Not ready: 74 tickets not verified");
	const shipped = readiness({ ...v3175, showShipped: false });
	assert.equal(marks(shipped.stages), "ooo");
	assert.equal(shipped.verdict.text, "Shipped");
});

test("not started: ART has staged nothing", () => {
	const r = readiness({
		...v3181,
		release: { name: "quay-v3.18.2", released: false } as ReleaseVersion,
		build: null,
		catalogs: {},
		tickets: [],
	});
	assert.equal(marks(r.stages), "oovo");
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
	});
	assert.equal(r.message, "ART has not staged it yet");
	assert.equal(r.pastDue, undefined);
});
