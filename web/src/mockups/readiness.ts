import type {
	BuildTicket,
	KonfluxRelease,
	ReleaseSnapshot,
	ReleaseVersion,
	StagedSnapshot,
} from "../api/types.ts";
import { releaseStatus } from "../utils/releaseStatus.ts";

/** v done, x blocking, o not started. */
export type Mark = "v" | "x" | "o";

/** One line of the Readiness card. */
export interface Row {
	label: string;
	mark: Mark;
	text: string;
	/** What unblocks an x row: the next step, or who owns it. */
	next?: string;
	/** What the verdict names for an x or o row. */
	short?: string;
}

/** A staged FBC in the Q3-C fixture that dev/fbc-ocp.sh writes. */
export interface OcpCatalog {
	version: string;
	operator: string;
	ocp: string;
	name: string;
	created_at: string;
	status: string;
}

export const OPERATORS = [
	"quay-operator",
	"container-security-operator",
	"quay-bridge-operator",
];

// Ticket statuses counted as verified. Unlike /issues/summary today, Release Pending is one.
const VERIFIED = new Set(["release pending", "verified", "closed", "done"]);

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;

// Due dates are midnight UTC, so a local time zone west of UTC shows the day before.
const day = (iso: string, year?: "numeric") =>
	new Date(iso).toLocaleDateString("en-US", {
		timeZone: "UTC",
		month: "short",
		day: "numeric",
		year,
	});

/** "Oct 9 13:39Z" */
export const utcTime = (iso: string) =>
	`${day(iso)} ${new Date(iso).toISOString().slice(11, 16)}Z`;

/** "3.18.1" for "quay-v3.18.1". */
export const versionNumber = (name: string) => name.replace(/^[a-z]+-v/, "");

/** "Due Aug 20, 2026", with "(50 days late)" when lateness is asked for. */
export function dueText(
	due: string | undefined,
	now: number,
	lateness: boolean,
) {
	if (!due) return "No due date";
	const text = `Due ${day(due, "numeric")}`;
	if (!lateness) return text;
	const late = Math.floor((now - Date.parse(due)) / 86_400_000);
	if (late > 0) return `${text} (${plural(late, "day")} late)`;
	return late === 0
		? `${text} (today)`
		: `${text} (in ${plural(-late, "day")})`;
}

const failedAt = (r: KonfluxRelease) =>
	[r.failed_task, r.failed_step].filter(Boolean).join("/") || "an unknown step";

// The known owner of a failed managed pipeline task, else the fallback.
const nextStep = (r: KonfluxRelease, fallback: string) => {
	const status = releaseStatus(r);
	return ("detail" in status && status.detail) || fallback;
};

function stagedRow(
	label: string,
	noun: string,
	staged: StagedSnapshot | null,
	version: string,
	subject = staged?.name,
): Row {
	const r = staged?.release;
	if (!r) {
		const text = staged
			? `ART built ${noun} but has not staged it yet.`
			: `ART has not staged ${noun} for ${version} yet.`;
		return { label, mark: "o", text, short: `ART stages ${noun}` };
	}
	const at = utcTime(r.completion_time ?? r.created_at);
	if (r.released_status === "True")
		return { label, mark: "v", text: `${subject} staged ${at}.` };
	if (r.released_reason === "Failed")
		return {
			label,
			mark: "x",
			text: `Staging ${noun} failed at ${failedAt(r)}, ${at}.`,
			next: nextStep(r, "Ask ART to restage it."),
			short: `${noun} failed to stage`,
		};
	return {
		label,
		mark: "o",
		text: `ART is staging ${noun}.`,
		short: `ART stages ${noun}`,
	};
}

/** The image snapshot ART staged for the version. */
const buildRow = (staged: StagedSnapshot | null, version: string) =>
	stagedRow("Build", "the build", staged, version);

/** One operator's newest staged catalog. */
export const catalogRow = (
	operator: string,
	staged: StagedSnapshot | null,
	version: string,
) =>
	stagedRow(
		"Catalog",
		`the ${operator} catalog`,
		staged,
		version,
		`${operator} catalog`,
	);

