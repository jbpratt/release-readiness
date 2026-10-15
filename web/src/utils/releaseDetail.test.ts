import assert from "node:assert/strict";
import { test } from "node:test";
import type {
	CIJob,
	FBCCandidate,
	FBCMember,
	ImageScan,
	KonfluxRelease,
	OperatorFBC,
	OperatorImage,
	ReleaseCandidate,
	Verdict,
	VerdictItem,
} from "../api/types.ts";
import {
	artGroup,
	bundleBuilt,
	compareVersions,
	fbcGroups,
	fbcParams,
	fbcPullspecs,
	fbcsBuilt,
	fixableLabels,
	freshnessLine,
	jobLabel,
	jobLabels,
	jobShortName,
	needsAttention,
	noFbcText,
	nvrLabel,
	otherBundle,
	readingLine,
	releaseImages,
	releaseLine,
	rowChips,
	runResult,
	scanCell,
	shownOperators,
	slotItem,
	slotRows,
	tknCommand,
	verdictEntries,
	verdictLine,
	worst,
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

test("jobLabel keeps the arch", () => {
	const job = (name: string) => `periodic-ci-quay-quay-redhat-3.18-${name}`;
	assert.equal(
		jobLabel(job("libvirt-s390x-ocp422-e2e-install-libvirt-s3-nightly")),
		"libvirt-s390x-ocp422-libvirt-s3",
	);
	assert.equal(
		jobLabel(job("aws-arm64-ocp422-e2e-install-aws-s3-nightly")),
		"aws-arm64-ocp422-aws-s3",
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
	members,
	missing_ocp: [],
	conflicts: [],
	fbc_ri_stage: { state: "none", release: null },
	images: [],
	...o,
});

const fbcCandidate = (c: Partial<FBCCandidate>): FBCCandidate => ({
	source: "stage_snapshots",
	state: "complete",
	operators: [],
	pullspecs: [],
	...c,
});

test("stream only, no stage (3.16): pullspecs from the stream", () => {
	const head =
		"quay-operator-metadata-container-3.16.7.202610121915.p2.ga4fe015.assembly.stream.el9-1";
	const members = ["4.12", "4.14"].map((v) => fbcMember(v, head));
	const c = fbcCandidate({
		source: "stream",
		operators: ["quay-operator", "container-security-operator"].map((o) =>
			operatorFBC(o, head, members),
		),
		pullspecs: [members[0].pullspec, members[1].pullspec],
	});
	assert.equal(
		fbcPullspecs(c),
		"quay.io/art-fbc@sha256:4.12-" +
			head +
			",quay.io/art-fbc@sha256:4.14-" +
			head,
	);
});

test("missing operator (3.17.6): CSO and QBO not in stage show the stream's FBCs", () => {
	const head = bundle("202610060936");
	const released = release({ name: "r", released_status: "True" });
	const qo = operatorFBC(
		"quay-operator",
		head,
		["4.12", "4.13", "4.14"].map((v) =>
			fbcMember(v, head, { release: released }),
		),
	);
	const cso = operatorFBC("container-security-operator", "", [], {
		missing_ocp: ["4.14", "4.12"],
	});
	const c = fbcCandidate({
		state: "incomplete",
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
	assert.deepEqual(slotRows(cso), [
		{ ocp: "4.12", kind: "missing", fbc: null },
		{ ocp: "4.14", kind: "missing", fbc: null },
	]);
	const shown = shownOperators(c, stream, false);
	assert.deepEqual(
		shown.map((s) => s.operator),
		[qo, streamCSO, streamQBO],
	);
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
		},
	);
	const c = fbcCandidate({
		state: "inconsistent",
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

test("checking: nothing warns or fails", () => {
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

test("verdictEntries: the headline counts every listed blocker (3.16.7)", () => {
	const v: Verdict = {
		state: "not_ready",
		items: [
			{
				...item("blocker", "", "not_staged", "Not in a stage shipment"),
				owner: "Quay (release-from-fbc)",
			},
			...["quay-operator", "container-security-operator"].map((op) =>
				item(
					"blocker",
					op,
					"mixed_builds",
					`${op} FBCs ship different bundles: OCP 4.13 not on b`,
				),
			),
			item("info", "quay-operator", "stream_ahead", "newer build"),
		],
	};
	const entries = verdictEntries(v);
	assert.deepEqual(
		entries.map((e) => [e.level, e.text, e.count, e.on]),
		[
			["blocker", "Not in a stage shipment", 1, ""],
			[
				"blocker",
				"Mixed bundles",
				2,
				"quay-operator; container-security-operator",
			],
			["info", "Newer build", 1, "quay-operator"],
		],
	);
	assert.deepEqual(entries[0].tip, [
		"Not in a stage shipment · Quay (release-from-fbc)",
	]);
	assert.equal(verdictLine(v).text, "Not ready: 3 blockers");
});

test("verdictEntries: one code's blockers grouped with their OCPs (3.17.6)", () => {
	const ocps = [
		"4.14",
		"4.15",
		"4.16",
		"4.17",
		"4.18",
		"4.19",
		"4.20",
		"4.21",
		"4.22",
	];
	const op = "container-security-operator";
	const v: Verdict = {
		state: "not_ready",
		items: ocps.map((o) =>
			item(
				"blocker",
				op,
				"stage_release_failed",
				`${op} OCP ${o}: stage release failed (verify-conforma)`,
			),
		),
	};
	const [e, ...rest] = verdictEntries(v);
	assert.deepEqual(rest, []);
	assert.equal(e.text, "Release failed");
	assert.equal(e.count, 9);
	assert.equal(e.on, `${op} OCP ${ocps.join(", ")}`);
	assert.equal(e.tip.length, 9);
	assert.equal(verdictLine(v).text, "Not ready: 9 blockers");
	assert.deepEqual(
		verdictEntries({
			state: "not_ready",
			items: [
				item(
					"blocker",
					"quay-operator",
					"missing_ocp",
					"quay-operator OCP 4.12, 4.13 not in the stage shipment",
				),
			],
		})[0].on,
		"quay-operator OCP 4.12, 4.13",
	);
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
	assert.deepEqual(chips(member({ release: ok })), []);
	assert.deepEqual(chips(member({ release: failed })), []);
	const held = {
		ocp: "4.21",
		kind: "other" as const,
		fbc: fbcMember("4.21", other, { build_url: "https://art/builds/1" }),
	};
	assert.deepEqual(chips(held), []);
	assert.deepEqual(otherBundle(held), {
		text: "holds an older bundle (Oct 12 16:13)",
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
		{ status: "danger", text: "can't read", title: items[0].text },
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
				}),
			},
			{ shipped: true, unreadable: "x" },
		),
		[],
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

test("bundleBuilt: an operand NVR's stamp follows a dash, an FBC's has seconds", () => {
	assert.equal(
		bundleBuilt(
			"quay-clair-container-3.18.1-202610060910.p2.g6bc3daf.assembly.stream.el9",
		),
		"Oct 6 09:10",
	);
	assert.equal(
		bundleBuilt("quay-operator-fbc-3.17.6-20261006151612.ocp4.14"),
		"Oct 6 15:16",
	);
	assert.equal(bundleBuilt("quay-operator-fbc-3.17.6-2026100615.ocp4.14"), "");
	assert.equal(bundleBuilt(""), "");
});

const image = (name: string, digest: string): OperatorImage => ({
	name,
	image: `registry.redhat.io/quay/${name}@${digest}`,
	digest,
	nvr: "",
	commit: "",
	build_url: "",
	stage: false,
	scan: null,
});

test("fbcGroups and needsAttention: a failed stage, a flag or a failed run", () => {
	const head = bundle("202610061637");
	const other = bundle("202610050101");
	const ok = release({
		released_status: "True",
		completion_time: "2026-10-07T12:00:00Z",
	});
	const failedRelease = release({ released_reason: "Failed" });
	const qo = operatorFBC(
		"quay-operator",
		head,
		[
			fbcMember("4.12", head, {
				release: ok,
				ci: { exact: [job("success", "failure")], equivalent: [] },
			}),
			fbcMember("4.13", head, { release: failedRelease }),
			fbcMember("4.16", head, {
				release: ok,
				ci: { exact: [job("failure")], equivalent: [job("success")] },
			}),
		],
		{
			conflicts: [fbcMember("4.14", other)],
			missing_ocp: ["4.15"],
			images: [
				image("", "sha256:b"),
				image("", "sha256:b"),
				image("quay", "sha256:q"),
			],
		},
	);
	const d: ReleaseCandidate = {
		shipped: false,
		candidate: null,
		reason: "",
		fbc_candidate: fbcCandidate({ operators: [qo] }),
		fbc_stream: null,
		verdict: {
			state: "not_ready",
			items: [
				item("blocker", "quay-operator", "mixed_builds", "mixed"),
				item("blocker", "quay-operator", "stage_release_failed", "failed"),
			],
		},
		reading: { read: 0, failed: 0, pending: 0, history_days: 0 },
	};
	const [g] = fbcGroups(d);
	assert.deepEqual(
		g.rows.map((r) => [
			r.ocp,
			r.stage?.text ?? null,
			r.flags.map((f) => f.text),
		]),
		[
			["4.12", "Staged Oct 7", []],
			["4.13", "Stage failed", []],
			["4.14", null, ["holds an older bundle (Oct 5 01:01)"]],
			["4.15", null, ["no FBC"]],
			["4.16", "Staged Oct 7", []],
		],
	);
	const [, , , , runs] = g.rows;
	assert.deepEqual(
		runs.runs.map((p) => [p.state, p.runs, p.equivalent]),
		[
			["failure", 1, false],
			["success", 1, true],
		],
	);
	assert.equal(g.rows[0].runs[0].runs, 2);
	// Only a head-bundle FBC carries the operator's images, each once.
	assert.deepEqual(
		g.rows.map((r) => r.images.map((i) => i.digest)),
		[
			["sha256:b", "sha256:q"],
			["sha256:b", "sha256:q"],
			[],
			[],
			["sha256:b", "sha256:q"],
		],
	);
	assert.deepEqual(
		g.rows.filter(needsAttention).map((r) => r.ocp),
		["4.13", "4.14", "4.15", "4.16"],
	);
	const [stream] = fbcGroups({
		...d,
		fbc_candidate: fbcCandidate({ source: "stream", operators: [qo] }),
	});
	assert.deepEqual(
		stream.rows.map((r) => r.stage),
		[null, null, null, null, null],
	);
	const unreadable = "quay-operator OCP 4.12: manifest unknown";
	const [read] = fbcGroups({
		...d,
		verdict: {
			state: "not_ready",
			items: [item("blocker", "quay-operator", "read_failed", unreadable)],
		},
	});
	assert.deepEqual(read.rows[0].flags, [
		{ status: "danger", text: "can't read", title: unreadable },
	]);
	assert.deepEqual(fbcGroups({ ...d, fbc_candidate: null }), []);
});

test("releaseImages and worst: each digest once, with its issues", () => {
	const quay = {
		...image("quay-rhel9", "sha256:q"),
		nvr: "quay-rhel9-container-3.18.1-202610121915",
		stage: true,
	};
	const pg = {
		...image("postgresql-15", "sha256:p"),
		image: "quay.io/sclorg/postgresql-15@sha256:p",
	};
	const images = releaseImages(
		[
			operatorFBC("quay-operator", "", [], { images: [quay, pg] }),
			operatorFBC("container-security-operator", "", [], { images: [quay] }),
		],
		"3.18.2",
	);
	assert.deepEqual(
		images.map((r) => [
			r.label,
			r.operators,
			r.issues.map((f) => [f.text, f.status]),
			worst(r),
		]),
		[
			[
				"quay-rhel9",
				["quay-operator", "container-security-operator"],
				[["3.18.1 build", "warning"]],
				1,
			],
			[
				"postgresql-15",
				["quay-operator"],
				[["not registry.redhat.io", "danger"]],
				0,
			],
		],
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
		status: "warning",
		url: "https://konflux.example/plr",
	});
	assert.deepEqual(scanCell(scan({ state: "not_scanned" })), {
		text: "not scanned",
		url: "https://konflux.example/plr",
	});
	assert.deepEqual(scanCell(scan({ state: "pending" })), { text: "-" });
	assert.deepEqual(scanCell(null), { text: "-" });
});

test("runResult", () => {
	assert.deepEqual(
		[
			"success",
			"failure",
			"error",
			"pending",
			"triggered",
			"aborted",
			"bogus",
		].map(runResult),
		[
			{ status: "done", text: "Passed" },
			{ status: "failed", text: "Failed" },
			{ status: "failed", text: "Error" },
			{ status: "running", text: "Running" },
			{ status: "running", text: "Pending" },
			{ status: undefined, text: "Aborted" },
			{ status: undefined, text: "bogus" },
		],
	);
});

test("freshnessLine", () => {
	const ago = (min: number) =>
		new Date(Date.now() - min * 60_000).toISOString();
	const sources = [
		{ source: "art-builds", last_success: ago(2) },
		{ source: "art-history", last_success: ago(7) },
		{ source: "jira", last_success: ago(90) },
		{ source: "konflux", last_success: ago(1) },
	];
	// Jira never feeds the verdict, so neither its age nor its failure shows.
	assert.deepEqual(freshnessLine({ problems: [], sources }), {
		text: "Refreshed 7 minutes ago",
		problems: [],
	});
	const jira = {
		source: "jira",
		message: "boom",
		since: ago(5),
		last_success: ago(90),
	};
	assert.equal(
		freshnessLine({ problems: [jira], sources })?.text,
		"Refreshed 7 minutes ago",
	);

	const failing = {
		source: "art-history",
		message: "boom",
		since: ago(5),
		last_success: ago(7),
	};
	const stale = {
		source: "konflux",
		message: "no successful sync in 10m0s",
		since: null,
		last_success: null,
	};
	assert.deepEqual(
		freshnessLine({ problems: [failing, jira, stale], sources }),
		{
			text: "Stale: 2 sources",
			problems: [failing, stale],
		},
	);
	assert.equal(
		freshnessLine({ problems: [stale], sources })?.text,
		"Stale: Konflux releases",
	);

	// A source on its first pass, or none tracked, says nothing.
	assert.equal(
		freshnessLine({
			problems: [],
			sources: [...sources, { source: "fbc-render", last_success: null }],
		}),
		null,
	);
	assert.equal(
		freshnessLine({
			problems: [],
			sources: sources.filter((f) => f.source === "jira"),
		}),
		null,
	);
});

test("noFbcText", () => {
	const base = "No FBC build found for v3.0.0.";
	assert.equal(
		noFbcText("3.0.0", "not a concrete quay-vX.Y.Z version"),
		`${base} Only Quay versions named quay-vX.Y.Z have FBC builds.`,
	);
	assert.equal(
		noFbcText("3.0.0", "no stage candidate"),
		`${base} No stage candidate.`,
	);
	assert.equal(noFbcText("3.0.0", ""), base);
	assert.equal(noFbcText("3.0.0", undefined), base);
});
