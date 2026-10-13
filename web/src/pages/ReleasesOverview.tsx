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
	Spinner,
	Switch,
	Title,
	ToggleGroup,
	ToggleGroupItem,
	Toolbar,
	ToolbarContent,
	ToolbarGroup,
	ToolbarItem,
} from "@patternfly/react-core";
import {
	CheckCircleIcon,
	ExclamationCircleIcon,
	ExclamationTriangleIcon,
	ListIcon,
	ThIcon,
} from "@patternfly/react-icons";
import { useNavigate, useSearchParams } from "react-router-dom";
import { listReleasesOverview } from "../api/client";
import type {
	IssueSummary,
	ReadinessResponse,
	ReleaseVersion,
} from "../api/types";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { day } from "../utils/format";
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
	viewMode,
	jiraBaseUrl,
}: {
	release: ReleaseVersion;
	issueSummary?: IssueSummary;
	readinessSignal: ReadinessResponse;
	latestBuild?: string;
	viewMode: ViewMode;
	jiraBaseUrl?: string;
}) {
	const target = release.due_date ?? release.release_date;
	const targetDate = target && day(target);

	const signalIcons = {
		green: <CheckCircleIcon />,
		red: <ExclamationCircleIcon />,
		yellow: <ExclamationTriangleIcon />,
	};

	const lastBuild = latestBuild
		? new Date(latestBuild).toLocaleDateString()
		: "None";

	const navigate = useNavigate();
	const displayName = formatReleaseName(release.name);

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
						{release.no_jira && (
							<span
								style={{
									marginLeft: "0.5rem",
									fontWeight: "normal",
									fontSize: "0.85rem",
									color: "var(--pf-t--global--text--color--subtle)",
								}}
							>
								no Jira fixVersion
							</span>
						)}
					</FlexItem>
					{readinessSignal.signal && (
						<FlexItem>
							<Label
								color={readinessSignal.signal}
								icon={signalIcons[readinessSignal.signal]}
								isCompact
							>
								{readinessSignal.message}
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
						<DescriptionListGroup>
							<DescriptionListTerm>Built</DescriptionListTerm>
							<DescriptionListDescription>
								{lastBuild}
							</DescriptionListDescription>
						</DescriptionListGroup>
						{release.release_ticket_key && (
							<DescriptionListGroup>
								<DescriptionListTerm>Ticket</DescriptionListTerm>
								<DescriptionListDescription>
									{ticket}
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
						<FlexItem>
							<span className="rr-label">Built</span>
							<div>{lastBuild}</div>
						</FlexItem>
						{issueSummary && issueSummary.cves > 0 && (
							<FlexItem>
								<span className="rr-label">CVEs</span>
								<div>{issueSummary.cves}</div>
							</FlexItem>
						)}
					</Flex>
				)}
			</CardBody>
		</Card>
	);
}
