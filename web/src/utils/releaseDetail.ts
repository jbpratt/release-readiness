import type { ImageScan, ScanCounts, SeverityCounts } from "../api/types";

// Ticket statuses counted as verified.
const VERIFIED = new Set(["release pending", "verified", "closed", "done"]);

/** How many tickets are not verified yet. */
export const notVerified = (tickets: { status: string }[]) =>
	tickets.filter((t) => !VERIFIED.has(t.status.toLowerCase())).length;

/** A periodic job's install target: ...-e2e-install-aws-s3-nightly is aws-s3. */
export const jobShortName = (job: string) =>
	job.match(/e2e-install-(.+?)(?:-nightly)?$/)?.[1] ?? job;

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
