import type {
	FBCCandidate,
	FBCMember,
	ImageCompare,
	KonfluxRelease,
	OperatorFBC,
	ReadProgress,
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
 * Each job's short name, or for jobs that share one, its name after
 * "redhat-X.Y-" less "e2e-install-" and "-nightly": aws-ocp414-aws-s3.
 */
export function jobLabels(jobs: string[]): string[] {
	const short = jobs.map(jobShortName);
	return jobs.map((job, i) =>
		short.indexOf(short[i]) === short.lastIndexOf(short[i])
			? short[i]
			: (job
					.match(/redhat-\d+\.\d+-(.+)$/)?.[1]
					.replace("e2e-install-", "")
					.replace(NIGHTLY, "") ?? job),
	);
}

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
	return { variant: s, text: `${passed} of ${counted} runs passed` };
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
	if (!same) return { variant: "warning", text: "No CI ran on these FBCs" };
	return {
		variant: "warning",
		text: `${same.text} on an equivalent FBC (same bundle)`,
	};
}

/** Whether an operator has an FBC, on the head bundle or not. */
const hasFBC = (o: OperatorFBC) => o.members.length + o.conflicts.length > 0;

const withFBC = (c: FBCCandidate) => c.operators.filter(hasFBC);

/**
 * The Build step, of the stream's FBCs: how many operators have one, and how
 * many of their OCP version slots do; while history, the days of ART build
 * history left to read, is not 0, no count, as one may be missing.
 */
export function fbcBuildStep(
	c: FBCCandidate,
	history = 0,
): CIStep & { fbcs: string } {
	const built = withFBC(c).length;
	const slots = c.operators.reduce((n, o) => n + o.coverage.slots, 0);
	const missing = c.operators.reduce((n, o) => n + o.missing_ocp.length, 0);
	const fbcs = `${slots - missing} of ${slots} FBCs`;
	if (history > 0)
		return { variant: "info", text: "Reading build history", fbcs };
	return {
		variant: built === c.operators.length ? "success" : "warning",
		text: `${built} of ${c.operators.length} operators built`,
		fbcs,
	};
}

// Operators as the Stage step names them.
const shortNames: Record<string, string> = {
	"container-security-operator": "CSO",
	"quay-bridge-operator": "QBO",
};

/**
 * The candidate's Stage step: not staged for the stream; else how many
 * operators are staged, every FBC's newest Release succeeded, and which
 * failed, any failed Release failing it and an operator the shipment leaves
 * out warning.
 */
export function fbcStageStep(c: FBCCandidate): CIStep {
	if (c.source === "stream")
		return { variant: "pending", text: "Not in a stage shipment" };
	const states = (o: OperatorFBC) =>
		[...o.members, ...o.conflicts].map((m) =>
			m.release ? releaseLine(m.release).status : "running",
		);
	const staged = withFBC(c).filter((o) =>
		states(o).every((s) => s === "done"),
	).length;
	const failed = c.operators
		.filter((o) => states(o).includes("failed"))
		.map((o) => shortNames[o.operator] ?? o.operator);
	const text = `${staged} of ${c.operators.length} operators staged`;
	if (failed.length > 0)
		return {
			variant: "danger",
			text: `${text} · ${failed.join(", ")} release failed`,
		};
	if (withFBC(c).length < c.operators.length)
		return { variant: "warning", text };
	return {
		variant: staged === c.operators.length ? "success" : "info",
		text,
	};
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

/** Why a candidate has no fbc-pullspecs to copy. */
export const pullspecsBlocked = (c: FBCCandidate) =>
	c.errors.length > 0 ? c.errors.join("; ") : stateTexts[c.state];

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

/** The OCP strip's cells: a glyph and what it means. */
export const stripCells = {
	staged: { glyph: "●", text: "staged" },
	built: { glyph: "○", text: "built, not in shipment" },
	other: { glyph: "◐", text: "other build" },
	missing: { glyph: "✗", text: "no FBC" },
	reading: { glyph: "·", text: "reading" },
	unknown: { glyph: "?", text: "no known bundle" },
	absent: { glyph: "–", text: "not part of release" },
	// Outside the record of a shipped release, RR may not know of an FBC yet.
	blank: { glyph: "\u00a0", text: "no FBC known" },
} as const;

export type StripCell = keyof typeof stripCells;

/** An operator whose FBCs a summary row shows, and its OCP strip. */
export interface SummaryRow {
	operator: OperatorFBC;
	cells: StripCell[];
}

const slotCells: Record<SlotRow["kind"], StripCell> = {
	member: "staged",
	other: "other",
	unresolved: "reading",
	missing: "missing",
};

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

/**
 * The operator summary: each OCP version an operator has a slot for, and a
 * row per shown operator with its cell for each.
 */
export function operatorSummary(
	c: FBCCandidate,
	stream: FBCCandidate | null,
	shipped: boolean,
	items: VerdictItem[] = [],
): { ocps: string[]; rows: SummaryRow[] } {
	const picks = shownOperators(c, stream, shipped);
	const ocps = [
		...new Set(
			picks.flatMap(({ operator }) => slotRows(operator).map((r) => r.ocp)),
		),
	].sort(compareVersions);
	const rows = picks.map(({ operator, leftOut }) => {
		const built = leftOut || c.source === "stream";
		const kinds = new Map(slotRows(operator).map((r) => [r.ocp, r.kind]));
		const cells = ocps.map((ocp): StripCell => {
			const kind = kinds.get(ocp);
			if (!kind) return shipped ? "absent" : "blank";
			if (
				kind === "unresolved" &&
				slotItem(items, "no_head_bundle", operator.operator, ocp)
			)
				return "unknown";
			return kind === "member" && built ? "built" : slotCells[kind];
		});
		return { operator, cells };
	});
	return { ocps, rows };
}

/**
 * Whether an operator's block opens: when the verdict has a blocker or
 * warning on it, or the stage shipment leaves it out.
 */
export const blockOpen = (
	{ operator, leftOut }: ShownOperator,
	items: VerdictItem[],
) =>
	leftOut ||
	items.some((i) => i.operator === operator.operator && i.level !== "info");

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
	color: "red" | "orange" | "grey";
	text: string;
	title?: string;
}

