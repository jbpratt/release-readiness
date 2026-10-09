import {
	Breadcrumb,
	BreadcrumbItem,
	Card,
	CardBody,
	CardFooter,
	CardTitle,
	Flex,
	HelperText,
	HelperTextItem,
	Label,
	PageSection,
	Spinner,
	Title,
	Tooltip,
} from "@patternfly/react-core";
import {
	CheckCircleIcon,
	ExclamationCircleIcon,
	OutlinedCircleIcon,
} from "@patternfly/react-icons";
import { Fragment } from "react";
import { Link, useParams } from "react-router-dom";
import {
	getBuildTickets,
	getRelease,
	getReleaseReadiness,
	getReleaseSnapshot,
	getStaged,
	listReleaseSnapshots,
	listReleasesOverview,
} from "../api/client";
import type { BuildTicket, ReleaseVersion, StagedSnapshot } from "../api/types";
import BuildAttempts from "../components/BuildAttempts";
import {
	LatestSnapshot,
	useLatestSnapshot,
} from "../components/ReleaseSnapshots";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { IssuesCard } from "../pages/ReleaseDetail";
import { formatReleaseName, jiraIssueUrl } from "../utils/links";
import fixture from "./fbc-ocp.json";
import {
	catalogRow,
	dueText,
	type Mark,
	OPERATORS,
	ocpRows,
	readiness,
	utcTime,
	versionNumber,
} from "./readiness";
import Switcher, { mockupPath, type Picks, usePicks } from "./Switcher";

/** The proposed version page: a header line, the Readiness card, the tickets. */
export default function ReleaseMockup() {
	const { version } = useParams<{ version: string }>();
	const picks = usePicks();
	const config = useConfig();
	const { data: release, loading } = useCachedFetch(
		version ? `release:${version}` : null,
		() => getRelease(version!),
	);
	const { data: tickets, error: ticketsError } = useCachedFetch(
		version ? `buildTickets:${version}` : null,
		() => getBuildTickets(version!),
	);
	const switcher = (
		<PageSection>
			<Switcher version={version!} picks={picks} />
		</PageSection>
	);

	if (!release) {
		return (
			<>
				{switcher}
				<PageSection>
					{loading ? <Spinner /> : `No data found for release "${version}".`}
				</PageSection>
			</>
		);
	}

	const displayName = formatReleaseName(release.name);
	const ticket = release.release_ticket_key;
	return (
		<>
			{switcher}
			<PageSection>
				<Breadcrumb style={{ marginBottom: "1rem" }}>
					<BreadcrumbItem>
						<Link to="/">Releases</Link>
					</BreadcrumbItem>
					<BreadcrumbItem isActive>{displayName}</BreadcrumbItem>
				</Breadcrumb>
				<Flex
					alignItems={{ default: "alignItemsBaseline" }}
					style={{ marginBottom: "1rem" }}
				>
					<Title headingLevel="h1">{displayName}</Title>
					<span>
						{dueText(
							release.due_date ?? release.release_date,
							Date.now(),
							picks.q2 === "A",
						)}
					</span>
					{ticket && (
						<a
							href={jiraIssueUrl(
								ticket,
								config?.jira_base_url || "https://redhat.atlassian.net",
							)}
							target="_blank"
							rel="noopener noreferrer"
						>
							{ticket}
						</a>
					)}
					<span>{release.release_ticket_assignee || "Unassigned"}</span>
				</Flex>
				<ReadinessCard
					release={release}
					picks={picks}
					tickets={tickets?.tickets}
				/>
				{picks.q1 === "B" && <StreamCards release={release} />}
				<IssuesCard
					data={tickets}
					error={ticketsError}
					version={release.name}
					app={release.konflux_application}
					config={config}
				/>
			</PageSection>
		</>
	);
}

const marks: Record<Mark, React.ReactNode> = {
	v: (
		<CheckCircleIcon
			title="Done"
			style={{
				color: "var(--pf-t--global--icon--color--status--success--default)",
			}}
		/>
	),
	x: (
		<ExclamationCircleIcon
			title="Blocking"
			style={{
				color: "var(--pf-t--global--icon--color--status--danger--default)",
			}}
		/>
	),
	o: (
		<OutlinedCircleIcon
			title="Not started"
			style={{ color: "var(--pf-t--global--icon--color--subtle)" }}
		/>
	),
};

const terms: Record<string, string> = {
	Build:
		"The images ART built for this version and pushed to stage, its pre-production push that QE tests. Konflux records them as a snapshot.",
	Catalog:
		"The operator's OperatorHub entry, a file-based catalog (FBC). ART stages one per operator and OCP version.",
	Tickets:
		"Jira tickets whose Target Version is this release. Release Pending, Verified, Closed and Done count as verified.",
};

const subtle = { color: "var(--pf-t--global--text--color--subtle)" };

