import type { ReadinessResponse, ShipmentMR } from "../api/types.ts";

/** A PatternFly Label status; undefined is the default grey. */
type Status = "success" | "warning" | "danger" | "info" | undefined;

const stageText: Record<ShipmentMR["stage"], string> = {
	open: "Shipment MR open",
	stage_released: "Stage released",
	prod_released: "Prod released",
	shipped: "Shipped",
	closed: "Shipment MR closed",
};

/**
 * A shipment MR's stage chip: its text, Label status and tooltip. Never
 * danger: a failed pipeline shows as a flag, so a card has danger at most once.
 */
export function shipmentChip(mr: ShipmentMR): {
	text: string;
	status: Status;
	tooltip: string;
} {
	const text = stageText[mr.stage];
	let status: Status = "info";
	if (mr.stage === "shipped") status = "success";
	else if (mr.stage === "closed") status = undefined;
	const tip = [`!${mr.iid}`, text];
	if (mr.draft) tip.push("draft");
	if (mr.source === "search") tip.push("found by branch search");
	return { text, status, tooltip: tip.join(" · ") };
}

/** A shipment MR's flags, only those set. */
export function shipmentFlags(mr: ShipmentMR): string[] {
	const flags: string[] = [];
	if (mr.pipeline_failed) flags.push("Pipeline failed");
	if (mr.needs_approval) flags.push("Needs approval");
	return flags;
}

/**
 * A version's one shipment MR label, linking the MR. Once the version
 * shipped it reads Shipped, whatever the MR's stage: ART can merge it after
 * the release.
 */
export function shipmentLabel(
	mr: ShipmentMR,
	shipped: boolean,
): { text: string; status: Status; tooltip: string } {
	const chip = shipmentChip(mr);
	return shipped ? { ...chip, text: "Shipped", status: "success" } : chip;
}

/**
 * A version's shipment MR stage and, before the version ships, its flags as
 * one line of text, e.g. "Stage released · pipeline failed".
 */
export function shipmentText(mr: ShipmentMR, shipped: boolean): string {
	const flags = shipped ? [] : shipmentFlags(mr);
	return [shipmentChip(mr).text, ...flags.map((f) => f.toLowerCase())].join(
		" · ",
	);
}

const readinessStatus = {
	green: "success",
	yellow: "warning",
	red: "danger",
} as const;

const rank = (s: Status) =>
	s === "danger" ? 3 : s === "warning" ? 2 : s === "success" ? 0 : 1;

/**
 * A release card's one status label: the most severe of the readiness
 * signal and, before the version ships, the shipment MR's pipeline, approval
 * and stage; the readiness signal wins a tie. link: the label links the MR.
 * Readiness is green only once the version is released or shipped.
 */
export function cardStatus(
	readiness: ReadinessResponse,
	mr: ShipmentMR | null,
): { text: string; status: Status; link: boolean } | undefined {
	const shipped = readiness.signal === "green";
	const candidates: { text: string; status: Status; link: boolean }[] = [];
	if (readiness.signal)
		candidates.push({
			text: readiness.message,
			status: readinessStatus[readiness.signal],
			link: shipped && mr !== null,
		});
	if (mr && !shipped) {
		if (mr.pipeline_failed)
			candidates.push({
				text: "Pipeline failed",
				status: "danger",
				link: true,
			});
		if (mr.needs_approval)
			candidates.push({
				text: "Needs approval",
				status: "warning",
				link: true,
			});
		const stage = shipmentChip(mr);
		candidates.push({ text: stage.text, status: stage.status, link: true });
	}
	return candidates.reduce<(typeof candidates)[number] | undefined>(
		(best, c) => (!best || rank(c.status) > rank(best.status) ? c : best),
		undefined,
	);
}
