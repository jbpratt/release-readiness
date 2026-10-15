import assert from "node:assert/strict";
import { test } from "node:test";
import type { ShipmentMR } from "../api/types.ts";
import {
	cardStatus,
	shipmentChip,
	shipmentFlags,
	shipmentLabel,
	shipmentText,
} from "./shipment.ts";

function mr(over: Partial<ShipmentMR> = {}): ShipmentMR {
	return {
		iid: 948,
		web_url: "https://gitlab.example/mr/948",
		state: "opened",
		draft: false,
		stage: "open",
		pipeline_failed: false,
		needs_approval: false,
		source: "pointer",
		...over,
	};
}

test("shipmentChip stages", () => {
	assert.deepEqual(shipmentChip(mr()), {
		text: "Shipment MR open",
		status: "info",
		tooltip: "!948 · Shipment MR open",
	});
	assert.deepEqual(shipmentChip(mr({ stage: "stage_released" })), {
		text: "Stage released",
		status: "info",
		tooltip: "!948 · Stage released",
	});
	assert.deepEqual(shipmentChip(mr({ stage: "prod_released" })), {
		text: "Prod released",
		status: "info",
		tooltip: "!948 · Prod released",
	});
	assert.deepEqual(shipmentChip(mr({ stage: "shipped", state: "merged" })), {
		text: "Shipment MR: shipped",
		status: "success",
		tooltip: "!948 · Shipment MR: shipped",
	});
	assert.deepEqual(shipmentChip(mr({ stage: "closed", state: "closed" })), {
		text: "Shipment MR closed",
		status: undefined,
		tooltip: "!948 · Shipment MR closed",
	});
});

test("shipmentChip is not danger when the pipeline failed", () => {
	// The flag carries it, so a card shows danger at most once.
	assert.equal(
		shipmentChip(mr({ stage: "stage_released", pipeline_failed: true })).status,
		"info",
	);
});

test("shipmentChip tooltip names draft and branch search", () => {
	assert.equal(
		shipmentChip(mr({ draft: true, source: "search" })).tooltip,
		"!948 · Shipment MR open · draft · found by branch search",
	);
});

test("shipmentFlags", () => {
	assert.deepEqual(shipmentFlags(mr()), []);
	assert.deepEqual(
		shipmentFlags(mr({ pipeline_failed: true, needs_approval: true })),
		["Pipeline failed", "Needs approval"],
	);
});

test("shipmentLabel of a shipped version reads Shipment MR: shipped", () => {
	// ART can merge the MR after the release ships.
	assert.deepEqual(
		shipmentLabel(mr({ stage: "prod_released", needs_approval: true }), true),
		{
			text: "Shipment MR: shipped",
			status: "success",
			tooltip: "!948 · Prod released",
		},
	);
	assert.equal(shipmentLabel(mr(), false).text, "Shipment MR open");
});

test("shipmentText names the stage and, before shipping, the flags", () => {
	const v3173 = mr({
		stage: "stage_released",
		pipeline_failed: true,
		needs_approval: true,
	});
	assert.equal(
		shipmentText(v3173, false),
		"Stage released · pipeline failed · needs approval",
	);
	// ART can merge the MR after the release ships.
	assert.equal(shipmentText(v3173, true), "Stage released");
});

test("cardStatus of a shipped version is one success label linking the MR", () => {
	assert.deepEqual(
		cardStatus(
			{ signal: "green", message: "Released" },
			mr({ stage: "prod_released", pipeline_failed: true }),
		),
		{ text: "Released", status: "success", link: true },
	);
});

test("cardStatus picks the most severe of readiness and the MR", () => {
	assert.deepEqual(
		cardStatus(
			{ signal: "yellow", message: "No image snapshot yet" },
			mr({
				stage: "stage_released",
				pipeline_failed: true,
				needs_approval: true,
			}),
		),
		{ text: "Pipeline failed", status: "danger", link: true },
	);
	// On a tie the readiness signal wins.
	assert.deepEqual(
		cardStatus(
			{ signal: "red", message: "Past due date" },
			mr({ pipeline_failed: true }),
		),
		{ text: "Past due date", status: "danger", link: false },
	);
	assert.deepEqual(cardStatus({ signal: "", message: "" }, mr()), {
		text: "Shipment MR open",
		status: "info",
		link: true,
	});
});

test("cardStatus without an MR is the readiness label", () => {
	assert.deepEqual(
		cardStatus({ signal: "yellow", message: "Due in 2 days" }, null),
		{ text: "Due in 2 days", status: "warning", link: false },
	);
	assert.equal(cardStatus({ signal: "", message: "" }, null), undefined);
});
