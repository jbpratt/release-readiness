import assert from "node:assert/strict";
import { test } from "node:test";
import type { ShipmentMR } from "../api/types.ts";
import { shipmentChip, shipmentFlags } from "./shipment.ts";

function mr(over: Partial<ShipmentMR> = {}): ShipmentMR {
	return {
		iid: 948,
		web_url: "https://gitlab.example/mr/948",
		state: "opened",
		draft: false,
		stage: "open",
		pipeline_failed: false,
		needs_approval: false,
		head_pipeline_status: "success",
		updated_at: "2026-10-14T00:00:00Z",
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
		text: "Shipped",
		status: "success",
		tooltip: "!948 · Shipped",
	});
	assert.deepEqual(shipmentChip(mr({ stage: "closed", state: "closed" })), {
		text: "Shipment MR closed",
		status: undefined,
		tooltip: "!948 · Shipment MR closed",
	});
});

test("shipmentChip is danger when the pipeline failed", () => {
	assert.equal(
		shipmentChip(mr({ stage: "stage_released", pipeline_failed: true })).status,
		"danger",
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
	assert.deepEqual(shipmentFlags(mr({ pipeline_failed: true })), [
		{ text: "Pipeline failed", status: "danger" },
	]);
	assert.deepEqual(shipmentFlags(mr({ needs_approval: true })), [
		{ text: "Needs approval", status: "warning" },
	]);
});
