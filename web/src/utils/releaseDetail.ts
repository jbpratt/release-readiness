import type {
	CIRun,
	FBCCandidate,
	FBCMember,
	FBCTags,
	ImageScan,
	KonfluxRelease,
	OperatorFBC,
	OperatorImage,
	ReadProgress,
	ReleaseCandidate,
	ScanCounts,
	SyncProblem,
	SyncStatus,
	Verdict,
	VerdictItem,
} from "../api/types";
import { SHOW_CVES } from "./features.ts";
import { relative } from "./format.ts";

// A "-nightly" segment, at the end or before a suffix like "-fips".
const NIGHTLY = /-nightly(?=-|$)/;

/**
 * A periodic job's install target: ...-e2e-install-aws-s3-nightly is aws-s3,
 * ...-e2e-install-aws-s3-nightly-fips aws-s3-fips.
 */
export const jobShortName = (job: string) =>
	job.match(/e2e-install-(.+)$/)?.[1].replace(NIGHTLY, "") ?? job;

/**
 * A job's name after "redhat-X.Y-" less "e2e-install-" and "-nightly":
 * aws-ocp422-aws-s3. Other jobs get their short name.
 */
export const jobLabel = (job: string) =>
	job
		.match(/redhat-\d+\.\d+-(.+)$/)?.[1]
		.replace("e2e-install-", "")
		.replace(NIGHTLY, "") ?? jobShortName(job);

export const jobLabels = (jobs: string[]) => jobs.map(jobLabel);

const failedState = (s: string) => s === "failure" || s === "error";

/** Each FBC of a slot, on the head bundle or not, of every operator. */
const slotFBCs = (c: FBCCandidate) =>
	c.operators.flatMap((o) => [...o.members, ...o.conflicts]);

/**
 * When ART built the candidate's oldest FBC, by its NVR's UTC timestamp:
 * quay-operator-fbc-3.17.6-20261012201007.ocp4.14 is 2026-10-12T20:10:07Z;
 * "" without one.
 */
export function fbcsBuilt(c: FBCCandidate): string {
	const [t] = slotFBCs(c)
		.map((m) => m.nvr.match(/-(\d{14})\.ocp/)?.[1] ?? "")
		.filter(Boolean)
		.sort();
	return t
		? `${t.slice(0, 4)}-${t.slice(4, 6)}-${t.slice(6, 8)}T${t.slice(8, 10)}:${t.slice(10, 12)}:${t.slice(12)}Z`
		: "";
}

/** Whether an operator has an FBC, on the head bundle or not. */
const hasFBC = (o: OperatorFBC) => o.members.length + o.conflicts.length > 0;

/**
 * release-from-fbc's fbc-pullspecs, comma-separated, only for a complete
 * candidate; else null.
 */
export const fbcPullspecs = (c: FBCCandidate): string | null =>
	c.state === "complete" && c.pullspecs.length > 0
		? c.pullspecs.join(",")
		: null;

/** release-from-fbc's fbc-pullspecs from ART's tags, with what to tell. */
export interface TagList {
	/** The pullspecs, comma-separated; null while there is an error. */
	value: string | null;
	count: number;
	errors: string[];
	warnings: string[];
}

/**
 * The tag pullspecs of a version's FBCs. A slot with no tag, no tag at all,
 * or a pullspec that is a digest, not a tag, is an error and offers no list,
 * as a partial list must not be released.
 */
export function fbcTagList(t: FBCTags): TagList {
	const errors = [...t.errors];
	if (errors.length === 0 && t.pullspecs.length === 0) {
		errors.push("ART has no FBC tags for this version");
	}
	for (const p of t.pullspecs) {
		if (p.includes("@")) errors.push(`${p} is a digest, not a tag`);
	}
	return {
		value: errors.length > 0 ? null : t.pullspecs.join(","),
		count: t.pullspecs.length,
		errors,
		warnings: t.warnings,
	};
}

