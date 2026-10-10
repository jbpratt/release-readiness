import type {
	BuildTicket,
	KonfluxRelease,
	ReleaseVersion,
	StagedCatalog,
	StagedSnapshot,
	StagedSnapshots,
} from "../api/types.ts";
import { releaseStatus } from "./releaseStatus.ts";

/** v done, x blocking, o waiting or not started. */
export type Mark = "v" | "x" | "o";

/** One stage of the readiness pipeline, left to right. */
export interface Stage {
	label: string;
	mark: Mark;
	/** The state or count: "Staged", "1 of 3 staged", "74 of 82 not verified". */
	state: string;
	/** Why an o stage waits. */
	detail?: string;
	/** What unblocks an x stage: the next step, or who owns it. */
	next?: string;
	/** What the verdict names for an x or o stage. */
	short?: string;
	/** Catalog: one stage per operator, in /staged order. */
	operators?: Stage[];
}

export type Readiness = ReturnType<typeof readiness>;

// Ticket statuses counted as verified.
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

/** "Due Aug 20, 2026" */
export const dueText = (due?: string) =>
	due ? `Due ${day(due, "numeric")}` : "No due date";

// The known owner of a failed managed pipeline task, else the fallback.
const nextStep = (r: KonfluxRelease, fallback: string) => {
	const status = releaseStatus(r);
	return ("detail" in status && status.detail) || fallback;
};

/** A staged snapshot's stage. Once the version shipped, nothing blocks. */
function stagedStage(
	label: string,
	noun: string,
	staged: StagedSnapshot | null,
	shipped: boolean,
): Stage {
	const r = staged?.release;
	if (!r) {
		if (shipped) return { label, mark: "o", state: "No record" };
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
	if (r.released_status === "True")
		return { label, mark: "v", state: "Staged" };
	if (r.released_reason === "Failed")
		return {
			label,
			mark: shipped ? "o" : "x",
			state: "Staging failed",
			next: nextStep(r, "Ask ART to restage it."),
			short: `${noun} failed to stage`,
		};
	return {
		label,
		mark: "o",
		state: "Staging",
		detail: `ART is staging it, since ${utcTime(r.completion_time ?? r.created_at)}`,
		short: `ART stages ${noun}`,
	};
}

/** Each operator's newest staged catalog, as one stage. */
function catalogStage(catalogs: StagedCatalog[], shipped: boolean): Stage {
	const label = "Catalog";
	const operators = catalogs.map((c) =>
		stagedStage(c.operator, `the ${c.operator} catalog`, c.staged, shipped),
	);
	const done = operators.filter((s) => s.mark === "v").length;
	const state = `${done} of ${operators.length} staged`;
	const first =
		operators.find((s) => s.mark === "x") ??
		operators.find((s) => s.mark === "o");
	if (!first) return { label, mark: "v", state, operators };
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
function ticketsStage(
	tickets: BuildTicket[],
	jiraEnabled: boolean,
	shipped: boolean,
): Stage {
	const label = "Tickets";
	// Without a Jira sync, no tickets says nothing about the version.
	if (tickets.length === 0 && !jiraEnabled)
		return {
			label,
			mark: "o",
			state: "Jira not configured",
			detail:
				"Jira sync is not configured on this server, so the version's tickets are unknown.",
			short: "a Jira sync",
		};
	const open = tickets.filter(
		(t) => !VERIFIED.has(t.status.toLowerCase()),
	).length;
	if (open === 0)
		return {
			label,
			mark: "v",
			state: tickets.length ? `All ${tickets.length} verified` : "No tickets",
		};
	return {
		label,
		mark: shipped ? "o" : "x",
		state: `${open} of ${tickets.length} not verified`,
		next: "QE verifies them; they are in the list below.",
		short: `${plural(open, "ticket")} not verified`,
	};
}

/** Shipped, else the first blocking stage, else the first not done, else the production release. */
function verdict(stages: Stage[], released: boolean, shipped: boolean) {
	if (shipped)
		return {
			color: "green",
			text: "Shipped",
			message: released
				? "Released in Jira"
				: "In the Red Hat catalog; not released in Jira",
		} as const;
	const x = stages.find((s) => s.mark === "x");
	if (x)
		return {
			color: "red",
			text: `Not ready: ${x.short}`,
			message: `Next: ${x.next}`,
		} as const;
	const o = stages.find((s) => s.mark === "o");
	if (o)
		return {
			color: "blue",
			text: `Next: ${o.short}`,
			message: o.detail,
		} as const;
	return {
		color: "blue",
		text: "Next: the production release",
		message: "Not in the Red Hat catalog; not released in Jira",
	} as const;
}

/**
 * The readiness pipeline's stages, its verdict with the message to read under
 * it, and the lateness while it applies.
 */
export function readiness(input: {
	release: ReleaseVersion;
	staged: StagedSnapshots;
	tickets: BuildTicket[];
	/** /readiness's shipped: in the Red Hat catalog or released in Jira. */
	shipped: boolean;
	jiraEnabled: boolean;
	now: number;
}) {
	const { release, staged, now } = input;
	const shipped = release.released || input.shipped;
	const tickets = ticketsStage(input.tickets, input.jiraEnabled, shipped);
	// A stream ART does not stage has no build or catalog to wait for.
	const stages = staged.stream_staged
		? [
				stagedStage("Build", "the build", staged.staged_image, shipped),
				catalogStage(staged.catalogs, shipped),
				tickets,
			]
		: [tickets];
	// computeReadiness's "Past due date": the due date passed and it has not shipped.
	const due = Date.parse(release.due_date ?? "");
	// computeReadiness flags the due day itself, so that day is 1 day late.
	const late = Math.ceil((now - due) / 86_400_000);
	const pastDue =
		!shipped && now > due
			? `Past due date · ${plural(late, "day")} late`
			: undefined;
	return {
		stages,
		verdict: verdict(stages, release.released, shipped),
		pastDue,
	};
}