function ReadinessCard({
	release,
	picks,
	tickets,
}: {
	release: ReleaseVersion;
	picks: Picks;
	tickets?: BuildTicket[];
}) {
	const { name } = release;
	const app = release.konflux_application ?? "";
	const v = versionNumber(name);
	const dashed = v.replace(/\./g, "-");
	const { data: staged } = useCachedFetch(`staged:${name}`, () =>
		getStaged(name),
	);
	const { data: overview } = useCachedFetch(
		"releasesOverview",
		listReleasesOverview,
	);
	const { data: signal } = useCachedFetch(
		picks.q2 === "B" ? `readiness:${name}` : null,
		() => getReleaseReadiness(name),
	);
	// Same request as the stream card's released list, so they share a cache entry.
	const { data: imageSnapshots } = useCachedFetch(
		picks.q4 === "A" ? `releasedSnapshots:${name}:${app}` : null,
		() =>
			listReleaseSnapshots(name, {
				application: app,
				withRelease: true,
				limit: 100,
				offset: 0,
			}),
	);
	const { data: byOperator } = useCachedFetch(
		picks.q3 === "B" ? `mockupCatalogs:${name}` : null,
		() => newestCatalogs(name, app),
	);

	const catalogs =
		picks.q3 === "A"
			? staged && [catalogRow(OPERATORS[0], staged.staged_fbc, v)]
			: picks.q3 === "B"
				? byOperator &&
					OPERATORS.map((op) => catalogRow(op, byOperator[op] ?? null, v))
				: ocpRows(fixture.catalogs, v);
	const prod =
		picks.q4 === "A"
			? imageSnapshots &&
				(imageSnapshots.snapshots.find((s) =>
					s.name.startsWith(`quay-prod-${dashed}-image-`),
				) ??
					null)
			: undefined;
	const shown =
		staged &&
		catalogs &&
		tickets &&
		overview &&
		(picks.q2 === "A" || signal) &&
		(picks.q4 === "B" || prod !== undefined);
	const result = shown
		? readiness({
				release,
				build: staged.staged_image,
				catalogs,
				tickets,
				inCatalog: !!overview.find((o) => o.release.name === name)?.shipped,
				prod,
			})
		: undefined;
	const verdict =
		picks.q2 === "A"
			? result?.verdict
			: signal && { color: signal.signal, text: signal.message };
	const minor = v.split(".").slice(0, 2).join(".");

	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardTitle>
				Readiness{" "}
				{verdict && (
					<Label color={verdict.color} style={{ marginLeft: "0.5rem" }}>
						{verdict.text}
					</Label>
				)}
			</CardTitle>
			<CardBody>
				{!result ? (
					<Spinner size="md" />
				) : (
					<div
						style={{
							display: "grid",
							gridTemplateColumns: "auto auto 1fr",
							columnGap: "0.75rem",
							rowGap: "0.25rem",
							alignItems: "baseline",
						}}
					>
						{result.rows.map((row, i) => (
							<Fragment key={`${row.label}:${row.text}`}>
								<span>{marks[row.mark]}</span>
								{result.rows[i - 1]?.label === row.label ? (
									<span />
								) : (
									<Tooltip
										content={
											terms[row.label] ??
											(picks.q4 === "A"
												? "The production Release, the Red Hat catalog and Jira's released flag."
												: "The Red Hat catalog and Jira's released flag.")
										}
									>
										<span
											style={{
												fontWeight: 600,
												textDecoration: "underline dotted",
											}}
										>
											{row.label}
										</span>
									</Tooltip>
								)}
								<span>
									{row.text}
									{row.next && <div style={subtle}>{row.next}</div>}
								</span>
							</Fragment>
						))}
					</div>
				)}
				<HelperText style={{ marginTop: "0.5rem" }}>
					{picks.q3 === "C" && (
						<HelperTextItem variant="warning">
							Catalog rows come from a cluster capture at{" "}
							{utcTime(fixture.captured_at)} (dev/fbc-ocp.sh), not live data: C
							needs RR to store each catalog's OCP version.
						</HelperTextItem>
					)}
					<HelperTextItem>
						Build: the images ART built for this version (a Konflux snapshot).
						Staged: pushed to ART's pre-production stage, which QE tests.
						Catalog (FBC): the operator's OperatorHub entry.
					</HelperTextItem>
				</HelperText>
			</CardBody>
			<CardFooter>
				<Tooltip
					content={`The stream: every ${minor}.z build, the same for each ${minor} version.`}
				>
					<Link
						to={
							picks.q1 === "A"
								? mockupPath(name, picks, true)
								: `/releases/${encodeURIComponent(name)}/snapshots`
						}
					>
						Builds of {app || "this release"}
					</Link>
				</Tooltip>
				<span style={subtle}>: snapshots, ART image builds and CI</span>
			</CardFooter>
		</Card>
	);
}

/** Q1-B: the stream cards, under the Readiness card and without tabs. */
function StreamCards({ release }: { release: ReleaseVersion }) {
	const latest = useLatestSnapshot(release.name, release);
	return (
		<>
			<LatestSnapshot version={release.name} state={latest} />
			<BuildAttempts version={release.name} />
		</>
	);
}

/** Q3-B: each operator's newest staged FBC, named by its single component. */
async function newestCatalogs(version: string, app: string) {
	const prefix = `quay-stage-${versionNumber(version).replace(/\./g, "-")}-fbc-`;
	const { snapshots } = await listReleaseSnapshots(version, {
		application: `fbc-${app}`,
		withRelease: true,
		limit: 100,
		offset: 0,
	});
	const found: Record<string, StagedSnapshot> = {};
	for (const s of snapshots) {
		if (OPERATORS.every((op) => op in found)) break;
		if (!s.name.startsWith(prefix) || s.missing) continue;
		const { components } = await getReleaseSnapshot(version, s.name);
		const op = components?.[0]?.name.replace(`fbc-${app}-`, "");
		if (op && !(op in found))
			found[op] = {
				name: s.name,
				created_at: s.created_at,
				release: s.releases?.[0] ?? null,
			};
	}
	return found;
}
