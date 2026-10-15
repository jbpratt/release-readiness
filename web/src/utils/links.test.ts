import assert from "node:assert/strict";
import { test } from "node:test";
import {
	artPipelineUrl,
	commitUrl,
	formatReleaseName,
	jiraIssueUrl,
	jiraSearchUrl,
	konfluxUrl,
	sippyRunsUrl,
	ticketsJql,
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

test("ticketsJql", () => {
	assert.equal(
		ticketsJql("PROJQUAY", "quay-v3.18.1"),
		'project=PROJQUAY AND "Target Version"="quay-v3.18.1"',
	);
});

test("artPipelineUrl", () => {
	assert.equal(
		artPipelineUrl("art-quay-tenant"),
		"https://art-pipelines-ui-art-pipelines-ui.apps.artc2023.pc3z.p1.openshiftapps.com/#/pipeline/art-quay-tenant/build-layered-products?namespace=art-quay-tenant",
	);
});

test("formatReleaseName", () => {
	assert.equal(formatReleaseName("quay-v3.18.0"), "Quay v3.18.0");
	assert.equal(formatReleaseName("omr-v2.0.10"), "OMR v2.0.10");
});

test("sippyRunsUrl", () => {
	assert.equal(
		sippyRunsUrl("3.18.2", "2026-10-12T20:10:07Z"),
		"https://sippy.dptools.openshift.org/sippy-ng/jobs/quay-3.18/runs?filters=%7B%22items%22%3A%5B%7B%22field%22%3A%22timestamp%22%2C%22operator%22%3A%22%3E%22%2C%22value%22%3A%222026-10-12T20%3A10%3A07Z%22%7D%5D%7D",
	);
	assert.equal(sippyRunsUrl("3.18.2", ""), null);
	assert.equal(sippyRunsUrl("next", "2026-10-12T20:10:07Z"), null);
});

test("commitUrl", () => {
	const sha = "117e757e6abd2729d6ab4cfa93a2af674d47e1e4";
	assert.equal(
		commitUrl("quay-operator bundle", sha),
		`https://github.com/quay/quay-operator/commit/${sha}`,
	);
	assert.equal(commitUrl("redis-7", sha), null);
	assert.equal(commitUrl("quay-rhel9", ""), null);
});
