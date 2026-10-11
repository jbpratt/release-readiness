import assert from "node:assert/strict";
import { test } from "node:test";
import type { ImageScan, KonfluxRelease, StageFlag } from "../api/types.ts";
import {
	buildTitle,
	componentLabel,
	fixableLabels,
	jobLabels,
	jobShortName,
	missingCommits,
	notVerified,
	nvrLabel,
	releaseLine,
	scanCell,
	stageLines,
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

test("notVerified: Release Pending, Verified, Closed and Done, in any case, are verified", () => {
	const tickets = [
		"Release Pending",
		"VERIFIED",
		"closed",
		"Done",
		"ON_QA",
		"New",
	].map((status) => ({ status }));
	assert.equal(notVerified(tickets), 2);
});

test("buildTitle", () => {
	assert.equal(buildTitle(["candidate"], "staged"), "Staged build");
	assert.equal(
		buildTitle(["candidate", "newest"], "staged"),
		"Staged build · Latest",
	);
	assert.equal(buildTitle(["candidate", "newest"], "newest"), "Latest");
	assert.equal(buildTitle(["newest"], "staged"), "Latest");
	assert.equal(buildTitle(["last_tested"], "staged"), "Last tested");
});

const release = (r: Partial<KonfluxRelease>): KonfluxRelease => ({
	name: "fbc-ri-stage-quay-3-18-quay-operator-ssrlx",
	release_plan: "quay-advisory-stage-3-18",
	released_status: "False",
	released_reason: "Failed",
	failed_task: "verify-conforma",
	failed_step: "assert",
	created_at: "2026-10-10T01:49:22Z",
	...r,
});

test("releaseLine: a released prod Release is done", () => {
	assert.deepEqual(
		releaseLine(
			release({
				name: "quay-prod-3-18-1",
				released_status: "True",
				released_reason: "Succeeded",
			}),
		),
		{ status: "done", text: "Released", release: "quay-prod-3-18-1" },
	);
});

test("stageLines", () => {
	const app = "quay-3-18";
	const c = (name: string) => `${app}-${name}`;
	const flag = (f: Partial<StageFlag>): StageFlag => ({
		state: "not_staged",
		staged_at: null,
		total: 10,
		not_staged: [],
		release: null,
		no_bundle: [],
		no_release: [],
		...f,
	});
	assert.deepEqual(
		stageLines(
			flag({ state: "staged", staged_at: "2026-10-07T12:00:00Z" }),
			app,
		),
		[{ status: "done", text: "Staged Oct 7" }],
	);
	assert.deepEqual(stageLines(flag({ state: "unknown" }), app), [
		{ status: "none", text: "No stage Release for quay-3-18 yet" },
	]);
	// 3.18.1's newest build on Oct 10.
	const noBundle = [
		c("container-security-operator"),
		c("quay-bridge-operator"),
	];
	assert.deepEqual(
		stageLines(
			flag({
				not_staged: [
					...noBundle,
					c("quay-clair"),
					c("quay-operator"),
					c("quay-quay"),
				],
				release: release({}),
				no_bundle: noBundle,
			}),
			app,
		),
		[
			{
				status: "failed",
				text: "Release failed: Enterprise Contract policy (verify-conforma/assert)",
				release: "fbc-ri-stage-quay-3-18-quay-operator-ssrlx",
			},
			{
				status: "waiting",
				text: "container-security-operator, bridge-operator: built, no bundle yet",
				info: "ART builds a bundle only after a clean build-layered-products run.",
			},
		],
	);
	assert.deepEqual(
		stageLines(
			flag({
				not_staged: [c("quay-clair"), c("quay-quay")],
				release: release({ released_reason: "Progressing" }),
				no_release: [c("quay-clair")],
			}),
			app,
		),
		[
			{
				status: "running",
				text: "Release in progress",
				release: "fbc-ri-stage-quay-3-18-quay-operator-ssrlx",
			},
			{ status: "waiting", text: "clair: not in a stage Release yet" },
		],
	);
	assert.deepEqual(
		stageLines(
			flag({
				total: 2,
				not_staged: [c("quay-clair"), c("quay-quay")],
				no_release: [c("quay-clair"), c("quay-quay")],
			}),
			app,
		),
		[{ status: "waiting", text: "Not in a stage Release yet" }],
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
