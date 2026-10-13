import assert from "node:assert/strict";
import { test } from "node:test";
import type {
	CIJob,
	FBCCandidate,
	FBCCI,
	FBCMember,
	ImageCompareRow,
	ImageScan,
	KonfluxRelease,
	OperatorFBC,
	VerdictItem,
} from "../api/types.ts";
import {
	actionLabels,
	artGroup,
	blockedReasons,
	bundleBuilt,
	ciList,
	compareLine,
	compareVersions,
	componentLabel,
	decisions,
	fbcBuildStep,
	fbcCIStep,
	fbcParams,
	fbcPullspecs,
	fbcStageStep,
	fbcsBuilt,
	fixableLabels,
	jobLabels,
	jobShortName,
	nvrLabel,
	otherBundle,
	readingLine,
	readyText,
	releaseLine,
	releaseWord,
	rowChips,
	runsStatus,
	scanCell,
	shipsVersion,
	shownOperators,
	slotItem,
	slotRows,
	stageText,
	streamAhead,
	tknCommand,
	unreadText,
	unshippedOf,
	verdictLine,
	whileChecking,
	withoutStage,
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
			job("libvirt-s390x-ocp422-e2e-install-libvirt-s3-nightly"),
			job("aws-arm64-ocp422-e2e-install-aws-s3-nightly"),
			job("aws-ocp422-e2e-install-aws-s3-nightly-fips"),
			job("gcp-ocp422-e2e-install-gcp-gcs"),
			"periodic-ci-quay-quay-master-e2e-install-aws-s3",
			"periodic-ci-quay-quay-tests-master-e2e",
		]),
		[
			"libvirt-s390x-ocp422-libvirt-s3",
			"aws-arm64-ocp422-aws-s3",
			"aws-ocp422-aws-s3-fips",
			"gcp-ocp422-gcp-gcs",
			"aws-s3",
			"periodic-ci-quay-quay-tests-master-e2e",
		],
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
	assert.equal(releaseWord(release({ released_status: "True" })), "Released");
	assert.match(
		releaseWord(
			release({
				released_status: "True",
				completion_time: new Date(Date.now() - 2 * 86_400_000).toISOString(),
			}),
		),
		/2 days ago/,
	);
	assert.equal(releaseWord(release({ released_reason: "Failed" })), "Failed");
	assert.equal(releaseWord(release({})), "In progress");
});

test("artGroup: the application's X.Y dotted", () => {
	assert.equal(artGroup("quay-3-17"), "quay-3.17");
	assert.equal(artGroup("quay-3-9"), "quay-3.9");
});

test("fbcParams and tknCommand: 3.17.6 with and without optional params", () => {
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
		tknCommand(fbcParams("quay-3-17", "3.17.6", fbc, [])),
		`tkn pipeline start release-from-fbc -n art-quay-tenant --use-param-defaults -p 'group=quay-3.17' -p 'assembly=3.17.6' -p 'fbc-pullspecs=${fbc}' -p 'dry-run=true'`,
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
	staged: 0,
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
		text: "Not staged",
	});
	assert.deepEqual(fbcCIStep(c), {
		variant: "warning",
		text: "No runs",
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
	assert.deepEqual(blockedReasons(c), errors);
	assert.deepEqual(fbcBuildStep(stream), {
		variant: "success",
		text: "",
	});
	assert.deepEqual(fbcStageStep(c), {
		variant: "warning",
		text: "CSO, QBO not staged",
	});
	assert.deepEqual(slotRows(cso), [
		{ ocp: "4.12", kind: "missing", fbc: null },
		{ ocp: "4.14", kind: "missing", fbc: null },
	]);
	const shown = shownOperators(c, stream, false);
	assert.deepEqual(
		shown.map((s) => s.operator),
		[qo, streamCSO, streamQBO],
	);
	assert.deepEqual(
		shown.map((s) => stageText(c, s)),
		[{ status: "done", text: "On stage" }, null, null],
	);
});

