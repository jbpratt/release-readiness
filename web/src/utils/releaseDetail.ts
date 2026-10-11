import type {
	BuildCommit,
	BuildRow,
	CandidateBuild,
	ImageScan,
	KonfluxRelease,
	ScanCounts,
	StageFlag,
} from "../api/types";
import { releaseStatus } from "./releaseStatus.ts";

// Ticket statuses counted as verified.
const VERIFIED = new Set(["release pending", "verified", "closed", "done"]);

/** How many tickets are not verified yet. */
export const notVerified = (tickets: { status: string }[]) =>
	tickets.filter((t) => !VERIFIED.has(t.status.toLowerCase())).length;

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

// A build's titles by role, in the order a build with several joins them.
const TITLES = ["Staged build", "Latest", "Last tested"] as const;

/**
 * A build's box title from its roles: the candidate is the staged build, or
 * when none is in stage the latest.
 */
export function buildTitle(
	roles: BuildRow["roles"],
	source: CandidateBuild["source"],
): string {
	const titles = roles.map((r) =>
		r === "last_tested"
			? "Last tested"
			: r === "candidate" && source === "staged"
				? "Staged build"
				: "Latest",
	);
	return TITLES.filter((t) => titles.includes(t)).join(" · ");
}

/** A line of a build's box: a status icon and the text it explains. */
export interface StatusLine {
	status: "done" | "failed" | "waiting" | "running" | "none";
	text: string;
	/** The Release the line words, linked to its Konflux page. */
	release?: string;
	/** Why, on an info icon. */
	info?: string;
}

const RELEASE_LINE = { green: "done", red: "failed", blue: "running" } as const;

/** A Release's status and where its managed pipeline failed. */
export function releaseLine(r: KonfluxRelease): StatusLine {
	const { color, text } = releaseStatus(r);
	return { status: RELEASE_LINE[color], text, release: r.name };
}

/**
 * Whether a build is in stage, else one line per reason its images are not:
 * a STAGE Release that holds some failed or is running, a bundle ART has not
 * built, no STAGE Release at all. Only images a STAGE Release holds set
 * release.
 */
export function stageLines(stage: StageFlag, app: string): StatusLine[] {
	if (stage.state === "staged") {
		const on = stage.staged_at
			? ` ${new Date(stage.staged_at).toLocaleDateString("en-US", { month: "short", day: "numeric" })}`
			: "";
		return [{ status: "done", text: `Staged${on}` }];
	}
	if (stage.state === "unknown") {
		return [{ status: "none", text: `No stage Release for ${app} yet` }];
	}
	const names = (cs: string[]) =>
		cs.map((c) => componentLabel(c, app)).join(", ");
	const lines: StatusLine[] = [];
	if (stage.release) {
		lines.push(releaseLine(stage.release));
	}
	if (stage.no_bundle.length > 0) {
		lines.push({
			status: "waiting",
			text: `${names(stage.no_bundle)}: built, no bundle yet`,
			info: "ART builds a bundle only after a clean build-layered-products run.",
		});
	}
	if (stage.no_release.length > 0) {
		lines.push({
			status: "waiting",
			text:
				stage.no_release.length === stage.total
					? "Not in a stage Release yet"
					: `${names(stage.no_release)}: not in a stage Release yet`,
		});
	}
	return lines;
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
