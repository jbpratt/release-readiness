import type { ImageScan, ScanCounts, SeverityCounts } from "../api/types";

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

const SEVERITIES = ["critical", "high", "medium", "low", "unknown"] as const;

const BASES: Record<ScanCounts["basis"], string> = {
	scanner_arch_findings: "Counted per architecture",
	unique_cves: "Unique CVEs across architectures",
};

const bySeverity = (c: SeverityCounts) =>
	SEVERITIES.map((s) => `${c[s]} ${s}`).join(", ");

/**
 * A candidate image's CVEs cell: its text and colour, the link, and the
 * counts on hover when scanned. A pending or unread scan is a muted dash.
 */
export function scanCell(scan: ImageScan | null): {
	text: string;
	color?: "red" | "orange" | "grey";
	url?: string;
	detail?: string[];
} {
	switch (scan?.state) {
		case "scanned": {
			const { basis, fixable, no_fix } = scan.counts!;
			const n = SEVERITIES.reduce((sum, s) => sum + fixable[s], 0);
			return {
				text: `${n} fixable`,
				color: n > 0 ? "red" : undefined,
				url: scan.url,
				detail: [
					`Fixable: ${bySeverity(fixable)}`,
					`No fix: ${bySeverity(no_fix)}`,
					BASES[basis],
				],
			};
		}
		case "scan_failed":
			return { text: "scan failed", color: "orange", url: scan.url };
		case "not_scanned":
			return { text: "not scanned", color: "grey", url: scan.url };
		default:
			return { text: "-", color: "grey" };
	}
}
