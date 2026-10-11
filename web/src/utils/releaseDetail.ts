import type {
	BuildCommit,
	BuildTickets,
	CandidateCI,
	KonfluxRelease,
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

/**
 * The candidate's CI step: runsStatus of every run that installed it, and
 * when some passed and some failed, how many of those passed.
 */
export function ciStep(ci: CandidateCI): {
	variant: "success" | "warning" | "danger";
	text: string;
} {
	const states =
		ci.builds
			.find((b) => b.kind === "candidate")
			?.jobs.flatMap((j) => j.runs.map((r) => r.state)) ?? [];
	const s = runsStatus(states);
	if (!s) return { variant: "warning", text: "Not tested" };
	if (s === "success") return { variant: s, text: "Passed" };
	if (s === "danger") return { variant: s, text: "Failed" };
	const passed = states.filter((st) => st === "success").length;
	const counted = passed + states.filter(failedState).length;
	return { variant: s, text: `${passed} of ${counted} runs passed` };
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

// The not_compared reasons of a GitHub API error, which the next pass retries.
const API_ERRORS = new Set([
	"github api error",
	"default branch: github api error",
]);

/**
 * The Tickets card's not-compared lines: the components not compared, with
 * the reason, then one line for all those a GitHub API error left.
 */
export function notComparedLines(
	notCompared: BuildTickets["not_compared"],
	app: string,
): string[] {
	const other = notCompared.filter((c) => !API_ERRORS.has(c.reason));
	const lines: string[] = [];
	if (other.length > 0) {
		lines.push(
			`Not compared: ${other.map((c) => `${componentLabel(c.component, app)} (${c.reason})`).join(", ")}`,
		);
	}
	if (other.length < notCompared.length) {
		lines.push("Not compared with GitHub: API error (retrying)");
	}
	return lines;
}

/**
 * A ticket's not_in_build commits once each, titled with the components that
 * lack them: an operator and its bundle share their commits.
 */
export function missingCommits(
	commits: BuildCommit[],
	app: string,
): { sha: string; url: string; title: string }[] {
	const out: { sha: string; url: string; title: string }[] = [];
	for (const c of commits) {
		const name = componentLabel(c.component, app);
		const seen = out.find((o) => o.sha === c.commit_sha);
		if (seen) {
			seen.title += `, ${name}`;
		} else {
			out.push({ sha: c.commit_sha, url: c.commit_url, title: name });
		}
	}
	return out;
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
	status: "done" | "failed" | "running" | "warning" | "none";
	text: string;
}

/**
 * The --fbc-pullspecs value to run release-from-fbc with ci's passed build:
 * its FBC pullspecs, comma-separated, when it has one for every operator and
 * OCP version; else null.
 */
export function fbcCopy(ci: CandidateCI): string | null {
	const fbc = ci.fbc;
	return fbc && fbc.missing.length === 0 && fbc.pullspecs.length > 0
		? fbc.pullspecs.join(",")
		: null;
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
