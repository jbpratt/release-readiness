import {
	Button,
	Card,
	CardBody,
	CardTitle,
	DescriptionList,
	DescriptionListDescription,
	DescriptionListGroup,
	DescriptionListTerm,
	EmptyState,
	EmptyStateActions,
	EmptyStateBody,
	EmptyStateFooter,
	Flex,
	FlexItem,
	Gallery,
	Label,
	PageSection,
	Popover,
	Spinner,
	Stack,
	Switch,
	Title,
	Toolbar,
	ToolbarContent,
	ToolbarItem,
} from "@patternfly/react-core";
import { CubesIcon } from "@patternfly/react-icons";
import { Link, useSearchParams } from "react-router-dom";
import { getCandidate, listReleasesOverview } from "../api/client";
import type {
	IssueSummary,
	ReadinessResponse,
	ReleaseVersion,
	ShipmentMR,
} from "../api/types";
import { Blockers } from "../components/Blockers";
import LoadError, { Loading } from "../components/LoadError";
import { external } from "../components/OperatorImages";
import { mrLink } from "../components/ShipmentLabels";
import { Status } from "../components/Status";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { blockers, splitBlockers } from "../utils/blockers";
import { SHOW_CVES } from "../utils/features";
import { day } from "../utils/format";
import { formatReleaseName, jiraIssueUrl } from "../utils/links";
import { verdictLine } from "../utils/releaseDetail";
import { cardStatus, shipmentChip, shipmentText } from "../utils/shipment";

const muted = { color: "var(--pf-t--global--text--color--subtle)" };

// A blockers popover's one width: one set before its blockers load keeps it
// beside its label, as Popper does not re-place it when it grows.
const popoverWidth = "min(40rem, calc(100vw - 4rem))";

export default function ReleasesOverview() {
	const [searchParams, setSearchParams] = useSearchParams();
	const showAll = searchParams.get("all") === "1";

	const config = useConfig();

	const {
		data: overviews,
		error,
		refetch,
	} = useCachedFetch("releasesOverview", listReleasesOverview);

	const setParam = (key: string, value: string) => {
		setSearchParams((prev) => {
			const next = new URLSearchParams(prev);
			if (value) {
				next.set(key, value);
			} else {
				next.delete(key);
			}
			return next;
		});
	};

	if (!overviews && !error) {
		return (
			<PageSection>
				<Loading title="Loading releases…" />
			</PageSection>
		);
	}

	if (!overviews) {
		return (
			<PageSection>
				<LoadError title="Could not load releases" onRetry={refetch}>
					The server did not return the release list. Try again in a moment.
				</LoadError>
			</PageSection>
		);
	}

	const overviewList = [...(overviews ?? [])].sort((a, b) =>
		b.release.name.localeCompare(a.release.name, undefined, { numeric: true }),
	);
	// Only the next z-release of each stream, unless every version is asked for.
	const visible = showAll
		? overviewList
		: overviewList.filter((ov) => ov.next_in_stream);

	if (overviewList.length === 0) {
		return (
			<PageSection>
				<EmptyState
					icon={CubesIcon}
					titleText="No releases yet"
					headingLevel="h2"
				>
					<EmptyStateBody>
						Releases appear once the Jira sync finds them.
					</EmptyStateBody>
					<EmptyStateFooter>
						<EmptyStateActions>
							<a href="/api/v1/sync-status" {...external}>
								Sync status
							</a>
						</EmptyStateActions>
					</EmptyStateFooter>
				</EmptyState>
			</PageSection>
		);
	}

	return (
		<PageSection>
			<Title
				headingLevel="h1"
				size="lg"
				style={{ marginBottom: "var(--pf-t--global--spacer--md)" }}
			>
				Releases
			</Title>
			<Toolbar>
				<ToolbarContent alignItems="center">
					<ToolbarItem>
						<Switch
							id="show-all-versions"
							label="Show all versions"
							isChecked={showAll}
							onChange={(_e, checked) => setParam("all", checked ? "1" : "")}
						/>
					</ToolbarItem>
					{!showAll && (
						<ToolbarItem style={muted}>Next version in each stream</ToolbarItem>
					)}
				</ToolbarContent>
			</Toolbar>

			{visible.length === 0 ? (
				<EmptyState
					icon={CubesIcon}
					titleText="No next version in any stream"
					headingLevel="h2"
				>
					<EmptyStateFooter>
						<EmptyStateActions>
							<Button variant="primary" onClick={() => setParam("all", "1")}>
								Show all versions
							</Button>
						</EmptyStateActions>
					</EmptyStateFooter>
				</EmptyState>
			) : (
				<Gallery component="ul" hasGutter minWidths={{ default: "300px" }}>
					{visible.map((ov) => (
						<ReleaseCard
							key={ov.release.name}
							release={ov.release}
							issueSummary={ov.issue_summary}
							readinessSignal={ov.readiness}
							latestBuild={ov.latest_build}
							shipmentMR={ov.shipment_mr}
							jiraBaseUrl={config?.jira_base_url}
						/>
					))}
				</Gallery>
			)}
		</PageSection>
	);
}