/**
 * An FBC row's exception chips. Shipped, only facts of the record: another
 * build and restages; else also a missing or unreadable FBC and a failed
 * stage Release.
 */
export function rowChips(
	r: SlotRow,
	{ shipped, unreadable }: { shipped: boolean; unreadable?: string },
): Chip[] {
	const chips: Chip[] = [];
	if (r.kind === "other")
		chips.push({
			color: "orange",
			text: `other build ${bundleBuilt(r.fbc?.head_bundle_nvr ?? "")}`,
			title: r.fbc?.head_bundle_nvr,
		});
	if ((r.fbc?.staged ?? 0) > 1)
		chips.push({ color: "grey", text: `staged ${r.fbc?.staged}x` });
	if (shipped) return chips;
	if (r.kind === "missing") chips.push({ color: "red", text: "no FBC" });
	if (unreadable)
		chips.push({ color: "red", text: "can't read", title: unreadable });
	const release = r.fbc?.release && releaseLine(r.fbc.release);
	if (release?.status === "failed")
		chips.push({ color: "red", text: "release failed", title: release.text });
	return chips;
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
		n("rebuild", "image rebuilt", "images rebuilt"),
		n("new build", "new build"),
		n("renamed", "image renamed", "images renamed"),
		n("added", "image added", "images added"),
		n("removed", "image removed", "images removed"),
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

/**
 * Where an operator's ART-built images are: in prod for a shipped version
 * once all are, else on stage, or why not; "" until its head bundle is read.
 */
export function imagesState(o: OperatorFBC, shipped: boolean): string {
	const art = o.images.filter((i) => i.nvr);
	if (art.length === 0) return "";
	if (shipped && art.every((i) => i.prod)) return "in prod";
	if (art.every((i) => i.stage)) return "on stage";
	return o.fbc_ri_stage.state === "failed"
		? "push to stage failed"
		: "not on stage";
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
		r.pending > 0 && `FBC contents: ${r.read} of ${total}`,
		r.history_days > 0 &&
			`ART build history: ${r.history_days} day${r.history_days === 1 ? "" : "s"} left`,
	].filter(Boolean);
	if (reading.length === 0)
		return r.failed
			? {
					status: "warning",
					text: `${r.failed} of ${total} FBCs can't be read`,
				}
			: null;
	return {
		status: "running",
		text: `Reading ${reading.join(" · ")}${r.failed ? ` · ${r.failed} can't be read` : ""}`,
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

/** release-from-fbc's fbc-pullspecs while there are none to run with. */
export const FBC_PLACEHOLDER = "<fbc-pullspecs>";

/** A Konflux application's ART group: quay-3-17 is quay-3.17. */
export const artGroup = (app: string) => app.replace(/-(\d+)$/, ".$1");

/**
 * release-from-fbc's params for a version, in a dry run: fbc-pullspecs is a
 * placeholder without pullspecs, date's day is target-release-date, and empty
 * optional params are left out so the Pipeline's defaults apply.
 */
export function fbcParams(
	app: string,
	version: string,
	pullspecs: string | null,
	bugs: string[],
	date?: string,
): Record<string, string> {
	const params: Record<string, string> = {
		group: artGroup(app),
		assembly: version,
		"fbc-pullspecs": pullspecs ?? FBC_PLACEHOLDER,
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
