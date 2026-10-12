import assert from "node:assert/strict";
import { test } from "node:test";
import type {
	CandidateCI,
	CIBuild,
	KonfluxRelease,
	NewestBuild,
} from "../api/types.ts";
import {
	artGroup,
	ciStep,
	componentLabel,
	fbcCopy,
	fbcParams,
	jobLabels,
	jobShortName,
	newestCIStep,
	nvrLabel,
	releaseLine,
	runsStatus,
	stepperBuild,
	tknCommand,
} from "./releaseDetail.ts";

test("jobShortName", () => {
	assert.equal(
		jobShortName(
			"periodic-ci-quay-quay-redhat-3.18-aws-ocp422-e2e-install-aws-s3-nightly",
		),
		"aws-s3",
	);
	assert.equal(
		jobShortName(
			"periodic-ci-quay-quay-redhat-3.18-gcp-ocp422-e2e-install-gcp-gcs",
		),
		"gcp-gcs",
	);
	assert.equal(
		jobShortName(
			"periodic-ci-quay-quay-redhat-3.18-aws-ocp422-e2e-install-aws-s3-nightly-fips",
		),
		"aws-s3-fips",
	);
	assert.equal(
		jobShortName("periodic-ci-quay-quay-tests-master-e2e"),
		"periodic-ci-quay-quay-tests-master-e2e",
	);
});

test("jobLabels", () => {
	const job = (name: string) => `periodic-ci-quay-quay-redhat-3.18-${name}`;
	assert.deepEqual(
		jobLabels([
			job("aws-ocp422-e2e-install-aws-odf-nightly"),
			job("aws-ocp422-e2e-install-aws-s3-nightly"),
			job("aws-ocp422-e2e-install-aws-s3-nightly-fips"),
		]),
		["aws-odf", "aws-s3", "aws-s3-fips"],
	);
	assert.deepEqual(
		jobLabels([
			job("aws-ocp414-e2e-install-aws-s3-nightly"),
			job("aws-ocp422-e2e-install-aws-s3-nightly"),
			job("aws-ocp422-e2e-install-aws-s3-nightly-fips"),
			job("gcp-ocp422-e2e-install-gcp-gcs-nightly"),
		]),
		["aws-ocp414-aws-s3", "aws-ocp422-aws-s3", "aws-s3-fips", "gcp-gcs"],
	);
	assert.deepEqual(
		jobLabels([
			"periodic-ci-quay-quay-master-e2e-install-aws-s3",
			job("aws-ocp422-e2e-install-aws-s3-nightly"),
		]),
		["periodic-ci-quay-quay-master-e2e-install-aws-s3", "aws-ocp422-aws-s3"],
	);
});

test("runsStatus: green when every counted run passed, red when none did, yellow when both", () => {
	const want: [string[], ReturnType<typeof runsStatus>][] = [
		[[], undefined],
		[["success", "success"], "success"],
		[["failure", "error"], "danger"],
		[["error"], "danger"],
		[["success", "success", "failure"], "warning"],
		[["aborted"], undefined],
		[["success", "aborted"], "success"],
	];
	for (const [states, status] of want) {
		assert.equal(runsStatus(states), status, `[${states}]`);
	}
});

test("componentLabel", () => {
	assert.equal(componentLabel("quay-3-18-quay-quay", "quay-3-18"), "quay");
	assert.equal(
		componentLabel("quay-3-18-quay-operator-bundle", "quay-3-18"),
		"operator-bundle",
	);
	assert.equal(
		componentLabel("quay-3-18-container-security-operator", "quay-3-18"),
		"container-security-operator",
	);
});

test("nvrLabel", () => {
	assert.equal(
		nvrLabel(
			"quay-quay-container-3.18.1-202610060528.p2.g1148474.assembly.stream.el9",
		),
		"3.18.1-202610060528.p2.g1148474",
	);
	assert.equal(
		nvrLabel(
			"quay-operator-metadata-container-3.18.1.202610061637.p2.g35cf767.assembly.stream.el9-1",
		),
		"3.18.1.202610061637.p2.g35cf767-1",
	);
	assert.equal(nvrLabel("quay-builder-3.18.1-1"), "quay-builder-3.18.1-1");
});

const release = (r: Partial<KonfluxRelease>): KonfluxRelease => ({
	name: "r1",
	released_status: "",
	released_reason: "",
	...r,
});