function ReleaseCard({
	release,
	issueSummary,
	readinessSignal,
	latestBuild,
	shipmentMR,
	jiraBaseUrl,
}: {
	release: ReleaseVersion;
	issueSummary?: IssueSummary;
	readinessSignal: ReadinessResponse;
	latestBuild?: string;
	shipmentMR: ShipmentMR | null;
	jiraBaseUrl?: string;
}) {
	const target = release.due_date ?? release.release_date;
	const targetDate = target && day(target);

	const status = cardStatus(readinessSignal, shipmentMR);
	// The readiness message the header label does not carry, shown as text.
	const readiness =
		readinessSignal.signal && status?.text !== readinessSignal.message
			? readinessSignal.message
			: undefined;
	const shipment = shipmentMR && (
		<>
			<a href={shipmentMR.web_url} {...external}>
				MR !{shipmentMR.iid}
			</a>
			{" · "}
			{shipmentText(shipmentMR, readinessSignal.signal === "green")}
		</>
	);

	// A build time, so its local date, in the same format as the target date.
	const lastBuild = latestBuild
		? new Date(latestBuild).toLocaleDateString("en-US", {
				month: "short",
				day: "numeric",
				year: "numeric",
			})
		: "No image snapshot yet";
	// The status label or the Readiness row already says there is none.
	const showSnapshot = readinessSignal.message !== lastBuild;

	const displayName = formatReleaseName(release.name);
	const releasePath = `/releases/${encodeURIComponent(release.name)}`;

	const ticket =
		release.release_ticket_key && jiraBaseUrl ? (
			<a
				href={jiraIssueUrl(release.release_ticket_key, jiraBaseUrl)}
				target="_blank"
				rel="noopener noreferrer"
				style={{ textDecoration: "none" }}
			>
				{release.release_ticket_key}
			</a>
		) : (
			release.release_ticket_key
		);

	return (
		<Card component="li" isCompact>
			<CardTitle>
				<Flex
					justifyContent={{ default: "justifyContentSpaceBetween" }}
					alignItems={{ default: "alignItemsCenter" }}
				>
					<FlexItem>
						<Title headingLevel="h2" size="md">
							<Link to={releasePath}>{displayName}</Link>
						</Title>
					</FlexItem>
					{status && readinessSignal.signal !== "green" && (
						<FlexItem>
							<Popover
								aria-label={`Why ${displayName} is not shipped`}
								position="bottom-start"
								flipBehavior={[
									"bottom-start",
									"bottom-end",
									"top-start",
									"top-end",
								]}
								minWidth={popoverWidth}
								maxWidth={popoverWidth}
								bodyContent={<CardBlockers release={release} />}
								footerContent={
									<>
										<Link to={releasePath}>View release</Link>
										{status.link && shipmentMR && (
											<>
												{" · "}
												<a href={shipmentMR.web_url} {...external}>
													Open shipment MR
												</a>
											</>
										)}
									</>
								}
							>
								{/* onClick renders the Label as a button, so it takes focus, Enter and Space; the Popover toggles itself. */}
								<Label
									status={status.status}
									isCompact
									onClick={() => undefined}
								>
									{status.text}
								</Label>
							</Popover>
						</FlexItem>
					)}
					{status && readinessSignal.signal === "green" && (
						<FlexItem>
							<Label
								status={status.status}
								isCompact
								render={
									status.link && shipmentMR
										? mrLink(shipmentMR, shipmentChip(shipmentMR).tooltip)
										: undefined
								}
							>
								{status.text}
							</Label>
						</FlexItem>
					)}
				</Flex>
			</CardTitle>
			<CardBody>
				<DescriptionList isCompact isHorizontal>
					{targetDate && (
						<DescriptionListGroup>
							<DescriptionListTerm>Target</DescriptionListTerm>
							<DescriptionListDescription>
								{targetDate}
							</DescriptionListDescription>
						</DescriptionListGroup>
					)}
					{showSnapshot && (
						<DescriptionListGroup>
							<DescriptionListTerm>Image snapshot</DescriptionListTerm>
							<DescriptionListDescription>
								{lastBuild}
							</DescriptionListDescription>
						</DescriptionListGroup>
					)}
					{release.release_ticket_key && (
						<DescriptionListGroup>
							<DescriptionListTerm>Ticket</DescriptionListTerm>
							<DescriptionListDescription>{ticket}</DescriptionListDescription>
						</DescriptionListGroup>
					)}
					{shipment && (
						<DescriptionListGroup>
							<DescriptionListTerm>Shipment</DescriptionListTerm>
							<DescriptionListDescription>
								{shipment}
							</DescriptionListDescription>
						</DescriptionListGroup>
					)}
					{readiness && (
						<DescriptionListGroup>
							<DescriptionListTerm>Readiness</DescriptionListTerm>
							<DescriptionListDescription>
								{readiness}
							</DescriptionListDescription>
						</DescriptionListGroup>
					)}
					{SHOW_CVES && issueSummary && issueSummary.cves > 0 && (
						<DescriptionListGroup>
							<DescriptionListTerm>CVE tickets</DescriptionListTerm>
							<DescriptionListDescription>
								{issueSummary.cves}
							</DescriptionListDescription>
						</DescriptionListGroup>
					)}
				</DescriptionList>
			</CardBody>
		</Card>
	);
}

/**
 * A release's verdict and grouped blockers, fetched when first shown: the
 * overview carries no verdict items.
 */
function CardBlockers({ release }: { release: ReleaseVersion }) {
	const config = useConfig();
	const { data, error, refetch } = useCachedFetch(
		`candidate:${release.name}`,
		() => getCandidate(release.name),
	);
	if (!data) {
		return error ? (
			<LoadError
				variant="xs"
				title="Could not load the blockers"
				onRetry={refetch}
			>
				The server did not return this release's candidate.
			</LoadError>
		) : (
			<Spinner size="md" aria-label="Loading blockers" />
		);
	}
	const head = verdictLine(data.verdict);
	const { blocking, rest } = splitBlockers(
		blockers(data, release.name, release.konflux_application ?? "", config),
	);
	return (
		<Stack hasGutter>
			<Status status={head.status}>
				<strong>{head.text}</strong>
			</Status>
			{blocking.length > 0 && <Blockers groups={blocking} />}
			{rest.length > 0 && (
				<Title headingLevel="h4" size="md">
					Warnings and notes
				</Title>
			)}
			{rest.length > 0 && <Blockers groups={rest} />}
		</Stack>
	);
}