/** Compares dot-separated versions numerically: OCP 4.9 is lower than 4.12. */
export function compareVersions(a: string, b: string): number {
	const x = a.split(".").map(Number);
	const y = b.split(".").map(Number);
	for (let i = 0; i < Math.max(x.length, y.length); i++) {
		const d = (x[i] ?? -1) - (y[i] ?? -1);
		if (d !== 0) return d;
	}
	return 0;
}

/**
 * An operator's OCP version slot: its FBC on the head bundle ("member"), on
 * another bundle ("other"), on an unresolved one ("unresolved"), or none
 * ("missing").
 */
export interface SlotRow {
	ocp: string;
	kind: "member" | "other" | "unresolved" | "missing";
	fbc: FBCMember | null;
}

/** An operator's slots by OCP version. */
export function slotRows(o: OperatorFBC): SlotRow[] {
	const rows: SlotRow[] = [
		...o.members.map((m) => ({ ocp: m.ocp, kind: "member" as const, fbc: m })),
		...o.conflicts.map((m) => ({
			ocp: m.ocp,
			kind: m.head_bundle_nvr ? ("other" as const) : ("unresolved" as const),
			fbc: m,
		})),
		...o.missing_ocp.map((ocp) => ({
			ocp,
			kind: "missing" as const,
			fbc: null,
		})),
	];
	return rows.sort((a, b) => compareVersions(a.ocp, b.ocp));
}

/** An operator as the page shows it, and whether it is the stream's. */
export interface ShownOperator {
	operator: OperatorFBC;
	/** The stage shipment leaves it out: these are the stream's FBCs to add. */
	leftOut: boolean;
}

/**
 * Each operator of the candidate; one an unshipped stage shipment leaves out
 * is the stream's, which would be added. A shipped one is the record.
 */
export const shownOperators = (
	c: FBCCandidate,
	stream: FBCCandidate | null,
	shipped: boolean,
): ShownOperator[] =>
	c.operators.map((o) => {
		const s = stream?.operators.find((x) => x.operator === o.operator);
		return !shipped &&
			c.source === "stage_snapshots" &&
			!hasFBC(o) &&
			s &&
			hasFBC(s)
			? { operator: s, leftOut: true }
			: { operator: o, leftOut: false };
	});

/** The verdict item of code on an operator's FBC for an OCP version. */
export const slotItem = (
	items: VerdictItem[],
	code: string,
	operator: string,
	ocp: string,
) =>
	items.find(
		(i) =>
			i.code === code &&
			i.operator === operator &&
			i.text.startsWith(`${operator} OCP ${ocp}: `),
	);

/** A chip on an FBC row, flagging an exception. */
export interface Chip {
	/** Its PF Label status; none for a note that is not a problem. */
	status?: "danger" | "warning";
	text: string;
	title?: string;
}

/**
 * An FBC row's exception chips: a missing or unreadable FBC, none shipped;
 * the row's FBC build says how often it was staged.
 */
export function rowChips(
	r: SlotRow,
	{ shipped, unreadable }: { shipped: boolean; unreadable?: string },
): Chip[] {
	const chips: Chip[] = [];
	if (shipped) return chips;
	if (r.kind === "missing") chips.push({ status: "danger", text: "no FBC" });
	if (unreadable)
		chips.push({ status: "danger", text: "can't read", title: unreadable });
	return chips;
}

/**
 * The bundle an FBC row's FBC heads when it is not its operator's head
 * bundle, linking the FBC's build; else null.
 */
export function otherBundle(
	r: SlotRow,
): { text: string; title: string; href: string | null } | null {
	const nvr = r.kind === "other" ? r.fbc?.head_bundle_nvr : "";
	if (!nvr) return null;
	return {
		text: `holds an older bundle (${bundleBuilt(nvr) || nvrLabel(nvr)})`,
		title: nvr,
		href: r.fbc?.build_url || null,
	};
}

/** A job's newest run on an FBC, how many runs it had, and on which FBC. */
export type RunPill = CIRun & {
	job: string;
	runs: number;
	/** It installed another FBC of this OCP version on the same bundle. */
	equivalent: boolean;
};

