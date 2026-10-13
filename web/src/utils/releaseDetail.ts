import type {
	FBCCandidate,
	FBCMember,
	ImageCompare,
	ImageScan,
	KonfluxRelease,
	OperatorFBC,
	ReadProgress,
	ScanCounts,
	UnshippedBuild,
	Verdict,
	VerdictItem,
} from "../api/types";
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
 * Each job's name after "redhat-X.Y-" less "e2e-install-" and "-nightly":
 * aws-ocp422-aws-s3. Other jobs get their short name.
 */
export const jobLabels = (jobs: string[]) =>
	jobs.map(
		(job) =>
			job
				.match(/redhat-\d+\.\d+-(.+)$/)?.[1]
				.replace("e2e-install-", "")
				.replace(NIGHTLY, "") ?? jobShortName(job),
	);

const failedState = (s: string) => s === "failure" || s === "error";

/**
 * A job's status by its runs' Prow states: success when every run that passed
 * or failed passed, danger when none did, warning when both. Aborted and other
 * runs do not count.
 */
export function runsStatus(
	states: string[],
): "success" | "warning" | "danger" | undefined {
	const passed = states.includes("success");
	const failed = states.some(failedState);
	if (passed && failed) return "warning";
	if (passed) return "success";
	return failed ? "danger" : undefined;
}

type CIStep = {
	variant: "success" | "warning" | "danger" | "info" | "pending";
	text: string;
};

/**
 * Runs' step: runsStatus of their states, and when some passed and some
 * failed, how many of those passed; undefined without a counted run.
 */
function runsStep(states: string[]): CIStep | undefined {
	const s = runsStatus(states);
	if (!s) return undefined;
	if (s === "success") return { variant: s, text: "Passed" };
	if (s === "danger") return { variant: s, text: "Failed" };
	const passed = states.filter((st) => st === "success").length;
	const counted = passed + states.filter(failedState).length;
	return { variant: s, text: `${passed}/${counted} passed` };
}

/** Each FBC of a slot, on the head bundle or not, of every operator. */
const slotFBCs = (c: FBCCandidate) =>
	c.operators.flatMap((o) => [...o.members, ...o.conflicts]);

/**
 * The candidate's CI step: the runs that installed one of its FBCs by digest;
 * without one, runs on another FBC of the same bundle only, which do not
 * count as tested.
 */
export function fbcCIStep(c: FBCCandidate): CIStep {
	const states = (tier: "exact" | "equivalent") =>
		slotFBCs(c).flatMap(
			(m) => m.ci?.[tier].flatMap((j) => j.runs.map((r) => r.state)) ?? [],
		);
	const exact = runsStep(states("exact"));
	if (exact) return exact;
	const same = runsStep(states("equivalent"));
	if (!same) return { variant: "warning", text: "No runs" };
	return {
		variant: "warning",
		text: `${same.text} (same bundle)`,
	};
}

/** Whether an operator has an FBC, on the head bundle or not. */
const hasFBC = (o: OperatorFBC) => o.members.length + o.conflicts.length > 0;

/**
 * The Build step, of the stream's FBCs, naming any operator without one;
 * while history, the days of ART build history left to read, is not 0, only
 * that it is reading, as one may be missing.
 */
export function fbcBuildStep(c: FBCCandidate, history = 0): CIStep {
	if (history > 0) return { variant: "info", text: "Reading…" };
	const unbuilt = c.operators.filter((o) => !hasFBC(o)).map(shortName);
	return unbuilt.length > 0
		? { variant: "warning", text: `${unbuilt.join(", ")} not built` }
		: { variant: "success", text: "" };
}

// Operators as the stepper names them.
const shortNames: Record<string, string> = {
	"container-security-operator": "CSO",
	"quay-bridge-operator": "QBO",
};

const shortName = (o: OperatorFBC) => shortNames[o.operator] ?? o.operator;

/** Each of an operator's FBCs' newest stage Release status, running before one. */
const releaseStates = (o: OperatorFBC) =>
	[...o.members, ...o.conflicts].map((m) =>
		m.release ? releaseLine(m.release).status : "running",
	);

/**
 * The candidate's Stage step: not staged for the stream; else any operator
 * whose FBC's newest Release failed fails it, one the shipment leaves out
 * warns, and one with a Release still running is in progress.
 */
export function fbcStageStep(c: FBCCandidate): CIStep {
	if (c.source === "stream") return { variant: "pending", text: "Not staged" };
	const failed = c.operators
		.filter((o) => releaseStates(o).includes("failed"))
		.map(shortName);
	if (failed.length > 0)
		return { variant: "danger", text: `${failed.join(", ")} failed` };
	const unstaged = c.operators.filter((o) => !hasFBC(o)).map(shortName);
	if (unstaged.length > 0)
		return { variant: "warning", text: `${unstaged.join(", ")} not staged` };
	return c.operators.every((o) => releaseStates(o).every((s) => s === "done"))
		? { variant: "success", text: "" }
		: { variant: "info", text: "In progress" };
}

