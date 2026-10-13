import assert from "node:assert/strict";
import { test } from "node:test";
import type {
	CIJob,
	FBCCandidate,
	FBCMember,
	KonfluxRelease,
	OperatorFBC,
} from "../api/types.ts";
import {
	artGroup,
	bundleBuilt,
	compareVersions,
	componentLabel,
	fbcBuildStep,
	fbcCIStep,
	fbcParams,
	fbcPullspecs,
	fbcStageStep,
	imageCheckLine,
	imagesState,
	jobLabels,
	jobShortName,
	nvrLabel,
	operatorSummary,
	pullspecsBlocked,
	readingLine,
	releaseLine,
	runsStatus,
	shipsVersion,
	slotRows,
	streamAhead,
	tknCommand,
	verdictLine,
	whileChecking,
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

const job = (...states: string[]): CIJob => ({
	job_name:
		"periodic-ci-quay-quay-redhat-3.17-aws-ocp422-e2e-install-aws-s3-nightly",
	runs: states.map((state, i) => ({
		state,
		started_at: "2026-10-12T00:00:00Z",
		prow_url: `https://prow.example/${i}`,
	})),
});

const bundle = (built: string) =>
	`quay-operator-metadata-container-3.17.6.${built}.p2.g35cf767.assembly.stream.el9-1`;

const fbcMember = (
	ocp: string,
	head: string,
	m: Partial<FBCMember> = {},
): FBCMember => ({
	ocp,
	digest: `sha256:${ocp}-${head}`,
	pullspec: `quay.io/art-fbc@sha256:${ocp}-${head}`,
	nvr: `quay-operator-fbc-3.17.6-20261012000000.ocp${ocp}`,
	build_url: "",
	head_bundle_nvr: head,
	snapshot: "",
	release: null,
	ci: { exact: [], equivalent: [] },
	...m,
});

const operatorFBC = (
	operator: string,
	head: string,
	members: FBCMember[],
	o: Partial<OperatorFBC> = {},
): OperatorFBC => ({
	operator,
	head_bundle_nvr: head,
	channel: "stable-3.17",
	head_csv: "",
	coverage: { members: members.length, slots: members.length },
	members,
	missing_ocp: [],
	conflicts: [],
	fbc_ri_stage: { state: "none", release: null },
	image_check: { state: "absent", snapshot: "", bundle_nvr: "" },
	images: [],
	compare: [],
	stage: { staged: 0, total: 0 },
	...o,
});

const fbcCandidate = (c: Partial<FBCCandidate>): FBCCandidate => ({
	source: "stage_snapshots",
	state: "complete",
	errors: [],
	operators: [],
	unknown: [],
	pullspecs: [],
	installable: false,
	...c,
});

test("stream only, no stage (3.16): not staged, pullspecs from the stream", () => {
	const head =
		"quay-operator-metadata-container-3.16.7.202610121915.p2.ga4fe015.assembly.stream.el9-1";
	const members = ["4.12", "4.14"].map((v) => fbcMember(v, head));
	const c = fbcCandidate({
		source: "stream",
		operators: ["quay-operator", "container-security-operator"].map((o) =>
			operatorFBC(o, head, members, {
				channel: "stable-3.16",
				head_csv: "quay-operator.v3.99.0-dev",
			}),
		),
		pullspecs: [members[0].pullspec, members[1].pullspec],
	});
	assert.deepEqual(fbcStageStep(c), {
		variant: "pending",
		text: "Not in a stage shipment",
	});
	assert.deepEqual(fbcCIStep(c), {
		variant: "warning",
		text: "No CI ran on these FBCs",
	});
	assert.equal(
		fbcPullspecs(c),
		"quay.io/art-fbc@sha256:4.12-" +
			head +
			",quay.io/art-fbc@sha256:4.14-" +
			head,
	);
	assert.equal(streamAhead(c, c, "quay-operator"), null);
});

test("missing operator (3.17.6): CSO and QBO not in stage show the stream's FBCs", () => {
	const head = bundle("202610060936");
	const released = release({ name: "r", released_status: "True" });
	const qo = operatorFBC(
		"quay-operator",
		head,
		["4.12", "4.13", "4.14"].map((v) =>
			fbcMember(v, head, { snapshot: "s", release: released }),
		),
	);
	const cso = operatorFBC("container-security-operator", "", [], {
		missing_ocp: ["4.14", "4.12"],
		coverage: { members: 0, slots: 2 },
	});
	const errors = [
		"container-security-operator is not in stage",
		"quay-bridge-operator is not in stage",
	];
	const c = fbcCandidate({
		state: "incomplete",
		errors,
		operators: [qo, cso, operatorFBC("quay-bridge-operator", "", [])],
	});
	const streamCSO = operatorFBC(
		"container-security-operator",
		bundle("202610121613"),
		["4.12", "4.14"].map((v) => fbcMember(v, bundle("202610121613"))),
	);
	const streamQBO = operatorFBC(
		"quay-bridge-operator",
		bundle("202610120313"),
		[fbcMember("4.12", bundle("202610120313"))],
	);
	const stream = fbcCandidate({
		source: "stream",
		operators: [qo, streamCSO, streamQBO],
	});
	assert.equal(fbcPullspecs(c), null);
	assert.equal(pullspecsBlocked(c), errors.join("; "));
	assert.deepEqual(fbcBuildStep(stream), {
		variant: "success",
		text: "3 of 3 operators built",
		fbcs: "6 of 6 FBCs",
	});
	assert.deepEqual(fbcStageStep(c), {
		variant: "warning",
		text: "1 of 3 operators staged",
	});
	assert.deepEqual(slotRows(cso), [
		{ ocp: "4.12", kind: "missing", fbc: null },
		{ ocp: "4.14", kind: "missing", fbc: null },
	]);
	const summary = operatorSummary(c, stream);
	assert.deepEqual(summary.ocps, ["4.12", "4.13", "4.14"]);
	assert.deepEqual(
		summary.rows.map((r) => [r.operator, r.cells]),
		[
			[qo, ["staged", "staged", "staged"]],
			[streamCSO, ["built", "absent", "built"]],
			[streamQBO, ["built", "absent", "absent"]],
		],
	);
});

test("restaged slots (3.18.1): a failed latest Release fails the Stage step", () => {
	const head = bundle("202610090339");
	const newer = bundle("202610121915");
	const failed = release({ name: "f", released_reason: "Failed" });
	const ok = release({ name: "ok", released_status: "True" });
	const staged = operatorFBC("quay-operator", head, [
		fbcMember("4.14", head, { snapshot: "s2", release: failed }),
		fbcMember("4.15", head, { snapshot: "s1", release: ok }),
	]);
	const c = fbcCandidate({ operators: [staged] });
	assert.deepEqual(fbcStageStep(c), {
		variant: "danger",
		text: "1 of 2 FBCs released · 1 failed",
	});
	assert.deepEqual(
		fbcStageStep(
			fbcCandidate({
				operators: [
					operatorFBC("quay-operator", head, [
						fbcMember("4.14", head, { snapshot: "s2" }),
						fbcMember("4.15", head, { snapshot: "s1", release: ok }),
					]),
				],
			}),
		),
		{ variant: "info", text: "1 of 2 FBCs released" },
	);
	assert.deepEqual(
		fbcStageStep(
			fbcCandidate({
				operators: [
					operatorFBC("quay-operator", head, [
						fbcMember("4.14", head, { snapshot: "s2", release: ok }),
					]),
				],
			}),
		),
		{ variant: "success", text: "1 of 1 FBCs released" },
	);
	const stream = fbcCandidate({
		source: "stream",
		operators: [operatorFBC("quay-operator", newer, [])],
	});
	assert.equal(streamAhead(c, stream, "quay-operator"), newer);
	assert.equal(streamAhead(c, c, "quay-operator"), null);
});

test("mixed slots (CSO 7 on ...0313, 2 on ...1613): inconsistent, the others in OCP order", () => {
	const head = bundle("202610121613");
	const other = bundle("202610120313");
	const all = [
		"4.12",
		"4.13",
		"4.14",
		"4.15",
		"4.16",
		"4.17",
		"4.18",
		"4.19",
		"4.20",
	];
	const cso = operatorFBC(
		"container-security-operator",
		head,
		["4.19", "4.20"].map((v) => fbcMember(v, head)),
		{
			conflicts: all.slice(0, 7).map((v) => fbcMember(v, other)),
			coverage: { members: 2, slots: 9 },
		},
	);
	const c = fbcCandidate({
		state: "inconsistent",
		errors: [
			"container-security-operator heads more than one bundle across its OCP versions",
		],
		operators: [cso],
		pullspecs: ["x"],
	});
	assert.equal(fbcPullspecs(c), null);
	const rows = slotRows(cso);
	assert.deepEqual(
		rows.map((r) => `${r.ocp} ${r.kind}`),
		all.map((v, i) => `${v} ${i < 7 ? "other" : "member"}`),
	);
});

test("unresolved heads: slot and image check", () => {
	const o = operatorFBC("quay-operator", "", [], {
		conflicts: [fbcMember("4.14", "")],
	});
	assert.deepEqual(
		slotRows(o).map((r) => r.kind),
		["unresolved"],
	);
	assert.equal(imageCheckLine(o), "No image Snapshot to compare");
});

test("fbcCIStep: exact runs decide; else runs on an equivalent FBC", () => {
	const head = bundle("202610090936");
	const with_ = (ci: FBCMember["ci"]) =>
		fbcCandidate({
			operators: [
				operatorFBC("quay-operator", head, [fbcMember("4.14", head, { ci })]),
			],
		});
	assert.deepEqual(
		fbcCIStep(
			with_({
				exact: [job("success", "failure")],
				equivalent: [job("success")],
			}),
		),
		{ variant: "warning", text: "1 of 2 runs passed" },
	);
	assert.deepEqual(
		fbcCIStep(with_({ exact: [], equivalent: [job("success")] })),
		{
			variant: "warning",
			text: "Passed on an equivalent FBC (same bundle)",
		},
	);
	assert.deepEqual(
		fbcCIStep(with_({ exact: [job("success")], equivalent: [] })),
		{
			variant: "success",
			text: "Passed",
		},
	);
});

test("imageCheckLine: the image Snapshot's bundle against the head", () => {
	const head = bundle("202610090936");
	const check = (image_check: OperatorFBC["image_check"]) =>
		imageCheckLine(operatorFBC("quay-operator", head, [], { image_check }));
	assert.equal(
		check({
			state: "matches",
			snapshot: "quay-stage-3-17-6",
			bundle_nvr: head,
		}),
		"Image Snapshot quay-stage-3-17-6 holds this bundle",
	);
	assert.equal(
		check({
			state: "differs",
			snapshot: "quay-stage-3-17-6",
			bundle_nvr: bundle("202610010000"),
		}),
		"Image Snapshot quay-stage-3-17-6 holds bundle 3.17.6.202610010000.p2.g35cf767-1",
	);
	assert.equal(
		check({ state: "absent", snapshot: "quay-stage-3-17-6", bundle_nvr: "" }),
		"Image Snapshot quay-stage-3-17-6: no bundle to compare",
	);
});

test("restaged FBCs (3.18.1): the newest Release of each counts", () => {
	const head = bundle("202610061637");
	const ok = release({ name: "ok", released_status: "True" });
	const c = fbcCandidate({
		operators: [11, 10, 10].map((n, i) =>
			operatorFBC(
				`op${i}`,
				head,
				Array.from({ length: n }, (_, v) =>
					fbcMember(`4.${12 + v}`, head, { snapshot: "s", release: ok }),
				),
			),
		),
	});
	assert.deepEqual(fbcStageStep(c), {
		variant: "success",
		text: "31 of 31 FBCs released",
	});
});

test("checking: reading cells, nothing warns or fails", () => {
	const o = operatorFBC("quay-operator", "", [], {
		conflicts: [fbcMember("4.14", ""), fbcMember("4.15", "")],
	});
	const c = fbcCandidate({ state: "unknown", operators: [o] });
	assert.deepEqual(operatorSummary(c, null).rows[0].cells, [
		"reading",
		"reading",
	]);
	assert.equal(shipsVersion(o), "");
	assert.equal(imagesState(o, false), "");
	assert.deepEqual(whileChecking(fbcCIStep(c), true), {
		variant: "info",
		text: "No CI ran on these FBCs",
	});
	assert.deepEqual(whileChecking(fbcCIStep(c), false).variant, "warning");
	assert.deepEqual(verdictLine({ state: "checking", items: [] }), {
		status: "running",
		text: "Checking…",
	});
	assert.deepEqual(readingLine({ read: 40, failed: 2, pending: 47 }), {
		status: "running",
		text: "Reading FBC contents: 40 of 89 · 2 can't be read",
	});
	assert.deepEqual(readingLine({ read: 87, failed: 2, pending: 0 }), {
		status: "warning",
		text: "2 of 89 FBCs can't be read",
	});
	assert.equal(readingLine({ read: 89, failed: 0, pending: 0 }), null);
});

test("shipped (3.18.1): record mode, OCP versions not shipped are not part of it", () => {
	const head = bundle("202610061637");
	const ok = release({ name: "ok", released_status: "True" });
	const image = (nvr: string, prod: boolean) => ({
		name: nvr,
		image: "",
		digest: "",
		nvr,
		commit: "",
		stage: true,
		prod,
	});
	const qo = operatorFBC(
		"quay-operator",
		head,
		["4.12", "4.14"].map((v) =>
			fbcMember(v, head, { snapshot: "s", release: ok }),
		),
		{
			head_csv: "quay-operator.v3.18.1",
			images: [image("quay", true), image("", false)],
		},
	);
	const cso = operatorFBC("container-security-operator", head, [
		fbcMember("4.14", head, { snapshot: "s", release: ok }),
	]);
	const c = fbcCandidate({ operators: [qo, cso] });
	assert.deepEqual(
		operatorSummary(c, null).rows.map((r) => r.cells),
		[
			["staged", "staged"],
			["absent", "staged"],
		],
	);
	assert.equal(shipsVersion(qo), "v3.18.1");
	assert.equal(bundleBuilt(head), "Oct 6 16:37");
	assert.equal(imagesState(qo, true), "in prod");
	assert.equal(imagesState(qo, false), "on stage");
	assert.deepEqual(verdictLine({ state: "shipped", items: [] }), {
		status: "done",
		text: "Shipped",
	});
	const blocker = {
		level: "blocker" as const,
		operator: "",
		owner: "ART",
		code: "not_staged",
		text: "Not in a stage shipment",
	};
	assert.deepEqual(verdictLine({ state: "not_ready", items: [blocker] }), {
		status: "failed",
		text: "Not ready: 1 blocker",
	});
});

test("compareVersions: numeric by dot-separated part", () => {
	assert.ok(compareVersions("4.9", "4.12") < 0);
	assert.ok(compareVersions("4.12", "4.9") > 0);
	assert.equal(compareVersions("4.14", "4.14"), 0);
	assert.ok(compareVersions("4", "4.1") < 0);
});
