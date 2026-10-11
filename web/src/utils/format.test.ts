import assert from "node:assert/strict";
import { test } from "node:test";
import { due } from "./format.ts";

// West of UTC, a local date shows a midnight-UTC due date as the day before.
process.env.TZ = "America/New_York";

test("due", () => {
	// 17:35 on Oct 10 in New York, Oct 10 in UTC.
	const now = Date.parse("2026-10-10T21:35:00Z");
	const at = (date: string, shipped = false) =>
		due(`${date}T00:00:00Z`, shipped, now);
	assert.deepEqual(at("2026-10-09"), { date: "Oct 9, 2026", kind: "past" });
	assert.deepEqual(at("2026-10-10"), {
		date: "Oct 10, 2026",
		kind: "soon",
		days: 0,
	});
	assert.deepEqual(at("2026-10-13"), {
		date: "Oct 13, 2026",
		kind: "soon",
		days: 3,
	});
	assert.deepEqual(at("2026-10-14"), { date: "Oct 14, 2026", kind: "none" });
	assert.deepEqual(at("2026-10-09", true), {
		date: "Oct 9, 2026",
		kind: "none",
	});
});
