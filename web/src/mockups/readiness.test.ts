import assert from "node:assert/strict";
import { test } from "node:test";
import type {
	BuildTicket,
	KonfluxRelease,
	ReleaseSnapshot,
	ReleaseVersion,
	StagedSnapshot,
} from "../api/types.ts";
import { catalogRow, dueText, ocpRows, readiness } from "./readiness.ts";

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
const marks = (rows: { mark: string }[]) => rows.map((r) => r.mark).join("");

test("3.18.1: tickets block, and the production release failed", () => {
	const release = {
		name: "quay-v3.18.1",
		released: false,
		due_date: "2026-08-20T00:00:00Z",
	} as ReleaseVersion;
	const prod = {
		name: "quay-prod-3-18-1-image-20261009133455",
		releases: [
			{
				released_status: "False",
				released_reason: "Failed",
				failed_task: "verify-conforma",
				failed_step: "assert",
				created_at: "2026-10-09T13:34:55Z",
				completion_time: "2026-10-09T13:39:03Z",
			},
		],
	} as ReleaseSnapshot;
	const input = {
		release,
		build: staged(
			"quay-stage-3-18-1-image-20261007200243",
			"2026-10-07T20:14:57Z",
		),
		catalogs: [
			catalogRow(
				"quay-operator",
				staged("quay-stage-3-18-1-fbc-20261007222921", "2026-10-07T22:32:26Z"),
				"3.18.1",
			),
		],
		tickets: [
			...tickets(42, "quay-v3.18.1", "MODIFIED"),
			...tickets(19, "quay-v3.18.1", "ON_QA"),
			...tickets(7, "quay-v3.18.1", "Release Pending"),
			...tickets(14, "quay-v3.18.z", "MODIFIED"),
		],
		inCatalog: false,
		prod,
	};
	const { rows, verdict } = readiness(input);
	assert.equal(marks(rows), "vvxx");
	assert.deepEqual(
		rows.map((r) => r.text),
		[
			"quay-stage-3-18-1-image-20261007200243 staged Oct 7 20:14Z.",
			"quay-operator catalog staged Oct 7 22:32Z.",
			"61 of 68 not verified (42 MODIFIED, 19 ON_QA).",
			"Production release failed at verify-conforma/assert, Oct 9 13:39Z.",
		],
	);
	assert.deepEqual(verdict, {
		color: "red",
		text: "Not ready: 61 tickets not verified",
	});
	// Q4-B reads only the catalog and Jira.
	const q4b = readiness({ ...input, prod: undefined });
	assert.equal(marks(q4b.rows), "vvxo");
	assert.equal(q4b.verdict.text, "Not ready: 61 tickets not verified");

	const now = Date.parse("2026-10-09T21:50:00Z");
	assert.equal(
		dueText(release.due_date, now, true),
		"Due Aug 20, 2026 (50 days late)",
	);
	assert.equal(dueText(release.due_date, now, false), "Due Aug 20, 2026");

	const ocp = (operator: string, version: string) => ({
		version: "3.18.1",
		operator,
		ocp: version,
		name: "",
		created_at: "",
		status: "Succeeded",
	});
	assert.deepEqual(
		ocpRows(
			[
				ocp("container-security-operator", "v5.0"),
				ocp("quay-operator", "v4.12"),
				ocp("quay-operator", "v5.0"),
			],
			"3.18.1",
		).map((r) => r.text),
		[
			"OCP v4.12: quay-operator.",
			"OCP v5.0: quay-operator, container-security-operator.",
		],
	);
});

test("3.17.6: nothing blocks, so the verdict names what is next", () => {
	const { rows, verdict } = readiness({
		release: { name: "quay-v3.17.6", released: false } as ReleaseVersion,
		build: staged(
			"quay-stage-3-17-6-image-20261008082443",
			"2026-10-08T08:36:45Z",
		),
		catalogs: [
			catalogRow(
				"quay-operator",
				staged("quay-stage-3-17-6-fbc-20261008084029", "2026-10-08T09:25:03Z"),
				"3.17.6",
			),
		],
		tickets: tickets(18, "quay-v3.17.z", "MODIFIED"),
		inCatalog: false,
		prod: null,
	});
	assert.equal(marks(rows), "vvvo");
	assert.equal(
		rows[2].text,
		"No ticket targets 3.17.6. The list below has the build's 18 .z tickets.",
	);
	assert.equal(rows[3].text, "No production release yet.");
	assert.deepEqual(verdict, {
		color: "blue",
		text: "Next: the production release",
	});
});

test("not started: ART has staged nothing", () => {
	const { rows, verdict } = readiness({
		release: { name: "quay-v3.18.2", released: false } as ReleaseVersion,
		build: null,
		catalogs: [catalogRow("quay-operator", null, "3.18.2")],
		tickets: [],
		inCatalog: false,
		prod: null,
	});
	assert.equal(marks(rows), "oovo");
	assert.equal(rows[0].text, "ART has not staged the build for 3.18.2 yet.");
	assert.deepEqual(verdict, {
		color: "blue",
		text: "Next: ART stages the build",
	});
});