const runResults: Record<string, [StatusLine["status"] | undefined, string]> = {
	success: ["done", "Passed"],
	failure: ["failed", "Failed"],
	error: ["failed", "Error"],
	pending: ["running", "Running"],
	triggered: ["running", "Pending"],
	aborted: [undefined, "Aborted"],
};

/** A Prow run state as its Status and word; an unknown one has no status. */
export function runResult(state: string): {
	status?: StatusLine["status"];
	text: string;
} {
	const [status, text] = runResults[state] ?? [undefined, state];
	return { status, text };
}

/** An exception on an FBC row; otherBundle's links its FBC's build. */
export type Flag = Chip & { href?: string | null };

/** A candidate FBC as a table row. */
export interface FbcRow {
	operator: string;
	ocp: string;
	fbc: FBCMember | null;
	/** Its stage Release; null when the stage shipment does not hold it. */
	stage: StatusLine | null;
	flags: Flag[];
	runs: RunPill[];
	/** Its FBC's images when it holds its operator's head bundle; else none. */
	images: OperatorImage[];
}

/** An operator's images, each once: opm can list the bundle image twice. */
const uniqueImages = (o: OperatorFBC) =>
	o.images.filter(
		(i, k) =>
			o.images.findIndex((x) => x.name === i.name && x.digest === i.digest) ===
			k,
	);

/**
 * The candidate's FBCs by operator: a row per OCP version with its stage,
 * flags and CI.
 */
export function fbcGroups(
	d: ReleaseCandidate,
): { operator: string; rows: FbcRow[] }[] {
	const c = d.fbc_candidate;
	if (!c) return [];
	const items = d.verdict.items;
	return shownOperators(c, d.fbc_stream, d.shipped).map((s) => {
		const o = s.operator;
		const staged = !s.leftOut && c.source !== "stream";
		return {
			operator: o.operator,
			rows: slotRows(o).map((r) => {
				const flags: Flag[] = rowChips(r, {
					shipped: d.shipped,
					unreadable: slotItem(items, "read_failed", o.operator, r.ocp)?.text,
				});
				const held = otherBundle(r);
				if (held) flags.push({ status: "warning", ...held });
				return {
					operator: o.operator,
					ocp: r.ocp,
					fbc: r.fbc,
					stage: staged && r.fbc?.release ? fbcStage(r.fbc.release) : null,
					flags,
					runs: [
						...(r.fbc?.ci?.exact ?? []).map((j) => ({ j, equivalent: false })),
						...(r.fbc?.ci?.equivalent ?? []).map((j) => ({
							j,
							equivalent: true,
						})),
					]
						.filter(({ j }) => j.runs.length > 0)
						.map(({ j, equivalent }) => ({
							...j.runs[0],
							job: j.job_name,
							runs: j.runs.length,
							equivalent,
						})),
					images: r.kind === "member" ? uniqueImages(o) : [],
				};
			}),
		};
	});
}

/** An FBC's stage Release in a word or two. */
function fbcStage(r: KonfluxRelease): StatusLine {
	const l = releaseLine(r);
	if (l.status === "failed") return { status: "failed", text: "Stage failed" };
	if (l.status === "running") return { status: "running", text: "Staging" };
	return {
		status: "done",
		text: r.completion_time ? `Staged ${day(r.completion_time)}` : "Staged",
	};
}

/** Whether a row has something to act on: a failed stage, a flag or a failed run. */
export const needsAttention = (r: FbcRow) =>
	r.stage?.status === "failed" ||
	r.flags.length > 0 ||
	r.runs.some((p) => failedState(p.state));

const MONTHS = "Jan Feb Mar Apr May Jun Jul Aug Sep Oct Nov Dec".split(" ");

/**
 * When ART built a bundle, image or FBC, by its NVR's timestamp, in UTC as
 * ART writes it: ...-3.17.6.202610060936.p2... and ...-3.17.6-202610060936.p2...
 * are "Oct 6 09:36", ...-fbc-3.18.2-20261013021425.ocp4.12 "Oct 13 02:14";
 * "" without one.
 */
