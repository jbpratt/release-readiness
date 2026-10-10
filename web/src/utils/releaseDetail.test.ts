import assert from "node:assert/strict";
import { test } from "node:test";
import { jobShortName, notVerified } from "./releaseDetail.ts";

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
