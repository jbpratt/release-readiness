import {
	Breadcrumb,
	BreadcrumbItem,
	Button,
	Card,
	CardBody,
	CardTitle,
	Flex,
	FlexItem,
	PageSection,
	Spinner,
	Title,
	Tooltip,
} from "@patternfly/react-core";
import { ArrowRightIcon } from "@patternfly/react-icons";
import { Link, useParams } from "react-router-dom";
import {
	getBuildTickets,
	getRelease,
	getReleaseSnapshot,
	getStaged,
	listReleaseSnapshots,
	listReleasesOverview,
} from "../api/client";
import type { BuildTicket, ReleaseVersion, StagedSnapshot } from "../api/types";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { IssuesCard } from "../pages/ReleaseDetail";
import { formatReleaseName, jiraIssueUrl } from "../utils/links";
import { Bar, Pipeline, Strip } from "./ReadinessForms";
import { dueText, OPERATORS, readiness, versionNumber } from "./readiness";
import Switcher, { mockupPath, type Picks, usePicks } from "./Switcher";

const forms: Record<string, typeof Strip> = {
	strip: Strip,
	bar: Bar,
	pipeline: Pipeline,
};

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
	const app = release.konflux_application;
	const minor = versionNumber(release.name).split(".").slice(0, 2).join(".");
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
					<span>{dueText(release.due_date ?? release.release_date)}</span>
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
					<FlexItem align={{ default: "alignRight" }}>
						<Tooltip
							content={`The stream: every ${minor}.z build, the same for each ${minor} version.`}
						>
							<Button
								variant="secondary"
								icon={<ArrowRightIcon />}
								iconPosition="end"
								component={(props: object) => (
									<Link {...props} to={mockupPath(release.name, picks, true)} />
								)}
							>
								Builds, snapshots and CI of {app || "this release"}
							</Button>
						</Tooltip>
					</FlexItem>
				</Flex>
				<ReadinessCard
					release={release}
					picks={picks}
					tickets={tickets?.tickets}
				/>
				<IssuesCard
					data={tickets}
					error={ticketsError}
					version={release.name}
					app={app}
					config={config}
				/>
			</PageSection>
		</>
	);
}

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
	const { data: staged } = useCachedFetch(`staged:${name}`, () =>
		getStaged(name),
	);
	const { data: overview } = useCachedFetch(
		"releasesOverview",
		listReleasesOverview,
	);
	const { data: catalogs } = useCachedFetch(`mockupCatalogs:${name}`, () =>
		newestCatalogs(name, release.konflux_application ?? ""),
	);
	const result =
		staged && catalogs && tickets && overview
			? readiness({
					release,
					build: staged.staged_image,
					catalogs,
					tickets,
					shipped: !!overview.find((o) => o.release.name === name)?.shipped,
					showShipped: picks.q4 === "B",
					now: Date.now(),
				})
			: undefined;
	const Form = forms[picks.readiness];

	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardTitle>Readiness</CardTitle>
			<CardBody>
				{result ? <Form r={result} /> : <Spinner size="md" />}
			</CardBody>
		</Card>
	);
}

/** Each operator's newest staged FBC, named by its single component. */
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