export function bundleBuilt(nvr: string): string {
	const t = nvr.match(/[.-](\d{4})(\d{2})(\d{2})(\d{2})(\d{2})(?:\d{2})?\./);
	return t ? `${MONTHS[Number(t[2]) - 1]} ${Number(t[3])} ${t[4]}:${t[5]}` : "";
}

/** A date without its time: "Oct 7". */
const day = (iso: string) =>
	new Date(iso).toLocaleDateString(undefined, {
		month: "short",
		day: "numeric",
	});

/**
 * A line of the verdict's list: a level's items of one code. text is a whole
 * release item's own, else the code in a word or two.
 */
export interface VerdictEntry {
	level: VerdictItem["level"];
	text: string;
	count: number;
	/** What its items are on: "quay-operator OCP 4.14, 4.15"; "" on the whole release. */
	on: string;
	/** Each item's text and owner. */
	tip: string[];
}

const levelRank = { blocker: 0, warning: 1, info: 2 };

/**
 * The verdict's items as its list, one entry per level and code: blockers
 * first, the whole release's before the operators'. The headline counts the
 * blocker entries' items.
 */
export function verdictEntries(v: Verdict): VerdictEntry[] {
	const entries = new Map<
		string,
		VerdictEntry & { ocps: Map<string, string[]> }
	>();
	for (const i of v.items) {
		const key = `${i.level} ${i.code}`;
		const e = entries.get(key) ?? {
			level: i.level,
			text: i.operator ? (actionWords[i.code] ?? i.text) : i.text,
			count: 0,
			on: "",
			tip: [],
			ocps: new Map<string, string[]>(),
		};
		e.count++;
		e.tip.push(i.owner ? `${i.text} · ${i.owner}` : i.text);
		if (i.operator) {
			const ocps = e.ocps.get(i.operator) ?? [];
			const ocp = i.text.startsWith(i.operator)
				? i.text
						.slice(i.operator.length)
						.match(/^ OCP ([\d.]+(?:, [\d.]+)*)[: ]/)?.[1]
				: undefined;
			if (ocp) ocps.push(ocp);
			e.ocps.set(i.operator, ocps);
		}
		entries.set(key, e);
	}
	return [...entries.values()]
		.map(({ ocps, ...e }) => ({
			...e,
			on: [...ocps]
				.map(([op, o]) => (o.length ? `${op} OCP ${o.join(", ")}` : op))
				.join("; "),
		}))
		.sort(
			(a, b) =>
				levelRank[a.level] - levelRank[b.level] ||
				Number(a.on !== "") - Number(b.on !== ""),
		);
}

/**
 * The verdict's headline, counting the blockers its list shows; a shipped
 * version's is the record of its prod Release, which the page adds.
 */
export function verdictLine(v: Verdict): StatusLine {
	const blockers = verdictEntries(v)
		.filter((e) => e.level === "blocker")
		.reduce((n, e) => n + e.count, 0);
	switch (v.state) {
		case "checking":
			return { status: "running", text: "Checking…" };
		case "shipped":
			return { status: "done", text: "Shipped" };
		case "ready":
			return { status: "done", text: "Ready" };
		default:
			return {
				status: "failed",
				text: blockers
					? `Not ready: ${blockers} blocker${blockers === 1 ? "" : "s"}`
					: "Not ready",
			};
	}
}

/**
 * How far the render sync has read FBC images and the backfill ART build
 * history, null with nothing pending or failed: in progress while either is,
 * a warning for failed reads only once neither is.
 */
export function readingLine(r: ReadProgress): StatusLine | null {
	const total = r.read + r.failed + r.pending;
	const reading = [
		r.pending > 0 && `FBCs ${r.read}/${total}`,
		r.history_days > 0 &&
			`build history, ${r.history_days} day${r.history_days === 1 ? "" : "s"} left`,
	].filter(Boolean);
	if (reading.length === 0)
		return r.failed
			? {
					status: "warning",
					text: `${r.failed}/${total} FBCs unreadable`,
				}
			: null;
	return {
		status: "running",
		text: `Reading ${reading.join(" · ")}${r.failed ? ` · ${r.failed} unreadable` : ""}`,
	};
}

const CONTAINER = "-container-";

