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
export const day = (iso: string) =>
	new Date(iso).toLocaleDateString("en-US", {
		timeZone: "UTC",
		month: "short",
		day: "numeric",
		year: "numeric",
	});

const DAY = 86_400_000;

export const days = (n: number) => `${n} day${n === 1 ? "" : "s"}`;

/**
 * A due date, "Aug 20, 2026", and while unshipped whether it has passed or
 * is soon, within 3 days, with the days left: 0 is today.
 */
export function due(
	iso: string,
	shipped: boolean,
	now = Date.now(),
):
	| { date: string; kind: "past" | "none" }
	| { date: string; kind: "soon"; days: number } {
	const date = day(iso);
	const n = Math.floor(Date.parse(iso) / DAY) - Math.floor(now / DAY);
	if (shipped || n > 3) return { date, kind: "none" };
	if (n < 0) return { date, kind: "past" };
	return { date, kind: "soon", days: n };
}