/** Q3-C: one Catalog row per OCP version of the fixture's catalogs. */
export function ocpRows(catalogs: OcpCatalog[], version: string): Row[] {
	const byOcp = new Map<string, OcpCatalog[]>();
	for (const c of catalogs)
		if (c.version === version)
			byOcp.set(c.ocp, [...(byOcp.get(c.ocp) ?? []), c]);
	if (byOcp.size === 0) return [catalogRow(OPERATORS[0], null, version)];
	return [...byOcp]
		.sort(([a], [b]) => a.localeCompare(b, undefined, { numeric: true }))
		.map(([ocp, cs]): Row => {
			const operators = cs
				.sort(
					(a, b) =>
						OPERATORS.indexOf(a.operator) - OPERATORS.indexOf(b.operator),
				)
				.map((c) => c.operator)
				.join(", ");
			// The fixture holds only catalogs that staged, so each row is done.
			return { label: "Catalog", mark: "v", text: `OCP ${ocp}: ${operators}.` };
		});
}

/** The release's Target Version tickets; the .z tickets a build names don't count. */
function ticketsRow(version: string, tickets: BuildTicket[]): Row {
	const label = "Tickets";
	const target = tickets.filter((t) => t.fix_version === version);
	if (target.length === 0) {
		const z = tickets.length
			? ` The list below has the build's ${plural(tickets.length, ".z ticket")}.`
			: "";
		return {
			label,
			mark: "v",
			text: `No ticket targets ${versionNumber(version)}.${z}`,
		};
	}
	const open = target.filter((t) => !VERIFIED.has(t.status.toLowerCase()));
	if (open.length === 0)
		return { label, mark: "v", text: `All ${target.length} verified.` };
	const counts = new Map<string, number>();
	for (const t of open) counts.set(t.status, (counts.get(t.status) ?? 0) + 1);
	const byStatus = [...counts]
		.sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
		.map(([status, n]) => `${n} ${status}`)
		.join(", ");
	return {
		label,
		mark: "x",
		text: `${open.length} of ${target.length} not verified (${byStatus}).`,
		next: "QE verifies them; they are in the list below.",
		short: `${plural(open.length, "ticket")} not verified`,
	};
}

/**
 * The Red Hat catalog and Jira say whether the version shipped. Q4-A also
 * passes the newest production Release snapshot, null when there is none.
 */
function shippedRow(
	release: ReleaseVersion,
	inCatalog: boolean,
	prod?: ReleaseSnapshot | null,
): Row {
	const label = "Shipped";
	const r = prod?.releases?.[0];
	const released = r?.released_status === "True";
	const at = r && utcTime(r.completion_time ?? r.created_at);
	if (inCatalog || release.released) {
		const text = [
			released && `Released to production ${at}.`,
			inCatalog && "In the Red Hat catalog.",
			release.released && "Jira marks it released.",
		]
			.filter(Boolean)
			.join(" ");
		return { label, mark: "v", text };
	}
	const short = "the production release";
	if (prod === undefined)
		return {
			label,
			mark: "o",
			text: "Not in the Red Hat catalog, and Jira has not marked it released.",
			short,
		};
	if (!r)
		return { label, mark: "o", text: "No production release yet.", short };
	if (released)
		return {
			label,
			mark: "v",
			text: `Released to production ${at}; not in the Red Hat catalog yet.`,
		};
	if (r.released_reason === "Failed")
		return {
			label,
			mark: "x",
			text: `Production release failed at ${failedAt(r)}, ${at}.`,
			next: nextStep(r, `Ask ART about Release ${r.name}.`),
			short: "the production release failed",
		};
	return {
		label,
		mark: "o",
		text: "The production release is running.",
		short,
	};
}

/**
 * The Readiness card's rows, top-down, and its verdict: Shipped once the
 * version shipped, else the first blocking row, else the first not started.
 */
export function readiness(input: {
	release: ReleaseVersion;
	build: StagedSnapshot | null;
	/** The Catalog rows the Q3 pick shows. */
	catalogs: Row[];
	tickets: BuildTicket[];
	inCatalog: boolean;
	/** Q4-A only: the newest production Release snapshot, null when none. */
	prod?: ReleaseSnapshot | null;
}) {
	const { release } = input;
	const shipped = shippedRow(release, input.inCatalog, input.prod);
	const rows = [
		buildRow(input.build, versionNumber(release.name)),
		...input.catalogs,
		ticketsRow(release.name, input.tickets),
		shipped,
	];
	const blocking = rows.find((r) => r.mark === "x");
	const next = rows.find((r) => r.mark === "o");
	const verdict =
		shipped.mark === "v"
			? ({ color: "green", text: "Shipped" } as const)
			: blocking
				? ({ color: "red", text: `Not ready: ${blocking.short}` } as const)
				: ({ color: "blue", text: `Next: ${next?.short}` } as const);
	return { rows, verdict };
}