/**
 * An image's NVR without its image name and ART's assembly part:
 * quay-quay-container-3.18.1-1.p2.g1148474.assembly.stream.el9 is
 * 3.18.1-1.p2.g1148474.
 */
export function nvrLabel(nvr: string): string {
	const i = nvr.indexOf(CONTAINER);
	return i < 0
		? nvr
		: nvr.slice(i + CONTAINER.length).replace(/\.assembly\..+?\.el\d+/, "");
}

/** A line of a build's box: a status icon and the text it explains. */
export interface StatusLine {
	status: "done" | "failed" | "running" | "warning";
	text: string;
}

// Why a failed Release failed, by the managed pipeline task it stopped at.
const failedTasks: Record<string, string> = {
	"apply-mapping": "Release failed: mapping config (ART)",
	"verify-access-to-resources":
		"Release failed: registry/resource access (ART/Konflux)",
	"verify-conforma": "Release failed: Enterprise Contract policy",
};

/**
 * A Release's status and where its managed pipeline failed. A running
 * Release is also Released=False, with reason Progressing.
 */
export function releaseLine(r: KonfluxRelease): StatusLine {
	if (r.released_status === "True") {
		return {
			status: "done",
			text: r.completion_time
				? `Released ${relative(r.completion_time)}`
				: "Released",
		};
	}
	if (r.released_reason === "Failed") {
		const known = failedTasks[r.failed_task ?? ""];
		const at = [r.failed_task, r.failed_step].filter(Boolean).join("/");
		return {
			status: "failed",
			text: known
				? `${known} (${at})`
				: at
					? `Release failed at ${at}`
					: "Release failed",
		};
	}
	return { status: "running", text: "Release in progress" };
}

// What each verdict item code asks for, in a word or two.
const actionWords: Record<string, string> = {
	missing_operator: "Not staged",
	missing_ocp: "OCP missing",
	csv_mismatch: "Wrong CSV",
	mixed_builds: "Mixed bundles",
	image_mismatch: "Image mismatch",
	read_failed: "Can't read",
	no_head_bundle: "No bundle",
	stage_release_failed: "Release failed",
	stage_push_failed: "Stage push failed",
	stream_ahead: "Newer build",
};

/** A Konflux application's ART group: quay-3-17 is quay-3.17. */
export const artGroup = (app: string) => app.replace(/-(\d+)$/, ".$1");

/**
 * release-from-fbc's params for a version, in a dry run: date's day is
 * target-release-date, and empty optional params are left out so the
 * Pipeline's defaults apply.
 */
export function fbcParams(
	app: string,
	version: string,
	pullspecs: string,
	bugs: string[],
	date?: string,
): Record<string, string> {
	const params: Record<string, string> = {
		group: artGroup(app),
		assembly: version,
		"fbc-pullspecs": pullspecs,
	};
	if (bugs.length > 0) params["jira-bugs"] = bugs.join(",");
	if (date) params["target-release-date"] = date.slice(0, 10);
	params["dry-run"] = "true";
	return params;
}

/** The Jira status release-from-fbc's advisory takes issues in. */
export const RELEASE_PENDING = "Release Pending";

/** An issue key's number: PROJQUAY-12348 is 12348. */
const keyNumber = (key: string) => Number(key.slice(key.lastIndexOf("-") + 1));

/** Issue keys by number, so PROJQUAY-9 comes before PROJQUAY-10. */
const byKeyNumber = (a: string, b: string) =>
	keyNumber(a) - keyNumber(b) || a.localeCompare(b);

/** The issues release-from-fbc leaves out of the advisory, per status. */
export interface LeftOut {
	status: string;
	keys: string[];
}

/** A release's issues split for release-from-fbc's jira-bugs. */
export interface IssueLists {
	/** The release's Release Pending issues, by key number, each once. */
	keys: string[];
	/** The other issues by status, the largest group first; the release ticket is no issue. */
	leftOut: LeftOut[];
}

/**
 * Splits issues for jira-bugs: only Release Pending ones go in, the release
 * ticket is no advisory issue, and the rest are listed by status so none
 * vanishes silently. Undefined while issues is, still loading.
 */
