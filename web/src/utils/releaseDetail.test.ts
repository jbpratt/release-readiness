import assert from "node:assert/strict";
import { test } from "node:test";
import type { CandidateCI, CIBuild, KonfluxRelease } from "../api/types.ts";
import {
	ciStep,
	componentLabel,
	fbcCopy,
	jobLabels,
	jobShortName,
	missingCommits,
	notComparedLines,
	nvrLabel,
	releaseLine,
	runsStatus,
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

test("missingCommits: one link per commit, titled with the components lacking it", () => {
	const commit = (component: string, sha: string) => ({
		component: `quay-3-18-${component}`,
		commit_sha: sha,
		commit_url: `https://github.com/quay/x/commit/${sha}`,
	});
	assert.deepEqual(
		missingCommits(
			[
				commit("quay-operator", "aaa"),
				commit("quay-operator-bundle", "aaa"),
				commit("quay-operator", "bbb"),
			],
			"quay-3-18",
		),
		[
			{
				sha: "aaa",
				url: "https://github.com/quay/x/commit/aaa",
				title: "operator, operator-bundle",
			},
			{
				sha: "bbb",
				url: "https://github.com/quay/x/commit/bbb",
				title: "operator",
			},
		],
	);
	assert.deepEqual(missingCommits([], "quay-3-18"), []);
});

test("notComparedLines: GitHub API errors folded into one line", () => {
	const nc = (component: string, reason: string) => ({
		component: `quay-3-18-${component}`,
		reason,
	});
	const fold = "Not compared with GitHub: API error (retrying)";
	assert.deepEqual(
		notComparedLines(
			[
				nc("quay-quay", "github api error"),
				nc("quay-clair", "default branch: github api error"),
			],
			"quay-3-18",
		),
		[fold],
	);
	assert.deepEqual(
		notComparedLines(
			[
				nc("quay-builder", "upstream repo is not on github.com"),
				nc("quay-clair", "default branch: github api error"),
				nc("quay-quay", "not scanned yet"),
			],
			"quay-3-18",
		),
		[
			"Not compared: builder (upstream repo is not on github.com), quay (not scanned yet)",
			fold,
		],
	);
	assert.deepEqual(notComparedLines([], "quay-3-18"), []);
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
	release_plan: "rp",
	released_status: "",
	released_reason: "",
	created_at: "2026-10-08T09:00:00Z",
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
		since: "2026-10-06T17:10:27Z",
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

test("fbcCopy: the pullspecs, comma-separated, unless one is missing or unread", () => {
	const ci = (fbc: CandidateCI["fbc"]): CandidateCI => ({
		since: "2026-10-06T17:10:27Z",
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
