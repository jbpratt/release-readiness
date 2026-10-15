import {
	Card,
	CardBody,
	CardTitle,
	DescriptionList,
	DescriptionListDescription,
	DescriptionListGroup,
	DescriptionListTerm,
	EmptyState,
	EmptyStateBody,
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
	ToggleGroup,
	ToggleGroupItem,
	Toolbar,
	ToolbarContent,
	ToolbarGroup,
	ToolbarItem,
} from "@patternfly/react-core";
import { ListIcon, ThIcon } from "@patternfly/react-icons";
import { Link, useSearchParams } from "react-router-dom";
import { getCandidate, listReleasesOverview } from "../api/client";
import type {
	IssueSummary,
	ReadinessResponse,
	ReleaseVersion,
	ShipmentMR,
} from "../api/types";
import { Blockers } from "../components/Blockers";
import LoadError from "../components/LoadError";
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

type ViewMode = "compact" | "expanded";

const muted = { color: "var(--pf-t--global--text--color--subtle)" };

// A blockers popover's one width: one set before its blockers load keeps it
// beside its label, as Popper does not re-place it when it grows.
const popoverWidth = "min(40rem, calc(100vw - 4rem))";

export default function ReleasesOverview() {
	const [searchParams, setSearchParams] = useSearchParams();
	const viewMode = (searchParams.get("view") ?? "compact") as ViewMode;
	const showAll = searchParams.get("all") === "1";

	const config = useConfig();

	const {
		data: overviews,
		loading,
		error,
		refetch,
	} = useCachedFetch("releasesOverview", listReleasesOverview);

	const setParam = (key: string, value: string) => {
		setSearchParams((prev) => {
			const next = new URLSearchParams(prev);
			if (value && value !== "compact") {
				next.set(key, value);
			} else {
				next.delete(key);
			}
			return next;
		});
	};

	if (loading && !overviews) {
		return (
			<PageSection>
				<EmptyState icon={Spinner} titleText="Loading releases…" />
			</PageSection>
		);
	}

	if (!overviews && error) {
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
				<EmptyState>
					<Title headingLevel="h2" size="lg">
						No releases found
					</Title>
					<EmptyStateBody>
						No active releases discovered from Jira. Ensure the Jira sync is
						configured and release tickets with component
						&quot;-area/release&quot; exist.
					</EmptyStateBody>
				</EmptyState>
			</PageSection>
		);
	}

	const galleryMinWidth = viewMode === "compact" ? "300px" : "400px";

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
					<ToolbarGroup align={{ default: "alignEnd" }}>
						<ToolbarItem>
							<ToggleGroup aria-label="View mode">
								<ToggleGroupItem
									icon={<ListIcon />}
									aria-label="Compact view"
									isSelected={viewMode === "compact"}
									onChange={() => setParam("view", "compact")}
								/>
								<ToggleGroupItem
									icon={<ThIcon />}
									aria-label="Expanded view"
									isSelected={viewMode === "expanded"}
									onChange={() => setParam("view", "expanded")}
								/>
							</ToggleGroup>
						</ToolbarItem>
					</ToolbarGroup>
				</ToolbarContent>
			</Toolbar>

			<Gallery
				component="ul"
				hasGutter
				minWidths={{ default: galleryMinWidth }}
			>
				{visible.map((ov) => (
					<ReleaseCard
						key={ov.release.name}
						release={ov.release}
						issueSummary={ov.issue_summary}
						readinessSignal={ov.readiness}
						latestBuild={ov.latest_build}
						shipmentMR={ov.shipment_mr}
						viewMode={viewMode}
						jiraBaseUrl={config?.jira_base_url}
					/>
				))}
			</Gallery>
		</PageSection>
	);
}

function ReleaseCard({
	release,
	issueSummary,
	readinessSignal,
	latestBuild,
	shipmentMR,
	viewMode,
	jiraBaseUrl,
}: {
	release: ReleaseVersion;
	issueSummary?: IssueSummary;
	readinessSignal: ReadinessResponse;
	latestBuild?: string;
	shipmentMR: ShipmentMR | null;
	viewMode: ViewMode;
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
				{viewMode === "compact" ? (
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
								<DescriptionListDescription>
									{ticket}
								</DescriptionListDescription>
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
					</DescriptionList>
				) : (
					<Flex justifyContent={{ default: "justifyContentSpaceBetween" }}>
						{targetDate && (
							<FlexItem>
								<span className="rr-label">Target</span>
								<div>{targetDate}</div>
							</FlexItem>
						)}
						{release.release_ticket_key && (
							<FlexItem>
								<span className="rr-label">Ticket</span>
								<div>{ticket}</div>
							</FlexItem>
						)}
						{showSnapshot && (
							<FlexItem>
								<span className="rr-label">Image snapshot</span>
								<div>{lastBuild}</div>
							</FlexItem>
						)}
						{shipment && (
							<FlexItem>
								<span className="rr-label">Shipment</span>
								<div>{shipment}</div>
							</FlexItem>
						)}
						{readiness && (
							<FlexItem>
								<span className="rr-label">Readiness</span>
								<div>{readiness}</div>
							</FlexItem>
						)}
						{SHOW_CVES && issueSummary && issueSummary.cves > 0 && (
							<FlexItem>
								<span className="rr-label">CVE tickets</span>
								<div>{issueSummary.cves}</div>
							</FlexItem>
						)}
					</Flex>
				)}
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