test("releaseLine", () => {
	const failed = (failed_task: string, failed_step?: string) =>
		releaseLine(
			release({
				released_status: "False",
				released_reason: "Failed",
				failed_task,
				failed_step,
			}),
		);
	const running = { status: "running", text: "Release in progress" };
	assert.deepEqual(
		releaseLine(
			release({
				released_status: "False",
				released_reason: "Progressing",
				failed_task: "verify-conforma",
			}),
		),
		running,
	);
	assert.deepEqual(releaseLine(release({})), running);
	assert.deepEqual(failed("verify-conforma", "assert"), {
		status: "failed",
		text: "Release failed: Enterprise Contract policy (verify-conforma/assert)",
	});
	assert.deepEqual(failed("verify-conforma"), {
		status: "failed",
		text: "Release failed: Enterprise Contract policy (verify-conforma)",
	});
	assert.deepEqual(failed("collect-data", "x"), {
		status: "failed",
		text: "Release failed at collect-data/x",
	});
	assert.deepEqual(
		releaseLine(
			release({ released_status: "True", released_reason: "Succeeded" }),
		),
		{ status: "done", text: "Released" },
	);
});

test("ciStep: the candidate row's runs, as runsStatus counts them", () => {
	const row = (kind: CIBuild["kind"], ...jobs: string[][]): CIBuild => ({
		kind,
		snapshot: "quay-stage-3-18-1-image-20261007200243",
		created_at: "2026-10-07T20:02:43Z",
		ci: null,
		jobs: jobs.map((states, i) => ({
			job_name: `job-${i}`,
			runs: states.map((state) => ({
				state,
				started_at: "2026-10-08T00:56:37Z",
				prow_url: "https://prow.example/1",
			})),
		})),
	});
	const ci = (...builds: CIBuild[]): CandidateCI => ({
		builds,
		latest_in_ci: null,
		passed: null,
		fbc: null,
		refresh: "current",
	});
	const notTested: ReturnType<typeof ciStep> = {
		variant: "warning",
		text: "Not tested",
	};
	const want: [string, CandidateCI, ReturnType<typeof ciStep>][] = [
		["no candidate row", ci(), notTested],
		["a candidate row without jobs", ci(row("candidate")), notTested],
		[
			"success, success",
			ci(row("candidate", ["success", "success"])),
			{ variant: "success", text: "Passed" },
		],
		[
			"failure, error",
			ci(row("candidate", ["failure", "error"])),
			{ variant: "danger", text: "Failed" },
		],
		[
			"a mix across three jobs",
			ci(
				row(
					"candidate",
					["success", "success"],
					["failure", "failure"],
					["success", "failure"],
				),
			),
			{ variant: "warning", text: "3 of 6 runs passed" },
		],
		[
			"success, aborted",
			ci(row("candidate", ["success", "aborted"])),
			{ variant: "success", text: "Passed" },
		],
		[
			"success, failure, aborted",
			ci(row("candidate", ["success", "failure", "aborted"])),
			{ variant: "warning", text: "1 of 2 runs passed" },
		],
		[
			"a passing run only on a non-candidate row",
			ci(row("build", ["success"]), row("candidate")),
			notTested,
		],
	];
	for (const [name, c, step] of want) {
		assert.deepEqual(ciStep(c), step, name);
	}
});

test("newestCIStep: the runs of the newest build's row, else its CI state", () => {
	const newest = (
		state: NewestBuild["ci"]["state"],
		r?: string,
	): NewestBuild => ({
		snapshot: "quay-3-16-20261012-034719-000-c0",
		created_at: "2026-10-12T03:49:33Z",
		ci: { state, release: r ? release({ name: r }) : null },
	});
	const row = (
		snapshot: string,
		r: string | null,
		...states: string[]
	): CIBuild => ({
		kind: "build",
		snapshot,
		created_at: "2026-10-11T03:49:33Z",
		ci: { state: "in", release: r ? release({ name: r }) : null },
		jobs: [
			{
				job_name: "job",
				runs: states.map((state) => ({
					state,
					started_at: "2026-10-12T05:00:00Z",
					prow_url: "https://prow.example/1",
				})),
			},
		],
	});
	const ci = (...builds: CIBuild[]): CandidateCI => ({
		builds,
		latest_in_ci: null,
		passed: null,
		fbc: null,
		refresh: "none",
	});
	const want: [
		string,
		NewestBuild,
		CandidateCI | null,
		ReturnType<typeof newestCIStep>,
	][] = [
		[
			"unknown, no CI",
			newest("unknown"),
			null,
			{ variant: "pending", text: "No CI runs" },
		],
		["none", newest("none"), ci(), { variant: "pending", text: "Not in CI" }],
		[
			"in, no runs",
			newest("in", "r1"),
			ci(),
			{ variant: "info", text: "In CI" },
		],
		[
			"failed",
			newest("failed", "r1"),
			ci(),
			{ variant: "danger", text: "Failed to reach CI" },
		],
		[
			"its row by snapshot",
			newest("in", "r1"),
			ci(row("quay-3-16-20261012-034719-000-c0", null, "failure")),
			{ variant: "danger", text: "Failed" },
		],
		[
			"its row by stage Release, labeled by an older snapshot",
			newest("in", "r1"),
			ci(row("quay-3-16-20261011-034719-000-c0", "r1", "success")),
			{ variant: "success", text: "Passed" },
		],
		[
			"another build's row",
			newest("in", "r1"),
			ci(row("quay-3-16-20261010-034719-000-c0", "r0", "success")),
			{ variant: "info", text: "In CI" },
		],
	];
	for (const [name, n, c, step] of want) {
		assert.deepEqual(newestCIStep(n, c), step, name);
	}
});