/**
 * A step, in progress rather than a warning or failure while checking, as FBC
 * reads are pending: nothing is wrong with the release until they finish.
 */
export const whileChecking = <S extends CIStep>(s: S, checking: boolean): S =>
	checking && (s.variant === "warning" || s.variant === "danger")
		? { ...s, variant: "info" }
		: s;

// What each candidate state means.
const stateTexts: Record<FBCCandidate["state"], string> = {
	complete: "every operator's FBCs head one bundle",
	incomplete: "an operator or OCP version has no FBC",
	inconsistent: "an operator's FBCs head more than one bundle",
	unknown: "a head bundle or staged catalog is not resolved yet",
};

/**
 * release-from-fbc's fbc-pullspecs, comma-separated, only for a complete
 * candidate; else null.
 */
export const fbcPullspecs = (c: FBCCandidate): string | null =>
	c.state === "complete" && c.pullspecs.length > 0
		? c.pullspecs.join(",")
		: null;

/**
 * Why a candidate has no fbc-pullspecs to run with, naming each operator and
 * the OCP versions it has no FBC for; [] with pullspecs.
 */
export function blockedReasons(c: FBCCandidate): string[] {
	if (fbcPullspecs(c)) return [];
	const reasons = [
		...c.errors,
		...c.operators
			.filter((o) => hasFBC(o) && o.missing_ocp.length > 0)
			.map(
				(o) =>
					`${o.operator} has no FBC for OCP ${[...o.missing_ocp].sort(compareVersions).join(", ")}`,
			),
	];
	return reasons.length > 0 ? reasons : [stateTexts[c.state]];
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

/**
 * The stream's head bundle of an operator when a staged candidate heads
 * another one; else null.
 */
export function streamAhead(
	c: FBCCandidate,
	stream: FBCCandidate | null,
	operator: string,
): string | null {
	if (c.source === "stream") return null;
	const head = c.operators.find(
		(o) => o.operator === operator,
	)?.head_bundle_nvr;
	const ahead = stream?.operators.find(
		(o) => o.operator === operator,
	)?.head_bundle_nvr;
	return ahead && ahead !== head ? ahead : null;
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

/**
 * Why an operator's FBC contents show nothing though read: its read failed,
 * with the error, or its FBC heads no known bundle; else null, read pending.
 */
export function unreadText(
	items: VerdictItem[],
	operator: string,
): string | null {
	const on = (code: string) =>
		items.find((i) => i.operator === operator && i.code === code);
	const failed = on("read_failed");
	if (failed)
		return `Can't read FBC: ${failed.text.replace(/^.*?: can't read FBC: /, "")}`;
	return on("no_head_bundle") ? "FBC heads no known bundle" : null;
}

/** A chip on an FBC row, flagging an exception. */
export interface Chip {
	color: "red" | "orange" | "grey";
	text: string;
	title?: string;
}

/**
 * An FBC row's exception chips. Shipped, only facts of the record: restages;
 * else also a missing or unreadable FBC.
 */
export function rowChips(
	r: SlotRow,
	{ shipped, unreadable }: { shipped: boolean; unreadable?: string },
): Chip[] {
	const chips: Chip[] = [];
	if ((r.fbc?.staged ?? 0) > 1)
		chips.push({ color: "grey", text: `staged ${r.fbc?.staged}x` });
	if (shipped) return chips;
	if (r.kind === "missing") chips.push({ color: "red", text: "no FBC" });
	if (unreadable)
		chips.push({ color: "red", text: "can't read", title: unreadable });
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
		text: `holds bundle ${bundleBuilt(nvr) || nvrLabel(nvr)}`,
		title: nvr,
		href: r.fbc?.build_url || null,
	};
}

/**
 * What changed between two bundles' images: info for rebuilds from the same
 * source only, a warning for any new source, unknown commit, added or removed
 * image.
 */
export function compareLine(c: ImageCompare): {
	level: "info" | "warning";
	text: string;
} {
	const named = (change: string) =>
		c.rows.filter((r) => r.change === change).map((r) => r.name || "bundle");
	const n = (change: string, one: string, many = `${one}s`) => {
		const k = named(change).length;
		return k > 0 ? `${k} ${k === 1 ? one : many}` : "";
	};
	const source = named("new source");
	const parts = [
		source.length > 0 ? `${source.join(", ")} new source` : "",
		n("rebuild", "rebuilt", "rebuilt"),
		n("new build", "new build"),
		n("renamed", "renamed", "renamed"),
		n("added", "added", "added"),
		n("removed", "removed", "removed"),
	].filter(Boolean);
	return {
		level: c.rows.every((r) => r.change === "same" || r.change === "rebuild")
			? "info"
			: "warning",
		text: parts.join(", ") || "same images",
	};
}

/**
 * The image shipment's builds of an operator: those it names, else, as the
 * operands' operator, quay-operator's.
 */
export function unshippedOf(
	builds: UnshippedBuild[],
	operators: string[],
	operator: string,
): UnshippedBuild[] {
	const owner = (name: string) =>
		operators.find((o) => o !== "quay-operator" && name.includes(o)) ??
		"quay-operator";
	return builds.filter((b) => owner(b.name) === operator);
}

/** The version an operator's FBCs ship, by their head CSV: "v3.17.6". */
export const shipsVersion = (o: OperatorFBC) =>
	o.head_csv.includes(".v")
		? o.head_csv.slice(o.head_csv.indexOf(".v") + 1)
		: "";

const MONTHS = "Jan Feb Mar Apr May Jun Jul Aug Sep Oct Nov Dec".split(" ");

/**
 * When ART built a bundle or image, by its NVR's timestamp, in UTC as ART
 * writes it: ...-3.17.6.202610060936.p2... and ...-3.17.6-202610060936.p2...
 * are "Oct 6 09:36"; "" without one.
 */
export function bundleBuilt(nvr: string): string {
	const t = nvr.match(/[.-](\d{4})(\d{2})(\d{2})(\d{2})(\d{2})\./);
	return t ? `${MONTHS[Number(t[2]) - 1]} ${Number(t[3])} ${t[4]}:${t[5]}` : "";
}

/** A date without its time: "Oct 7". */
const day = (iso: string) =>
	new Date(iso).toLocaleDateString(undefined, {
		month: "short",
		day: "numeric",
	});

/** Whether the stage shipment holds an operator's FBCs, or only the stream. */
const notStaged = (c: FBCCandidate, { operator, leftOut }: ShownOperator) =>
	leftOut || c.source === "stream" || !hasFBC(operator);

/**
 * Where an operator's FBCs are on stage, by their newest stage Releases: a
 * failed Release, one running, or on stage since the day the newest finished;
 * null when the stage shipment does not hold them.
 */
export function stageText(
	c: FBCCandidate,
	s: ShownOperator,
): StatusLine | null {
	if (notStaged(c, s)) return null;
	const states = releaseStates(s.operator);
	if (states.includes("failed")) return { status: "failed", text: "Failed" };
	if (states.some((st) => st !== "done"))
		return { status: "running", text: "In progress" };
	const done = [...s.operator.members, ...s.operator.conflicts]
		.map((m) => m.release?.completion_time ?? "")
		.filter(Boolean)
		.sort();
	return {
		status: "done",
		text: done.length > 0 ? day(done[done.length - 1]) : "On stage",
	};
}

/**
 * The verdict's headline; a shipped version's is the record of its prod
 * Release, which the page adds.
 */
export function verdictLine(v: Verdict): StatusLine {
	const blockers = v.items.filter((i) => i.level === "blocker").length;
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

/**
 * A component named without what the page already says, its application and
 * product: quay-3-18-quay-clair is clair.
 */
export const componentLabel = (name: string, app: string) =>
	(name.startsWith(`${app}-`) ? name.slice(app.length + 1) : name).replace(
		/^quay-/,
		"",
	);

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

/** A Release in a word or two: when it was released, else failed or in progress. */
export function releaseWord(r: KonfluxRelease): string {
	const { status } = releaseLine(r);
	if (status === "failed") return "Failed";
	if (status === "running") return "In progress";
	return r.completion_time ? relative(r.completion_time) : "Released";
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
	stream_ahead: "Newer build",
};

/** A verdict label: its level, a word or two, and each item's text and owner. */
export interface ActionLabel {
	level: VerdictItem["level"];
	text: string;
	tip: string[];
}

/** An operator's verdict items as labels, one per code, in the items' order. */
export function actionLabels(
	items: VerdictItem[],
	operator: string,
): ActionLabel[] {
	const labels = new Map<string, ActionLabel>();
	for (const i of items.filter((i) => i.operator === operator)) {
		const l = labels.get(i.code) ?? {
			level: i.level,
			text: actionWords[i.code] ?? i.text,
			tip: [],
		};
		l.tip.push(i.owner ? `${i.text} · ${i.owner}` : i.text);
		labels.set(i.code, l);
	}
	return [...labels.values()];
}

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

export const BASES: Record<ScanCounts["basis"], string> = {
	scanner_arch_findings: "Counted per architecture",
	unique_cves: "Unique CVEs across architectures",
};

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
 * The CVEs cell of a candidate image without counts: its text, colour and
 * link. A pending or unread scan is a muted dash.
 */
export function scanCell(scan: ImageScan | null): {
	text: string;
	color: "orange" | "grey";
	url?: string;
} {
	switch (scan?.state) {
		case "scan_failed":
			return { text: "scan failed", color: "orange", url: scan.url };
		case "not_scanned":
			return { text: "not scanned", color: "grey", url: scan.url };
		default:
			return { text: "-", color: "grey" };
	}
}
