import assert from "node:assert/strict";
import { test } from "node:test";
import { dueText } from "./format.ts";

// West of UTC, a local date shows a midnight-UTC due date as the day before.
process.env.TZ = "America/New_York";

test("dueText", () => {
	assert.equal(dueText("2026-08-20T00:00:00Z"), "Due Aug 20, 2026");
});