test("stepperBuild: the newest build, else the passed one with its row's CI state", () => {
	const snap = "quay-3-16-20261011-034719-000-c0";
	const state = { state: "in" as const, release: release({ name: "r1" }) };
	const ci = (passed: boolean): CandidateCI => ({
		builds: [
			{
				kind: "build",
				snapshot: snap,
				created_at: "2026-10-11T03:49:33Z",
				ci: state,
				jobs: [],
			},
		],
		latest_in_ci: null,
		passed: passed
			? { snapshot: snap, created_at: "2026-10-11T03:49:33Z" }
			: null,
		fbc: null,
		refresh: passed ? "pick" : "none",
	});
	const newest: NewestBuild = {
		snapshot: "quay-3-16-20261012-034719-000-c0",
		created_at: "2026-10-12T03:49:33Z",
		ci: { state: "none", release: null },
	};
	assert.equal(stepperBuild(newest, ci(true)), newest);
	assert.deepEqual(stepperBuild(null, ci(true)), {
		snapshot: snap,
		created_at: "2026-10-11T03:49:33Z",
		ci: state,
	});
	assert.equal(stepperBuild(null, ci(false)), null);
	assert.equal(stepperBuild(null, null), null);
});

test("fbcCopy: the pullspecs, comma-separated, unless one is missing or unread", () => {
	const ci = (fbc: CandidateCI["fbc"]): CandidateCI => ({
		builds: [],
		latest_in_ci: null,
		passed: {
			snapshot: "quay-3-18-20261009-042614-000",
			created_at: "2026-10-09T04:28:09Z",
		},
		fbc,
		refresh: "rerun",
	});
	const pullspecs = [
		"quay.io/redhat-user-workloads/ocp-art-tenant/art-fbc@sha256:52b9f23c5edd4a1097cc6e3e0fac64e5de5ff68af76af97395397b1a39f20c9e",
		"quay.io/redhat-user-workloads/ocp-art-tenant/art-fbc@sha256:be4568f034f763465e06852349a4ebd7153f251d04d4a1b4e27b83e059c8f792",
	];
	assert.equal(
		fbcCopy(ci({ pullspecs, missing: [] })),
		`${pullspecs[0]},${pullspecs[1]}`,
	);
	assert.equal(
		fbcCopy(
			ci({
				pullspecs,
				missing: [
					{
						operator: "quay-bridge-operator",
						ocp: "4.12",
						url: "https://art.example/search?group=quay-3.18&name=quay-bridge-operator-fbc&assembly=stream",
					},
				],
			}),
		),
		null,
	);
	assert.equal(fbcCopy(ci(null)), null);
});

test("artGroup: the application's X.Y dotted", () => {
	assert.equal(artGroup("quay-3-17"), "quay-3.17");
	assert.equal(artGroup("quay-3-9"), "quay-3.9");
});

test("fbcParams and tknCommand: 3.17.6 with and without FBC pullspecs", () => {
	const fbc = "quay.io/a/art-fbc@sha256:1,quay.io/a/art-fbc@sha256:2";
	assert.deepEqual(
		fbcParams(
			"quay-3-17",
			"3.17.6",
			fbc,
			["PROJQUAY-1", "PROJQUAY-2"],
			"2026-10-15T00:00:00Z",
		),
		{
			group: "quay-3.17",
			assembly: "3.17.6",
			"fbc-pullspecs": fbc,
			"jira-bugs": "PROJQUAY-1,PROJQUAY-2",
			"target-release-date": "2026-10-15",
			"dry-run": "true",
		},
	);
	assert.equal(
		tknCommand(fbcParams("quay-3-17", "3.17.6", null, [])),
		"tkn pipeline start release-from-fbc -n art-quay-tenant --use-param-defaults -p 'group=quay-3.17' -p 'assembly=3.17.6' -p 'fbc-pullspecs=<fbc-pullspecs>' -p 'dry-run=true'",
	);
});
