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

const DAY = 86_400_000;

const days = (n: number) => `${n} day${n === 1 ? "" : "s"}`;

/**
 * A due date, "Aug 20, 2026", flagged while unshipped when past, red, or
 * within a week, orange: "Aug 20, 2026 (51 days overdue)", "(in 3 days)".
 */
export function due(
	iso: string,
	shipped: boolean,
	now = Date.now(),
): { text: string; color?: "red" | "orange" } {
	const n = Math.floor(Date.parse(iso) / DAY) - Math.floor(now / DAY);
	if (shipped || n > 7) return { text: day(iso) };
	if (n < 0) return { text: `${day(iso)} (${days(-n)} overdue)`, color: "red" };
	return {
		text: `${day(iso)} (${n === 0 ? "today" : `in ${days(n)}`})`,
		color: "orange",
	};
}
