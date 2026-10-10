// Ticket statuses counted as verified.
const VERIFIED = new Set(["release pending", "verified", "closed", "done"]);

/** How many tickets are not verified yet. */
export const notVerified = (tickets: { status: string }[]) =>
	tickets.filter((t) => !VERIFIED.has(t.status.toLowerCase())).length;

/** A periodic job's install target: ...-e2e-install-aws-s3-nightly is aws-s3. */
export const jobShortName = (job: string) =>
	job.match(/e2e-install-(.+?)(?:-nightly)?$/)?.[1] ?? job;
