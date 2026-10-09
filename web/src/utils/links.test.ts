import assert from "node:assert/strict";
import { test } from "node:test";
import {
	formatReleaseName,
	jiraIssueUrl,
	quayManifestUrl,
	upstreamCommitUrl,
} from "./links.ts";

const digest = `sha256:${"ab".repeat(32)}`;

test("quayManifestUrl", () => {
	assert.equal(
		quayManifestUrl(
			`quay.io/redhat-user-workloads/ocp-art-tenant/art-images@${digest}`,
		),
		`https://quay.io/repository/redhat-user-workloads/ocp-art-tenant/art-images/manifest/${digest}`,
	);
	assert.equal(
		quayManifestUrl(`quay.io/projectquay/quay@${digest}`),
		`https://quay.io/repository/projectquay/quay/manifest/${digest}`,
	);
	assert.equal(quayManifestUrl("quay.io/projectquay/quay:v3.18.0"), null);
	assert.equal(quayManifestUrl(`registry.redhat.io/quay/quay@${digest}`), null);
});

test("upstreamCommitUrl", () => {
	const sha = "35cf767efc3b4e7bebc7802afcf12b60cd343bc0";
	assert.equal(
		upstreamCommitUrl("https://github.com/quay/quay-operator", sha),
		`https://github.com/quay/quay-operator/commit/${sha}`,
	);
	assert.equal(
		upstreamCommitUrl("https://github.com/quay/quay-operator.git/", sha),
		`https://github.com/quay/quay-operator/commit/${sha}`,
	);
	assert.equal(
		upstreamCommitUrl("https://github.com/quay/quay-operator", ""),
		null,
	);
	assert.equal(upstreamCommitUrl("", sha), null);
	assert.equal(upstreamCommitUrl("https://gitlab.com/quay/quay", sha), null);
	assert.equal(upstreamCommitUrl("https://github.com/quay", sha), null);
});

test("jiraIssueUrl", () => {
	assert.equal(
		jiraIssueUrl("PROJQUAY-1", "https://issues.redhat.com/"),
		"https://issues.redhat.com/browse/PROJQUAY-1",
	);
});

test("formatReleaseName", () => {
	assert.equal(formatReleaseName("quay-v3.18.0"), "Quay v3.18.0");
	assert.equal(formatReleaseName("omr-v2.0.10"), "OMR v2.0.10");
});
