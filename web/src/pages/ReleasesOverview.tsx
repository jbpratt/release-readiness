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
	Progress,
	ProgressMeasureLocation,
	Spinner,
	Switch,
	Title,
	ToggleGroup,
	ToggleGroupItem,
	Toolbar,
	ToolbarContent,
	ToolbarGroup,
	ToolbarItem,
	Tooltip,
} from "@patternfly/react-core";
import {
	CheckCircleIcon,
	ExclamationCircleIcon,
	ExclamationTriangleIcon,
	ListIcon,
	ThIcon,
} from "@patternfly/react-icons";
import { useEffect } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { listReleasesOverview } from "../api/client";
import type {
	IssueSummary,
	ReadinessResponse,
	ReleaseVersion,
} from "../api/types";
import { seedCache, useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { formatReleaseName, jiraIssueUrl } from "../utils/links";

type ViewMode = "compact" | "expanded";

export default function ReleasesOverview() {
	const [searchParams, setSearchParams] = useSearchParams();
	const viewMode = (searchParams.get("view") ?? "compact") as ViewMode;
	const showAll = searchParams.get("all") === "1";

	const config = useConfig();

	const { data: overviews, loading } = useCachedFetch(
		"releasesOverview",
		listReleasesOverview,
	);

	// Seed individual cache entries so detail pages can reuse data
	useEffect(() => {
		if (!overviews) return;
		for (const ov of overviews) {
			seedCache(`readiness:${ov.release.name}`, ov.readiness);
		}
	}, [overviews]);

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
				<div style={{ textAlign: "center" }}>
					<Spinner />
				</div>
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
						No active releases discovered from JIRA. Ensure the JIRA sync is
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
			<Toolbar>
				<ToolbarContent>
					<ToolbarItem>
						<Switch
							id="show-all-versions"
							label="Show all versions"
							isChecked={showAll}
							onChange={(_e, checked) => setParam("all", checked ? "1" : "")}
						/>
					</ToolbarItem>
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

			<Gallery hasGutter minWidths={{ default: galleryMinWidth }}>
				{visible.map((ov) => (
					<ReleaseCard
						key={ov.release.name}
						release={ov.release}
						issueSummary={ov.issue_summary}
						readinessSignal={ov.readiness}
						latestBuild={ov.latest_build}
						shipped={ov.shipped}
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
	shipped,
	viewMode,
	jiraBaseUrl,
}: {
	release: ReleaseVersion;
	issueSummary?: IssueSummary;
	readinessSignal?: ReadinessResponse;
	latestBuild?: string;
	shipped: boolean;
	viewMode: ViewMode;
	jiraBaseUrl?: string;
}) {
	const dueDate = release.due_date ? new Date(release.due_date) : null;
	const releaseDate = release.release_date
		? new Date(release.release_date)
		: null;
	const targetDate = dueDate ?? releaseDate;

	const signalColor = readinessSignal?.signal ?? "grey";
	const signalIcon =
		signalColor === "green" ? (
			<CheckCircleIcon />
		) : signalColor === "red" ? (
			<ExclamationCircleIcon />
		) : signalColor === "yellow" ? (
			<ExclamationTriangleIcon />
		) : undefined;

	const verifiedPercent =
		issueSummary && issueSummary.total > 0
			? Math.round((issueSummary.verified / issueSummary.total) * 100)
			: 0;

	const lastBuild = latestBuild ? (
		<Tooltip content="Newest Konflux snapshot for this version's applications.">
			<span>{new Date(latestBuild).toLocaleDateString()}</span>
		</Tooltip>
	) : (
		"None yet"
	);

	const navigate = useNavigate();
	const displayName = formatReleaseName(release.name);

	const ticketLink =
		release.release_ticket_key && jiraBaseUrl
			? jiraIssueUrl(release.release_ticket_key, jiraBaseUrl)
			: null;

	return (
		<Card
			isCompact
			isClickable
			style={{ cursor: "pointer" }}
			onClick={(e) => {
				if ((e.target as HTMLElement).closest("a, button")) return;
				navigate(`/releases/${encodeURIComponent(release.name)}`);
			}}
		>
			<CardTitle>
				<Flex
					justifyContent={{ default: "justifyContentSpaceBetween" }}
					alignItems={{ default: "alignItemsCenter" }}
				>
					<FlexItem>
						{displayName}
						{shipped && !release.released && (
							<Tooltip content="The JIRA release ticket is still open: ticket hygiene, not release risk.">
								<Label isCompact color="blue" style={{ marginLeft: "0.5rem" }}>
									Shipped, ticket still open
								</Label>
							</Tooltip>
						)}
					</FlexItem>
					<FlexItem>
						{/* A shipped release with an open JIRA ticket shows only its shipped
						    badge: the readiness signal would only reflect the stale ticket. */}
						{readinessSignal && !(shipped && !release.released) && (
							<Label
								color={
									signalColor === "green"
										? "green"
										: signalColor === "red"
											? "red"
											: signalColor === "yellow"
												? "yellow"
												: "grey"
								}
								icon={signalIcon}
								isCompact
							>
								{readinessSignal.message}
							</Label>
						)}
					</FlexItem>
				</Flex>
			</CardTitle>
			<CardBody>
				{viewMode === "compact" ? (
					<DescriptionList isCompact isHorizontal>
						{targetDate && (
							<DescriptionListGroup>
								<DescriptionListTerm>Target</DescriptionListTerm>
								<DescriptionListDescription>
									{targetDate.toLocaleDateString()}
								</DescriptionListDescription>
							</DescriptionListGroup>
						)}
						<DescriptionListGroup>
							<DescriptionListTerm>Last build</DescriptionListTerm>
							<DescriptionListDescription>
								{lastBuild}
							</DescriptionListDescription>
						</DescriptionListGroup>
						{release.release_ticket_key && (
							<DescriptionListGroup>
								<DescriptionListTerm>Ticket</DescriptionListTerm>
								<DescriptionListDescription>
									{ticketLink ? (
										<a
											href={ticketLink}
											target="_blank"
											rel="noopener noreferrer"
											style={{ textDecoration: "none" }}
										>
											{release.release_ticket_key}
										</a>
									) : (
										release.release_ticket_key
									)}
								</DescriptionListDescription>
							</DescriptionListGroup>
						)}
					</DescriptionList>
				) : (
					<Flex direction={{ default: "column" }}>
						<FlexItem>
							<Flex justifyContent={{ default: "justifyContentSpaceBetween" }}>
								{targetDate && (
									<FlexItem>
										<span className="rr-label">Target Date</span>
										<div>{targetDate.toLocaleDateString()}</div>
									</FlexItem>
								)}
								{release.release_ticket_key && (
									<FlexItem>
										<span className="rr-label">Ticket</span>
										<div>
											{ticketLink ? (
												<a
													href={ticketLink}
													target="_blank"
													rel="noopener noreferrer"
													style={{ textDecoration: "none" }}
												>
													{release.release_ticket_key}
												</a>
											) : (
												release.release_ticket_key
											)}
										</div>
									</FlexItem>
								)}
								<FlexItem>
									<span className="rr-label">Last build</span>
									<div>{lastBuild}</div>
								</FlexItem>
								{issueSummary && issueSummary.cves > 0 && (
									<FlexItem>
										<span className="rr-label">CVEs</span>
										<div>{issueSummary.cves}</div>
									</FlexItem>
								)}
							</Flex>
						</FlexItem>
						{issueSummary && issueSummary.total > 0 && (
							<FlexItem>
								<Progress
									value={verifiedPercent}
									title={`${issueSummary.verified}/${issueSummary.total} verified`}
									measureLocation={ProgressMeasureLocation.outside}
									size="sm"
								/>
							</FlexItem>
						)}
					</Flex>
				)}
			</CardBody>
		</Card>
	);
}
