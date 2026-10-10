import assert from "node:assert/strict";
import { test } from "node:test";
import type { ImageScan } from "../api/types.ts";
import { jobShortName, notVerified, scanCell } from "./releaseDetail.ts";

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
		jobShortName("periodic-ci-quay-quay-tests-master-e2e"),
		"periodic-ci-quay-quay-tests-master-e2e",
	);
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
