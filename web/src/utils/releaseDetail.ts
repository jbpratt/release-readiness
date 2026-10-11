import {
	CheckCircleIcon,
	CodeBranchIcon,
	CodeIcon,
	FlaskIcon,
	InProgressIcon,
	OutlinedCheckCircleIcon,
	OutlinedCircleIcon,
	QuestionCircleIcon,
} from "@patternfly/react-icons";
import type {
	BuildCommit,
	CandidateCI,
	ImageScan,
	KonfluxRelease,
	ScanCounts,
} from "../api/types";
import { relative } from "./format.ts";

// Ticket statuses counted as verified.
const VERIFIED = new Set(["release pending", "verified", "closed", "done"]);

const STATUS_ICONS: Record<string, typeof QuestionCircleIcon> = {
	new: OutlinedCircleIcon,
	assigned: InProgressIcon,
	"in progress": InProgressIcon,
	post: CodeBranchIcon,
	modified: CodeIcon,
	on_qa: FlaskIcon,
	verified: OutlinedCheckCircleIcon,
	"release pending": OutlinedCheckCircleIcon,
	closed: CheckCircleIcon,
	done: CheckCircleIcon,
};

/** A ticket status's icon, and whether it counts as verified, so green. */
export function statusIcon(status: string) {
	const s = status.toLowerCase();
	return {
		icon: STATUS_ICONS[s] ?? QuestionCircleIcon,
		verified: VERIFIED.has(s),
	};
}

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

/**
 * A component named without what the page already says, its application and
 * product: quay-3-18-quay-clair is clair.
 */
export const componentLabel = (name: string, app: string) =>
	(name.startsWith(`${app}-`) ? name.slice(app.length + 1) : name).replace(
		/^quay-/,
		"",
	);

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
 * The candidate card's CI warning, when the newest build is not the CI-tested
 * candidate: a newer build is in CI, with the start of the newest run that
 * installed the candidate, if any, and whether the warning names that build,
 * which the action line names when it waits on it or re-runs with it; or
 * "no_pass" when no run that installed the candidate passed.
 */
export function ciWarning(
	ci: CandidateCI,
): { lastRun: string | null; namesLatest: boolean } | "no_pass" | undefined {
	const runs =
		ci.builds
			.find((b) => b.kind === "candidate")
			?.jobs.flatMap((j) => j.runs) ?? [];
	if (!ci.latest_in_ci) {
		return runs.some((r) => r.state === "success") ? undefined : "no_pass";
	}
	let lastRun: string | null = null;
	for (const r of runs) {
		if (!lastRun || Date.parse(r.started_at) > Date.parse(lastRun)) {
			lastRun = r.started_at;
		}
	}
	return {
		lastRun,
		namesLatest: !(
			ci.refresh === "waiting" ||
			(ci.refresh === "rerun" &&
				ci.passed?.snapshot === ci.latest_in_ci.snapshot)
		),
	};
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

export const SEVERITIES = [
	"critical",
	"high",
	"medium",
	"low",
	"unknown",
] as const;

/** A severity's label colour, as master's scan view has it. */
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
