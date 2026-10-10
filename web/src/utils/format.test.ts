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
	assert.deepEqual(at("2026-08-20"), {
		text: "Aug 20, 2026 (51 days overdue)",
		color: "red",
	});
	assert.deepEqual(at("2026-10-09"), {
		text: "Oct 9, 2026 (1 day overdue)",
		color: "red",
	});
	assert.deepEqual(at("2026-10-10"), {
		text: "Oct 10, 2026 (today)",
		color: "orange",
	});
	assert.deepEqual(at("2026-10-11"), {
		text: "Oct 11, 2026 (in 1 day)",
		color: "orange",
	});
	assert.deepEqual(at("2026-10-17"), {
		text: "Oct 17, 2026 (in 7 days)",
		color: "orange",
	});
	assert.deepEqual(at("2026-10-18"), { text: "Oct 18, 2026" });
	assert.deepEqual(at("2026-08-20", true), { text: "Aug 20, 2026" });
});
