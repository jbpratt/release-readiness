const units: [Intl.RelativeTimeFormatUnit, number][] = [
	["day", 86_400],
	["hour", 3_600],
	["minute", 60],
	["second", 1],
];

export function relative(iso: string): string {
	const secs = Math.round((new Date(iso).getTime() - Date.now()) / 1000);
	const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
	const [unit, size] = units.find(([, size]) => Math.abs(secs) >= size) ?? [
		"second",
		1,
	];
	return rtf.format(Math.round(secs / size), unit);
}

// Due dates are midnight UTC, so a local time zone west of UTC shows the day before.
const day = (iso: string) =>
	new Date(iso).toLocaleDateString("en-US", {
		timeZone: "UTC",
		month: "short",
		day: "numeric",
		year: "numeric",
	});

/** "Due Aug 20, 2026" */
export const dueText = (due?: string) =>
	due ? `Due ${day(due)}` : "No due date";
