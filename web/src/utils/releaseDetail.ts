import type {
	CandidateCatalog,
	FBCCandidate,
	FBCMember,
	KonfluxRelease,
	OperatorFBC,
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
	return {
		variant: "warning",
		text: same ? `Not tested · same bundle: ${same.text}` : "Not tested",
	};
}

/**
 * The candidate's Stage step: not staged for the stream, else by the newest
 * Release of each staged slot's Snapshot, a failed one first. Released slots
 * are a warning while the candidate has errors, as a missing operator.
 */
export function fbcStageStep(c: FBCCandidate): CIStep {
	if (c.source === "stream") return { variant: "pending", text: "Not staged" };
	const states = slotFBCs(c).map((m) =>
		m.release ? releaseLine(m.release).status : "running",
	);
	const failed = states.filter((s) => s === "failed").length;
	if (failed > 0)
		return {
			variant: "danger",
			text: `${failed} Release${failed === 1 ? "" : "s"} failed`,
		};
	if (states.length > 0 && states.every((s) => s === "done"))
		return c.errors.length > 0
			? { variant: "warning", text: "Released with errors" }
			: { variant: "success", text: "Released" };
	return { variant: "info", text: "Release in progress" };
}

// What each candidate state means.
const stateTexts: Record<FBCCandidate["state"], string> = {
	complete: "every operator's FBCs head one bundle",
	incomplete: "an operator or OCP version has no FBC",
	inconsistent: "an operator's FBCs head more than one bundle",
	unknown: "a head bundle or staged catalog is not resolved yet",
};

/** The candidate's headline: where its FBCs come from and their state. */
export function candidateLine(c: FBCCandidate): StatusLine {
	const source =
		c.source === "stream" ? "Not staged: the stream's newest FBCs" : "Staged";
	const status =
		c.state === "complete"
			? c.source === "stream"
				? "warning"
				: "done"
			: c.state === "unknown"
				? "warning"
				: "failed";
	return { status, text: `${source}; ${stateTexts[c.state]}.` };
}

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

/** An operator's image check: the image Snapshot's bundle against its head. */
export function imageCheckLine(o: OperatorFBC): string {
	const { state, snapshot, bundle_nvr } = o.image_check;
	if (state === "matches")
		return `Image Snapshot ${snapshot} holds this bundle`;
	if (state === "differs")
		return `Image Snapshot ${snapshot} holds bundle ${nvrLabel(bundle_nvr)}`;
	return snapshot
		? `Image Snapshot ${snapshot}: no bundle to compare`
		: "No image Snapshot to compare";
}

/** Whether an operator's head bundle reached stage through fbc-ri. */
export const fbcRIText: Record<OperatorFBC["fbc_ri_stage"]["state"], string> = {
	in: "installable",
	failed: "fbc-ri Release failed",
	none: "not installable: no fbc-ri Release",
	unknown: "installability unknown",
};

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

const fbcs = (n: number) => `${n} FBC${n === 1 ? "" : "s"}`;

/**
 * A step's FBC line, null without a catalog to show: at Build, how many
 * catalogs are staged and how many built, not staged; at Stage, how many are
 * staged and how their Releases went.
 */
export function fbcLine(
	catalogs: CandidateCatalog[],
	step: "build" | "stage",
): string | null {
	const staged = catalogs.filter((c) => c.state === "staged");
	const built = catalogs.length - staged.length;
	if (step === "build") {
		const parts = [
			staged.length > 0 && `${fbcs(staged.length)} staged`,
			built > 0 &&
				`${staged.length > 0 ? built : fbcs(built)} built, not staged`,
		].filter(Boolean);
		return parts.length > 0 ? parts.join(" · ") : null;
	}
	if (staged.length === 0) return null;
	const states = staged.map((c) =>
		c.release ? releaseLine(c.release).status : "running",
	);
	const failed = states.filter((s) => s === "failed").length;
	const outcome = failed
		? `${failed} Release${failed === 1 ? "" : "s"} failed`
		: states.every((s) => s === "done")
			? "Released"
			: "Release in progress";
	return `${fbcs(staged.length)} staged · ${outcome}`;
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