export function issueLists(
	issues: { key: string; status: string }[] | undefined,
	ticketKey?: string,
): IssueLists | undefined {
	if (!issues) return undefined;
	const keys = new Set<string>();
	const others = new Map<string, Set<string>>();
	for (const { key, status } of issues) {
		if (key === ticketKey) continue;
		if (status === RELEASE_PENDING) {
			keys.add(key);
			continue;
		}
		const name = status || "No status";
		others.set(name, (others.get(name) ?? new Set()).add(key));
	}
	return {
		keys: [...keys].sort(byKeyNumber),
		leftOut: [...others]
			.map(([status, set]) => ({ status, keys: [...set].sort(byKeyNumber) }))
			.sort(
				(a, b) =>
					b.keys.length - a.keys.length || a.status.localeCompare(b.status),
			),
	};
}

/** The tkn command that starts release-from-fbc with params. */
export const tknCommand = (params: Record<string, string>) =>
	[
		"tkn pipeline start release-from-fbc -n art-quay-tenant --use-param-defaults",
		...Object.entries(params).map(([k, v]) => `-p '${k}=${v}'`),
	].join(" ");

export const SEVERITIES = [
	"critical",
	"high",
	"medium",
	"low",
	"unknown",
] as const;

/** A severity's label colour. */
export const SEVERITY_COLORS = {
	critical: "red",
	high: "red",
	medium: "orange",
	low: "yellow",
	unknown: "grey",
} as const;

/**
 * A scanned image's cell: a label per severity with fixable CVEs. The no-fix
 * ones, hundreds per image, would drown it.
 */
export const fixableLabels = ({ fixable }: ScanCounts) =>
	SEVERITIES.filter((s) => fixable[s] > 0).map((s) => ({
		text: `${fixable[s]} ${s}`,
		color: SEVERITY_COLORS[s],
	}));

/**
 * The CVEs cell of a candidate image without counts: its text, status and
 * link. A pending or unread scan is a muted dash.
 */
export function scanCell(scan: ImageScan | null): {
	text: string;
	status?: "warning";
	url?: string;
} {
	switch (scan?.state) {
		case "scan_failed":
			return { text: "scan failed", status: "warning", url: scan.url };
		case "not_scanned":
			return { text: "not scanned", url: scan.url };
		default:
			return { text: "-" };
	}
}

/**
 * An image the release ships, once per digest: the operators and
 * repositories referencing it, and what is wrong with it.
 */
export interface ReleaseImage {
	image: OperatorImage;
	/** Its relatedImage name, or "<operator> bundle". */
	label: string;
	operators: string[];
	repos: string[];
	issues: Flag[];
}

/** The operators the page shows: the stream's for one stage leaves out. */
export const candidateOperators = (d: ReleaseCandidate): OperatorFBC[] =>
	d.fbc_candidate
		? shownOperators(d.fbc_candidate, d.fbc_stream, d.shipped).map(
				(s) => s.operator,
			)
		: [];

const repo = (image: string) => image.split("@")[0];

/** The X.Y.Z an ART image was built as: ...-container-3.18.1-2026... is 3.18.1. */
const nvrVersion = (nvr: string) =>
	nvr.match(/-container-(\d+\.\d+\.\d+)[.-]/)?.[1] ?? "";

/** Operators' head bundle images, each once per digest, with their issues. */
export function releaseImages(
	ops: OperatorFBC[],
	version: string,
): ReleaseImage[] {
	const byDigest = new Map<string, ReleaseImage>();
	for (const o of ops)
		for (const i of o.images) {
			const seen = byDigest.get(i.digest);
			if (!seen) {
				byDigest.set(i.digest, {
					image: i,
					label: i.name || `${o.operator} bundle`,
					operators: [o.operator],
					repos: [repo(i.image)],
					issues: [],
				});
				continue;
			}
			if (!seen.operators.includes(o.operator)) seen.operators.push(o.operator);
			if (!seen.repos.includes(repo(i.image))) seen.repos.push(repo(i.image));
		}
	const images = [...byDigest.values()];
	for (const r of images) r.issues = imageIssues(r, version);
	return images;
}

