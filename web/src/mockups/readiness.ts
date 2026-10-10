import type {
	BuildTicket,
	KonfluxRelease,
	ReleaseVersion,
	StagedSnapshot,
} from "../api/types.ts";
import { releaseStatus } from "../utils/releaseStatus.ts";

/** v done, x blocking, o waiting or not started. */
export type Mark = "v" | "x" | "o";

/** One stage of the readiness forms, left to right. */
export interface Stage {
	label: string;
	mark: Mark;
	/** The state or count: "Staged", "1 of 3 staged", "74 of 82 not verified". */
	state: string;
	/** When, or the status breakdown. */
	detail: string;
	/** What unblocks an x stage: the next step, or who owns it. */
	next?: string;
	/** What the verdict names for an x or o stage. */
	short?: string;
	/** Catalog: one stage per operator, in OPERATORS order. */
	operators?: Stage[];
	/** Tickets: verified and total. */
	verified?: [number, number];
}

export type Readiness = ReturnType<typeof readiness>;

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
const utcTime = (iso: string) =>
	`${day(iso)} ${new Date(iso).toISOString().slice(11, 16)}Z`;

/** "3.18.1" for "quay-v3.18.1". */
export const versionNumber = (name: string) => name.replace(/^[a-z]+-v/, "");

/** "Due Aug 20, 2026" */
export const dueText = (due?: string) =>
	due ? `Due ${day(due, "numeric")}` : "No due date";

const failedAt = (r: KonfluxRelease) =>
	[r.failed_task, r.failed_step].filter(Boolean).join("/") || "an unknown step";

// The known owner of a failed managed pipeline task, else the fallback.
const nextStep = (r: KonfluxRelease, fallback: string) => {
	const status = releaseStatus(r);
	return ("detail" in status && status.detail) || fallback;
};

// "53 MODIFIED, 20 ON_QA, 1 ASSIGNED"
function byStatus(tickets: BuildTicket[]) {
	const counts = new Map<string, number>();
	for (const t of tickets)
		counts.set(t.status, (counts.get(t.status) ?? 0) + 1);
	return [...counts]
		.sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
		.map(([status, n]) => `${n} ${status}`)
		.join(", ");
}

/** A staged snapshot's stage. Once the version shipped, nothing blocks. */
function stagedStage(
	label: string,
	noun: string,
	staged: StagedSnapshot | null,
	shipped: boolean,
): Stage {
	const r = staged?.release;
	if (!r) {
		if (shipped)
			return {
				label,
				mark: "o",
				state: "No record",
				detail: "Nothing staged on record",
			};
		const detail = staged
			? "ART built it but has not staged it yet"
			: "ART has not staged it yet";
		return {
			label,
			mark: "o",
			state: "Not staged",
			detail,
			short: `ART stages ${noun}`,
		};
	}
	const at = utcTime(r.completion_time ?? r.created_at);
	if (r.released_status === "True")
		return { label, mark: "v", state: "Staged", detail: at };
	if (r.released_reason === "Failed")
		return {
			label,
			mark: shipped ? "o" : "x",
			state: "Staging failed",
			detail: `At ${failedAt(r)}, ${at}`,
			next: nextStep(r, "Ask ART to restage it."),
			short: `${noun} failed to stage`,
		};
	return {
		label,
		mark: "o",
		state: "Staging",
		detail: `ART is staging it, since ${at}`,
		short: `ART stages ${noun}`,
	};
}

/** Each operator's newest staged catalog, as one stage. */
function catalogStage(
	catalogs: Record<string, StagedSnapshot>,
	shipped: boolean,
): Stage {
	const label = "Catalog";
	const operators = OPERATORS.map((op) =>
		stagedStage(op, `the ${op} catalog`, catalogs[op] ?? null, shipped),
	);
	const done = operators.filter((s) => s.mark === "v").length;
	const state = `${done} of ${operators.length} staged`;
	const first =
		operators.find((s) => s.mark === "x") ??
		operators.find((s) => s.mark === "o");
	if (!first) {
		const times = OPERATORS.map((op) => {
			const r = catalogs[op].release!;
			return r.completion_time ?? r.created_at;
		}).sort();
		const detail = `Newest ${utcTime(times[times.length - 1])}`;
		return { label, mark: "v", state, detail, operators };
	}
	// "Not staged: container-security-operator, quay-bridge-operator"
	const byState = new Map<string, string[]>();
	for (const s of operators)
		if (s.mark !== "v")
			byState.set(s.state, [...(byState.get(s.state) ?? []), s.label]);
	// None staged, all for one reason: say it once.
	if (done === 0 && byState.size === 1) return { ...first, label, operators };
	return {
		label,
		mark: first.mark,
		state,
		detail: [...byState]
			.map(([s, ops]) => `${s}: ${ops.join(", ")}`)
			.join("; "),
		next: first.next,
		short: first.short,
		operators,
	};
}

