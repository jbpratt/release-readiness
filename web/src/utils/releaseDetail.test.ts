import assert from "node:assert/strict";
import { test } from "node:test";
import type { ImageScan } from "../api/types.ts";
import {
	componentLabel,
	jobLabels,
	jobShortName,
	missingCommits,
	notVerified,
	nvrLabel,
	scanCell,
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

test("scanCell", () => {
	const scan = (s: Partial<ImageScan>): ImageScan => ({
		state: "scanned",
		counts: null,
		url: "https://konflux.example/plr",
		...s,
	});
	const zero = { critical: 0, high: 0, medium: 0, low: 0, unknown: 0 };
	assert.deepEqual(
		scanCell(
			scan({
				counts: {
					basis: "scanner_arch_findings",
					fixable: zero,
					no_fix: { ...zero, high: 24, medium: 264, low: 232 },
				},
			}),
		),
		{
			text: "0 fixable",
			color: undefined,
			url: "https://konflux.example/plr",
			detail: [
				"Fixable: 0 critical, 0 high, 0 medium, 0 low, 0 unknown",
				"No fix: 0 critical, 24 high, 264 medium, 232 low, 0 unknown",
				"Counted per architecture",
			],
		},
	);
	const fixable = scanCell(
		scan({
			counts: {
				basis: "unique_cves",
				fixable: { ...zero, high: 4, unknown: 1 },
				no_fix: zero,
			},
		}),
	);
	assert.deepEqual(
		[fixable.text, fixable.color, fixable.detail?.[2]],
		["5 fixable", "red", "Unique CVEs across architectures"],
	);
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
