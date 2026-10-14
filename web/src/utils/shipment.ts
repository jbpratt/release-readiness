import type { ShipmentMR } from "../api/types.ts";

/** A PatternFly Label status; undefined is the default grey. */
type Status = "success" | "warning" | "danger" | "info" | undefined;

const stageText: Record<ShipmentMR["stage"], string> = {
	open: "Shipment MR open",
	stage_released: "Stage released",
	prod_released: "Prod released",
	shipped: "Shipped",
	closed: "Shipment MR closed",
};

/** A shipment MR's stage chip: its text, Label status and tooltip. */
export function shipmentChip(mr: ShipmentMR): {
	text: string;
	status: Status;
	tooltip: string;
} {
	const text = stageText[mr.stage];
	let status: Status = "info";
	if (mr.stage === "shipped") status = "success";
	else if (mr.stage === "closed") status = undefined;
	else if (mr.pipeline_failed) status = "danger";
	const tip = [`!${mr.iid}`, text];
	if (mr.draft) tip.push("draft");
	if (mr.source === "search") tip.push("found by branch search");
	return { text, status, tooltip: tip.join(" · ") };
}

/** The flags shown beside a shipment MR's stage chip, only those set. */
export function shipmentFlags(
	mr: ShipmentMR,
): { text: string; status: Status }[] {
	const flags: { text: string; status: Status }[] = [];
	if (mr.pipeline_failed)
		flags.push({ text: "Pipeline failed", status: "danger" });
	if (mr.needs_approval)
		flags.push({ text: "Needs approval", status: "warning" });
	return flags;
}