/**
 * An image's issues: a pullspec off registry.redhat.io; for one ART built, not
 * on stage, built as another version, or with SHOW_CVES its scan unread or
 * with fixable critical or high CVEs.
 */
function imageIssues({ image: i, repos }: ReleaseImage, version: string) {
	const issues: Flag[] = [];
	const off = repos.filter((r) => !r.startsWith("registry.redhat.io/"));
	if (off.length > 0)
		issues.push({
			status: "danger",
			text: "not registry.redhat.io",
			title: off.join("\n"),
		});
	if (!i.nvr) return issues;
	if (!i.stage) issues.push({ status: "danger", text: "not on stage" });
	const built = nvrVersion(i.nvr);
	if (built && built !== version)
		issues.push({ status: "warning", text: `${built} build`, title: i.nvr });
	if (!SHOW_CVES) return issues;
	const fixable = i.scan?.counts?.fixable;
	if (fixable && fixable.critical + fixable.high > 0)
		issues.push({
			status: "danger",
			text: `${fixable.critical + fixable.high} fixable critical/high`,
		});
	else if (!i.scan) issues.push({ text: "scan pending" });
	else if (i.scan.state !== "scanned")
		issues.push({ ...scanCell(i.scan), title: i.scan.url });
	return issues;
}

const rank = (f: Flag) =>
	f.status === "danger" ? 0 : f.status === "warning" ? 1 : 2;

/** An image's worst issue's rank, 0 for danger; 3 without one. */
export const worst = (r: ReleaseImage) => Math.min(3, ...r.issues.map(rank));

// Sync source ids as the page names them.
export const SOURCE_NAMES: Record<string, string> = {
	konflux: "Konflux releases",
	"art-builds": "ART builds",
	"art-history": "ART build history",
	"fbc-render": "FBC reads",
	jira: "Jira",
	catalog: "Catalog",
	prow: "Prow",
	"shipment-mrs": "Shipment MRs",
	"image-scans": "Image scans",
	reconcile: "FBC Jira reconcile",
	retention: "Retention",
};

// Sources no page reads: the daily prune and the FBC Jira writer.
const NON_DATA = new Set(["retention", "reconcile"]);

/**
 * How current the page's data is: when the least recent data source not
 * stale last succeeded, and the stale ones by name. CVE scans count only
 * while shown.
 */
export function freshnessLine(s: SyncStatus): {
	updated: string | null;
	stale: string | null;
	problems: SyncProblem[];
} {
	const shown = (x: { source: string }) =>
		SHOW_CVES || x.source !== "image-scans";
	const problems = s.problems.filter(shown);
	const bad = new Set(problems.map((p) => p.source));
	const last = s.sources
		.filter((f) => shown(f) && !bad.has(f.source) && !NON_DATA.has(f.source))
		.flatMap((f) => (f.last_success ? [Date.parse(f.last_success)] : []));
	return {
		updated:
			last.length > 0
				? `Updated ${relative(new Date(Math.min(...last)).toISOString())}`
				: null,
		stale:
			problems.length > 0
				? `Stale: ${problems.map((p) => SOURCE_NAMES[p.source] ?? p.source).join(", ")}`
				: null,
		problems,
	};
}

/** The backend's no-FBC reasons in release terms, each with what to do next. */
const noFbcReasons: Record<string, string> = {
	"no Konflux application":
		"Its Jira fixVersion maps to no Konflux application; check the fixVersion name.",
	"not a concrete quay-vX.Y.Z version":
		"Only Quay versions named quay-vX.Y.Z have FBC builds.",
	"no release candidate yet":
		"It shows here once ART builds a release candidate.",
};

/** The release candidate card's text when a version has no FBC, with the backend's reason. */
export function noFbcText(version: string, reason?: string): string {
	const text = `No FBC build found for v${version}.`;
	if (!reason) return text;
	const why =
		noFbcReasons[reason] ??
		`${reason.charAt(0).toUpperCase()}${reason.slice(1)}.`;
	return `${text} ${why}`;
}
