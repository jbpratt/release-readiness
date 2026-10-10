import assert from "node:assert/strict";
import { test } from "node:test";
import {
	formatReleaseName,
	jiraIssueUrl,
	jiraSearchUrl,
	konfluxUrl,
} from "./links.ts";

test("konfluxUrl", () => {
	const base =
		"https://konflux-ui.apps.kflux-ocp-p01.7ayg.p1.openshiftapps.com";
	const app = `${base}/ns/art-quay-tenant/applications/quay-3-18`;
	assert.equal(konfluxUrl(base, "art-quay-tenant", "quay-3-18"), app);
	assert.equal(konfluxUrl(`${base}/`, "art-quay-tenant", "quay-3-18"), app);
	assert.equal(
		konfluxUrl(
			base,
			"art-quay-tenant",
			"quay-3-18",
			"snapshots",
			"quay-3-18-20261010-013713-000",
		),
		`${app}/snapshots/quay-3-18-20261010-013713-000`,
	);
	assert.equal(
		konfluxUrl(
			base,
			"art-quay-tenant",
			"quay-3-18",
			"releases",
			"fbc-ri-stage-quay-3-18-quay-operator-ssrlx",
		),
		`${app}/releases/fbc-ri-stage-quay-3-18-quay-operator-ssrlx`,
	);
	assert.equal(konfluxUrl("", "art-quay-tenant", "quay-3-18"), null);
	assert.equal(konfluxUrl(base, "", "quay-3-18"), null);
	assert.equal(konfluxUrl(base, "art-quay-tenant", ""), null);
});

test("jiraIssueUrl", () => {
	assert.equal(
		jiraIssueUrl("PROJQUAY-1", "https://issues.redhat.com/"),
		"https://issues.redhat.com/browse/PROJQUAY-1",
	);
});

test("jiraSearchUrl", () => {
	assert.equal(
		jiraSearchUrl(
			'project=PROJQUAY AND "Target Version"="quay-v3.18.2"',
			"https://redhat.atlassian.net/",
		),
		"https://redhat.atlassian.net/issues/?jql=project%3DPROJQUAY%20AND%20%22Target%20Version%22%3D%22quay-v3.18.2%22",
	);
});

test("formatReleaseName", () => {
	assert.equal(formatReleaseName("quay-v3.18.0"), "Quay v3.18.0");
	assert.equal(formatReleaseName("omr-v2.0.10"), "OMR v2.0.10");
});
