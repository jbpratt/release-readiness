import assert from "node:assert/strict";
import { test } from "node:test";
import {
	CheckCircleIcon,
	CodeBranchIcon,
	CodeIcon,
	FlaskIcon,
	InProgressIcon,
	OutlinedCheckCircleIcon,
	OutlinedCircleIcon,
	QuestionCircleIcon,
} from "@patternfly/react-icons";
import type {
	BuildLabel,
	CandidateCI,
	CIRun,
	ImageScan,
	KonfluxRelease,
} from "../api/types.ts";
import {
	ciWarning,
	componentLabel,
	fbcCopy,
	fixableLabels,
	jobLabels,
	jobShortName,
	missingCommits,
	nvrLabel,
	releaseLine,
	scanCell,
	statusIcon,
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

test("statusIcon: each Jira status its icon, in any case, the verified ones green", () => {
	const want = [
		["New", OutlinedCircleIcon, false],
		["ASSIGNED", InProgressIcon, false],
		["In Progress", InProgressIcon, false],
		["POST", CodeBranchIcon, false],
		["MODIFIED", CodeIcon, false],
		["ON_QA", FlaskIcon, false],
		["on_qa", FlaskIcon, false],
		["Verified", OutlinedCheckCircleIcon, true],
		["Release Pending", OutlinedCheckCircleIcon, true],
		["Closed", CheckCircleIcon, true],
		["Done", CheckCircleIcon, true],
		["Refinement", QuestionCircleIcon, false],
	] as const;
	for (const [status, icon, verified] of want) {
		assert.deepEqual(statusIcon(status), { icon, verified }, status);
	}
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

test("fixableLabels", () => {
	const zero = { critical: 0, high: 0, medium: 0, low: 0, unknown: 0 };
	assert.deepEqual(
		fixableLabels({
			basis: "unique_cves",
			fixable: { critical: 1, high: 4, medium: 2, low: 3, unknown: 5 },
			no_fix: zero,
		}),
		[
			{ text: "1 critical", color: "red" },
			{ text: "4 high", color: "red" },
			{ text: "2 medium", color: "orange" },
			{ text: "3 low", color: "yellow" },
			{ text: "5 unknown", color: "grey" },
		],
	);
	assert.deepEqual(
		fixableLabels({
			basis: "scanner_arch_findings",
			fixable: { ...zero, high: 4 },
			no_fix: { ...zero, high: 102, medium: 684, low: 651 },
		}),
		[{ text: "4 high", color: "red" }],
	);
	assert.deepEqual(
		fixableLabels({
			basis: "scanner_arch_findings",
			fixable: zero,
			no_fix: { ...zero, high: 24 },
		}),
		[],
	);
});

test("scanCell", () => {
	const scan = (s: Partial<ImageScan>): ImageScan => ({
		state: "scan_failed",
		counts: null,
		url: "https://konflux.example/plr",
		...s,
	});
	assert.deepEqual(scanCell(scan({ state: "scan_failed" })), {
		text: "scan failed",
		color: "orange",
		url: "https://konflux.example/plr",
	});
	assert.deepEqual(scanCell(scan({ state: "not_scanned" })), {
		text: "not scanned",
		color: "grey",
		url: "https://konflux.example/plr",
	});
	assert.deepEqual(scanCell(scan({ state: "pending" })), {
		text: "-",
		color: "grey",
	});
	assert.deepEqual(scanCell(null), { text: "-", color: "grey" });
});

test("ciWarning", () => {
	const run = (state: string, started_at: string): CIRun => ({
		state,
		started_at,
		prow_url: "https://prow.example/1",
	});
	const latest: BuildLabel = {
		snapshot: "quay-3-18-20261009-042614-000",
		created_at: "2026-10-09T04:28:09Z",
	};
	const older: BuildLabel = {
		snapshot: "quay-3-18-20261008-131611-000",
		created_at: "2026-10-08T13:22:40Z",
	};
	const ci = (
		refresh: CandidateCI["refresh"],
		latest_in_ci: BuildLabel | null,
		passed: BuildLabel | null,
		...jobs: CIRun[][]
	): CandidateCI => ({
		since: "2026-10-06T17:10:27Z",
		builds: [
			{
				kind: "candidate",
				snapshot: "quay-stage-3-18-1-image-20261007200243",
				created_at: "2026-10-07T20:02:43Z",
				ci: null,
				jobs: jobs.map((runs, i) => ({ job_name: `job-${i}`, runs })),
			},
		],
		latest_in_ci,
		passed,
		fbc: null,
		refresh,
	});
	// A newer build in CI: the candidate's newest run of any job, and the
	// build named unless the action line names it.
	assert.deepEqual(
		ciWarning(
			ci(
				"current",
				latest,
				null,
				[run("success", "2026-10-07T23:49:38Z")],
				[
					run("success", "2026-10-09T00:56:39Z"),
					run("failure", "2026-10-07T00:56:56Z"),
				],
			),
		),
		{ lastRun: "2026-10-09T00:56:39Z", namesLatest: true },
	);
	for (const [refresh, passed, namesLatest] of [
		["waiting", null, false],
		["rerun", latest, false],
		["rerun", older, true],
	] as const) {
		assert.deepEqual(
			ciWarning(ci(refresh, latest, passed)),
			{ lastRun: null, namesLatest },
			`${refresh}, passed ${passed?.snapshot}`,
		);
	}
	// None newer: only whether a run that installed the candidate passed.
	assert.equal(
		ciWarning(
			ci("current", null, null, [run("failure", "2026-10-08T00:56:37Z")]),
		),
		"no_pass",
	);
	assert.equal(
		ciWarning(
			ci(
				"current",
				null,
				null,
				[run("failure", "2026-10-08T00:56:37Z")],
				[run("success", "2026-10-07T23:49:38Z")],
			),
		),
		undefined,
	);
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