test("blockedReasons: names each operator's missing OCP versions", () => {
	const head = bundle("202610060936");
	const qo = operatorFBC("quay-operator", head, [fbcMember("4.14", head)], {
		missing_ocp: ["4.22", "4.9"],
	});
	const cso = operatorFBC("container-security-operator", "", [], {
		missing_ocp: ["4.14"],
	});
	const c = fbcCandidate({
		state: "incomplete",
		errors: ["container-security-operator is not in stage"],
		operators: [qo, cso],
	});
	assert.deepEqual(blockedReasons(c), [
		"container-security-operator is not in stage",
		"quay-operator has no FBC for OCP 4.9, 4.22",
	]);
	assert.deepEqual(
		blockedReasons(fbcCandidate({ state: "unknown", operators: [qo] })),
		["quay-operator has no FBC for OCP 4.9, 4.22"],
	);
	assert.deepEqual(blockedReasons(fbcCandidate({ state: "unknown" })), [
		"a head bundle or staged catalog is not resolved yet",
	]);
	assert.deepEqual(
		blockedReasons(fbcCandidate({ operators: [qo], pullspecs: ["x"] })),
		[],
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
		text: "quay-operator failed",
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
		{ variant: "info", text: "In progress" },
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
		{ variant: "success", text: "" },
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

test("unresolved heads: slot", () => {
	const o = operatorFBC("quay-operator", "", [], {
		conflicts: [fbcMember("4.14", "")],
	});
	assert.deepEqual(
		slotRows(o).map((r) => r.kind),
		["unresolved"],
	);
});

test("fbcsBuilt: the oldest FBC's build time", () => {
	const head = bundle("202610060936");
	const fbc = (ocp: string, built: string) =>
		fbcMember(ocp, head, {
			nvr: `quay-operator-fbc-3.17.6-${built}.ocp${ocp}`,
		});
	const c = fbcCandidate({
		operators: [
			operatorFBC("quay-operator", head, [fbc("4.14", "20261012201007")], {
				conflicts: [
					fbc("4.12", "20261011090000"),
					fbcMember("4.13", "", { nvr: "" }),
				],
			}),
		],
	});
	assert.equal(fbcsBuilt(c), "2026-10-11T09:00:00Z");
	assert.equal(fbcsBuilt(fbcCandidate({})), "");
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
		{ variant: "warning", text: "1/2 passed" },
	);
	assert.deepEqual(
		fbcCIStep(with_({ exact: [], equivalent: [job("success")] })),
		{
			variant: "warning",
			text: "Passed (same bundle)",
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
		text: "",
	});
});

test("checking: nothing warns or fails", () => {
	const o = operatorFBC("quay-operator", "", [], {
		conflicts: [fbcMember("4.14", ""), fbcMember("4.15", "")],
	});
	const c = fbcCandidate({ state: "unknown", operators: [o] });
	const noHead = item(
		"blocker",
		"quay-operator",
		"no_head_bundle",
		"quay-operator OCP 4.15: FBC heads no known bundle",
	);
	const failed = item(
		"blocker",
		"quay-operator",
		"read_failed",
		"quay-operator OCP 4.14: can't read FBC: no channel stable-5.0",
	);
	assert.equal(unreadText([], "quay-operator"), null);
	assert.equal(
		unreadText([noHead], "quay-operator"),
		"FBC heads no known bundle",
	);
	assert.equal(
		unreadText([noHead, failed], "quay-operator"),
		"Can't read FBC: no channel stable-5.0",
	);
	assert.equal(unreadText([failed], "quay-bridge-operator"), null);
	assert.equal(shipsVersion(o), "");
	assert.deepEqual(whileChecking(fbcCIStep(c), true), {
		variant: "info",
		text: "No runs",
	});
	assert.deepEqual(whileChecking(fbcCIStep(c), false).variant, "warning");
	assert.deepEqual(verdictLine({ state: "checking", items: [] }), {
		status: "running",
		text: "Checking…",
	});
	assert.deepEqual(
		readingLine({ read: 40, failed: 2, pending: 47, history_days: 0 }),
		{
			status: "running",
			text: "Reading FBCs 40/89 · 2 unreadable",
		},
	);
	assert.deepEqual(
		readingLine({ read: 87, failed: 2, pending: 0, history_days: 0 }),
		{ status: "warning", text: "2/89 FBCs unreadable" },
	);
	assert.equal(
		readingLine({ read: 89, failed: 0, pending: 0, history_days: 0 }),
		null,
	);
	assert.deepEqual(
		readingLine({ read: 87, failed: 2, pending: 0, history_days: 12 }),
		{
			status: "running",
			text: "Reading build history, 12 days left · 2 unreadable",
		},
	);
});

test("shipped (3.18.1): record mode", () => {
	const head = bundle("202610061637");
	const ok = release({ name: "ok", released_status: "True" });
	const image = (nvr: string, prod: boolean) => ({
		name: nvr,
		image: "",
		digest: "",
		nvr,
		commit: "",
		build_url: "",
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
	assert.equal(shipsVersion(qo), "v3.18.1");
	assert.equal(bundleBuilt(head), "Oct 6 16:37");
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

test("history and stage failures (5.0.0): only reading from a partial history, failed operators named", () => {
	const head = bundle("202610121052");
	const ok = release({ name: "ok", released_status: "True" });
	const failed = release({ name: "f", released_reason: "Failed" });
	const member = (r: typeof ok) =>
		fbcMember("5.0", head, { snapshot: "s", release: r });
	const c = fbcCandidate({
		operators: [
			operatorFBC("quay-operator", head, [member(ok)]),
			operatorFBC("container-security-operator", head, [member(failed)]),
			operatorFBC("quay-bridge-operator", head, [member(ok)]),
		],
	});
	assert.deepEqual(fbcStageStep(c), {
		variant: "danger",
		text: "CSO failed",
	});
	const empty = fbcCandidate({
		source: "stream",
		operators: [operatorFBC("quay-operator", "", [])],
	});
	assert.deepEqual(fbcBuildStep(empty, 30), {
		variant: "info",
		text: "Reading…",
	});
	assert.deepEqual(fbcBuildStep(empty), {
		variant: "warning",
		text: "quay-operator not built",
	});
});

test("compareVersions: numeric by dot-separated part", () => {
	assert.ok(compareVersions("4.9", "4.12") < 0);
	assert.ok(compareVersions("4.12", "4.9") > 0);
	assert.equal(compareVersions("4.14", "4.14"), 0);
	assert.ok(compareVersions("4", "4.1") < 0);
});

const item = (
	level: VerdictItem["level"],
	operator: string,
	code: string,
	text = "",
): VerdictItem => ({ level, operator, owner: "", code, text });

test("withoutStage: the Stage step words stage blockers", () => {
	const items = [
		item("blocker", "", "not_staged"),
		item("blocker", "quay-operator", "missing_ocp"),
		item("blocker", "quay-bridge-operator", "missing_operator"),
		item("blocker", "quay-operator", "csv_mismatch"),
	];
	const v = withoutStage({ state: "not_ready", items });
	assert.deepEqual(
		v.items.map((i) => i.code),
		["csv_mismatch"],
	);
	assert.deepEqual(
		verdictLine(withoutStage({ state: "not_ready", items: v.items.slice(1) })),
		{ status: "failed", text: "Not ready" },
	);
	// Operator rows keep their stage labels, so none reads "Ready".
	assert.deepEqual(
		actionLabels(items, "quay-bridge-operator").map((l) => l.text),
		["Not staged"],
	);
});

test("decisions: the whole release's blockers, with a known code's action", () => {
	const items = [
		{
			...item("blocker", "", "not_staged", "Not in a stage shipment"),
			owner: "Quay (release-from-fbc)",
		},
		item("blocker", "", "unknown_fbc", "Staged FBCs with no ART build name: 1"),
		item("blocker", "quay-operator", "csv_mismatch"),
		item("warning", "", "stream_ahead"),
	];
	assert.deepEqual(
		decisions({ state: "not_ready", items }).map((d) => [
			d.text,
			d.owner,
			d.action,
		]),
		[
			[
				"Not in a stage shipment",
				"Quay (release-from-fbc)",
				"Stage the candidate",
			],
			["Staged FBCs with no ART build name: 1", "", ""],
		],
	);
	assert.deepEqual(decisions({ state: "checking", items }), []);
});

test("readyText: awaiting stage while not staged", () => {
	assert.equal(
		readyText([item("blocker", "", "not_staged")]),
		"FBC built, awaiting stage",
	);
	assert.equal(
		readyText([item("blocker", "quay-operator", "missing_ocp")]),
		"Ready",
	);
	assert.equal(readyText([]), "Ready");
});

test("stageText: by the FBCs' newest stage Releases", () => {
	const head = bundle("202610060936");
	const ok = release({
		released_status: "True",
		completion_time: "2026-10-07T12:00:00Z",
	});
	const failed = release({ released_reason: "Failed" });
	const shown = (...releases: (KonfluxRelease | null)[]) => ({
		operator: operatorFBC(
			"quay-operator",
			head,
			releases.map((r, i) => fbcMember(`4.1${i}`, head, { release: r })),
		),
		leftOut: false,
	});
	const c = fbcCandidate({});
	assert.deepEqual(stageText(c, shown(ok, failed)), {
		status: "failed",
		text: "Failed",
	});
	assert.deepEqual(stageText(c, shown(ok, null)), {
		status: "running",
		text: "In progress",
	});
	assert.deepEqual(stageText(c, shown(ok)), { status: "done", text: "Oct 7" });
	assert.equal(stageText(fbcCandidate({ source: "stream" }), shown(ok)), null);
});

test("actionLabels: one per code, the operator's only, texts and owners as tip", () => {
	const owned = (i: VerdictItem, owner: string) => ({ ...i, owner });
	const items = [
		owned(
			item("blocker", "quay-operator", "read_failed", "OCP 4.14: can't read"),
			"ART",
		),
		item("blocker", "quay-operator", "read_failed", "OCP 4.15: can't read"),
		item("blocker", "quay-bridge-operator", "missing_operator", "x"),
		item("info", "quay-operator", "stream_ahead", "newer build"),
		item("warning", "quay-operator", "unknown_code", "Something new"),
	];
	assert.deepEqual(actionLabels(items, "quay-operator"), [
		{
			level: "blocker",
			text: "Can't read",
			tip: ["OCP 4.14: can't read · ART", "OCP 4.15: can't read"],
		},
		{ level: "info", text: "Newer build", tip: ["newer build"] },
		{ level: "warning", text: "Something new", tip: ["Something new"] },
	]);
});

test("rowChips: exceptions only, per row", () => {
	const head = bundle("202610060936");
	const other = bundle("202610121613");
	const failed = release({
		released_status: "False",
		released_reason: "Failed",
	});
	const ok = release({ released_status: "True" });
	const chips = (
		r: Parameters<typeof rowChips>[0],
		o: Partial<Parameters<typeof rowChips>[1]> = {},
	) => rowChips(r, { shipped: false, ...o }).map((c) => c.text);
	const member = (m: Partial<FBCMember>) => ({
		ocp: "4.14",
		kind: "member" as const,
		fbc: fbcMember("4.14", head, m),
	});
	assert.deepEqual(chips(member({ release: ok, staged: 1 })), []);
	assert.deepEqual(chips(member({ release: ok, staged: 3 })), ["staged 3x"]);
	assert.deepEqual(chips(member({ release: failed })), []);
	const held = {
		ocp: "4.21",
		kind: "other" as const,
		fbc: fbcMember("4.21", other, { build_url: "https://art/builds/1" }),
	};
	assert.deepEqual(chips(held), []);
	assert.deepEqual(otherBundle(held), {
		text: "holds bundle Oct 12 16:13",
		title: other,
		href: "https://art/builds/1",
	});
	assert.equal(otherBundle(member({})), null);
	assert.deepEqual(chips({ ocp: "4.12", kind: "missing", fbc: null }), [
		"no FBC",
	]);
	const unread = {
		ocp: "4.20",
		kind: "unresolved" as const,
		fbc: fbcMember("4.20", ""),
	};
	assert.deepEqual(chips(unread), []);
	const items = [
		item(
			"blocker",
			"quay-bridge-operator",
			"read_failed",
			"quay-bridge-operator OCP 4.20: can't read FBC: no channel stable-5.0",
		),
	];
	assert.equal(
		slotItem(items, "read_failed", "quay-bridge-operator", "4.2"),
		undefined,
	);
	const why = slotItem(
		items,
		"read_failed",
		"quay-bridge-operator",
		"4.20",
	)?.text;
	assert.deepEqual(rowChips(unread, { shipped: false, unreadable: why }), [
		{ color: "red", text: "can't read", title: items[0].text },
	]);
});

test("record mode: no prompts, only facts", () => {
	const head = bundle("202610061637");
	const failed = release({
		released_status: "False",
		released_reason: "Failed",
	});
	assert.deepEqual(
		rowChips(
			{
				ocp: "4.14",
				kind: "other",
				fbc: fbcMember("4.14", bundle("202610121613"), {
					release: failed,
					staged: 2,
				}),
			},
			{ shipped: true, unreadable: "x" },
		).map((c) => c.text),
		["staged 2x"],
	);
	assert.deepEqual(
		rowChips(
			{ ocp: "4.12", kind: "missing", fbc: null },
			{
				shipped: true,
				leftOut: false,
			},
		),
		[],
	);
	// A shipped candidate is the record: an operator it lacks is not offered
	// from the stream, so its block does not say "add these".
	const cso = operatorFBC("container-security-operator", "", []);
	const stream = fbcCandidate({
		source: "stream",
		operators: [
			operatorFBC("container-security-operator", head, [
				fbcMember("4.14", head),
			]),
		],
	});
	const [shown] = shownOperators(
		fbcCandidate({ operators: [cso] }),
		stream,
		true,
	);
	assert.deepEqual(shown, { operator: cso, leftOut: false });
});

test("compareLine: rebuild-only is info, any new source a warning", () => {
	const row = (name: string, change: ImageCompareRow["change"]) => ({
		name,
		from: null,
		to: null,
		change,
	});
	const line = (...rows: ImageCompareRow[]) =>
		compareLine({ kind: "newer", label: "", rows });
	assert.deepEqual(
		line(
			row("quay", "new source"),
			row("operator", "rebuild"),
			row("clair", "rebuild"),
			row("builder", "rebuild"),
			row("builder-qemu", "rebuild"),
			row("postgresql-15", "same"),
		),
		{ level: "warning", text: "quay new source, 4 rebuilt" },
	);
	assert.deepEqual(line(row("operator", "rebuild"), row("clair", "rebuild")), {
		level: "info",
		text: "2 rebuilt",
	});
	assert.deepEqual(line(row("redis-6", "removed"), row("", "new build")), {
		level: "warning",
		text: "1 new build, 1 removed",
	});
	assert.deepEqual(line(row("operator", "renamed")), {
		level: "warning",
		text: "1 renamed",
	});
	assert.deepEqual(line(row("quay", "same")), {
		level: "info",
		text: "same images",
	});
});

test("unshippedOf: a left-out build goes to the operator it names, else quay-operator", () => {
	const b = (name: string) => ({
		name,
		image: "",
		nvr: "",
		build_url: "",
		snapshot: "",
	});
	const builds = [
		b("quay-3-17-container-security-operator"),
		b("quay-3-17-quay-bridge-operator-bundle"),
		b("quay-3-17-quay-clair"),
	];
	const ops = [
		"quay-operator",
		"container-security-operator",
		"quay-bridge-operator",
	];
	assert.deepEqual(
		ops.map((o) => unshippedOf(builds, ops, o).map((x) => x.name)),
		[
			["quay-3-17-quay-clair"],
			["quay-3-17-container-security-operator"],
			["quay-3-17-quay-bridge-operator-bundle"],
		],
	);
});

test("bundleBuilt: an operand NVR's stamp follows a dash", () => {
	assert.equal(
		bundleBuilt(
			"quay-clair-container-3.18.1-202610060910.p2.g6bc3daf.assembly.stream.el9",
		),
		"Oct 6 09:10",
	);
	assert.equal(
		bundleBuilt("quay-operator-fbc-3.17.6-20261006151612.ocp4.14"),
		"",
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

test("ciList: a line per slot the candidate FBC tested, the rest counted", () => {
	const job = (name: string, id: string): CIJob => ({
		job_name: name,
		runs: [
			{ state: "failure", started_at: "2026-10-12T00:00:00Z", prow_url: id },
		],
	});
	const slot = (ocp: string, ci: FBCCI | null) => ({
		operator: "quay-operator",
		ocp,
		ci,
	});
	// 4.20's runs were all on a prior FBC, and the candidate has no FBC of
	// 4.21: both count as untested.
	const untested = [
		slot("4.20", { exact: [], equivalent: [] }),
		slot("4.21", null),
	];
	const { lines, untested: n } = ciList([
		slot("4.22", { exact: [job("s390x", "1")], equivalent: [job("aws", "2")] }),
		...untested,
	]);
	assert.deepEqual(
		lines.map((l) => [
			`${l.operator} ${l.ocp}`,
			l.runs.map((r) => `${r.job}=${r.run.prow_url}`),
		]),
		[["quay-operator 4.22", ["s390x=1", "aws=2"]]],
	);
	assert.equal(n, 2);
	assert.deepEqual(ciList(untested), { lines: [], untested: 2 });
});