/** Every ticket /build-tickets lists: Target Version tickets and the .z tickets the build names. */
function ticketsStage(tickets: BuildTicket[], shipped: boolean): Stage {
	const label = "Tickets";
	const open = tickets.filter((t) => !VERIFIED.has(t.status.toLowerCase()));
	const verified: [number, number] = [
		tickets.length - open.length,
		tickets.length,
	];
	if (tickets.length === 0)
		return {
			label,
			mark: "v",
			state: "No tickets",
			detail: "No ticket targets it or is in the build",
			verified,
		};
	if (open.length === 0)
		return {
			label,
			mark: "v",
			state: `All ${tickets.length} verified`,
			detail: byStatus(tickets),
			verified,
		};
	return {
		label,
		mark: shipped ? "o" : "x",
		state: `${open.length} of ${tickets.length} not verified`,
		detail: byStatus(open),
		next: "QE verifies them; they are in the list below.",
		short: `${plural(open.length, "ticket")} not verified`,
		verified,
	};
}

/** The Red Hat catalog and Jira say whether the version shipped. */
function shippedStage(released: boolean, shipped: boolean): Stage {
	const label = "Shipped";
	if (shipped)
		return {
			label,
			mark: "v",
			state: "Shipped",
			detail: released
				? "Released in Jira"
				: "In the Red Hat catalog; not released in Jira",
		};
	return {
		label,
		mark: "o",
		state: "Not shipped",
		detail: "Not in the Red Hat catalog; not released in Jira",
		short: "the production release",
	};
}

/**
 * The readiness forms' stages and verdict: Shipped once the version shipped,
 * else the first blocking stage, else the first not done. The focus is the
 * stage the verdict names, and the message what to read under it.
 */
export function readiness(input: {
	release: ReleaseVersion;
	build: StagedSnapshot | null;
	/** Each operator's newest staged catalog. */
	catalogs: Record<string, StagedSnapshot>;
	tickets: BuildTicket[];
	/** The overview's shipped: in the Red Hat catalog or released in Jira. */
	shipped: boolean;
	/** Q4-B: a Shipped stage. Without it the verdict still says Shipped. */
	showShipped: boolean;
	now: number;
}) {
	const { release, now } = input;
	const shipped = release.released || input.shipped;
	const ship = shippedStage(release.released, shipped);
	const all = [
		stagedStage("Build", "the build", input.build, shipped),
		catalogStage(input.catalogs, shipped),
		ticketsStage(input.tickets, shipped),
		ship,
	];
	const focus = shipped
		? ship
		: (all.find((s) => s.mark === "x") ??
			all.find((s) => s.mark === "o") ??
			ship);
	const verdict = shipped
		? ({ color: "green", text: "Shipped" } as const)
		: focus.mark === "x"
			? ({ color: "red", text: `Not ready: ${focus.short}` } as const)
			: ({ color: "blue", text: `Next: ${focus.short}` } as const);
	// computeReadiness's "Past due date": the due date passed and it has not shipped.
	const due = Date.parse(release.due_date ?? "");
	// computeReadiness flags the due day itself, so that day is 1 day late.
	const late = Math.ceil((now - due) / 86_400_000);
	const pastDue =
		!shipped && now > due
			? `Past due date · ${plural(late, "day")} late`
			: undefined;
	return {
		stages: input.showShipped ? all : all.slice(0, -1),
		verdict,
		focus,
		message: focus.mark === "x" ? `Next: ${focus.next}` : focus.detail,
		pastDue,
	};
}
