import {
	Breadcrumb,
	BreadcrumbHeading,
	BreadcrumbItem,
	Card,
	CardBody,
	Checkbox,
	FormSelect,
	FormSelectOption,
	Label,
	List,
	ListItem,
	PageSection,
	Spinner,
	Title,
	Toolbar,
	ToolbarContent,
	ToolbarItem,
} from "@patternfly/react-core";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { getReleaseCI } from "../api/client";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { formatReleaseName } from "../utils/links";
import { compareVersions, jobLabels } from "../utils/releaseDetail";

const external = { target: "_blank", rel: "noopener noreferrer" };
const muted = { color: "var(--pf-t--global--text--color--subtle)" };

const when = (iso: string) =>
	new Date(iso).toLocaleString(undefined, {
		month: "short",
		day: "numeric",
		hour: "2-digit",
		minute: "2-digit",
		hourCycle: "h23",
	});

const states = {
	candidate: ["Candidate", "blue"],
	staged: ["Staged", "teal"],
	shipped: ["Shipped", "green"],
} as const;

/**
 * A version's FBC builds, newest first, each with the CI runs that installed
 * from it: each job opens in Sippy and each run in Prow. The operator and OCP
 * filters and whether builds without runs show are query parameters.
 */
export default function ReleaseCI() {
	const { version } = useParams<{ version: string }>();
	const [params, setParams] = useSearchParams();
	const { data, error } = useCachedFetch(`ci:${version}`, () =>
		getReleaseCI(version!),
	);
	const operator = params.get("operator") ?? "";
	const ocp = params.get("ocp") ?? "";
	const all = params.get("all") === "1";
	const set = (key: string, value: string) =>
		setParams(
			(prev) => {
				const next = new URLSearchParams(prev);
				if (value) next.set(key, value);
				else next.delete(key);
				return next;
			},
			{ replace: true },
		);

	const builds = data ?? [];
	const operators = [...new Set(builds.map((b) => b.operator))].sort();
	const ocps = [...new Set(builds.map((b) => b.ocp))].sort(compareVersions);
	const jobs = [...new Set(builds.flatMap((b) => b.runs.map((r) => r.job)))];
	const labels = jobLabels(jobs);
	const shown = builds.filter(
		(b) =>
			(!operator || b.operator === operator) &&
			(!ocp || b.ocp === ocp) &&
			(all || b.runs.length > 0),
	);

	return (
		<PageSection>
			<Breadcrumb style={{ marginBottom: "1rem" }}>
				<BreadcrumbItem>
					<Link to="/">Releases</Link>
				</BreadcrumbItem>
				<BreadcrumbItem>
					<Link to=".." relative="path">
						{formatReleaseName(version!)}
					</Link>
				</BreadcrumbItem>
				<BreadcrumbHeading>FBC CI history</BreadcrumbHeading>
			</Breadcrumb>
			<Card isCompact>
				<CardBody>
					<Title headingLevel="h1" size="lg">
						FBC CI history
					</Title>
					<div style={muted}>
						Runs before 2026-10-09 23:49Z carry no catalog digest.
					</div>
					<Toolbar>
						<ToolbarContent>
							<ToolbarItem>
								<FormSelect
									value={operator}
									onChange={(_e, v) => set("operator", v)}
									aria-label="Operator"
								>
									<FormSelectOption value="" label="All operators" />
									{operators.map((o) => (
										<FormSelectOption key={o} value={o} label={o} />
									))}
								</FormSelect>
							</ToolbarItem>
							<ToolbarItem>
								<FormSelect
									value={ocp}
									onChange={(_e, v) => set("ocp", v)}
									aria-label="OCP version"
									style={{ width: "12rem" }}
								>
									<FormSelectOption value="" label="All OCP versions" />
									{ocps.map((o) => (
										<FormSelectOption key={o} value={o} label={`OCP ${o}`} />
									))}
								</FormSelect>
							</ToolbarItem>
							<ToolbarItem alignSelf="center">
								<Checkbox
									id="all"
									label="Show FBCs without runs"
									isChecked={all}
									onChange={(_e, v) => set("all", v ? "1" : "")}
								/>
							</ToolbarItem>
						</ToolbarContent>
					</Toolbar>
					{error ? (
						<div>Failed to load CI history: {error.message}</div>
					) : !data ? (
						<Spinner size="lg" />
					) : shown.length === 0 ? (
						<div style={muted}>No FBC builds to show.</div>
					) : (
						<List isPlain>
							{shown.map((b) => (
								<ListItem key={b.nvr}>
									<strong>
										{b.operator} {b.ocp}
									</strong>{" "}
									{b.state && (
										<Label isCompact color={states[b.state][1]}>
											{states[b.state][0]}
										</Label>
									)}{" "}
									<span style={muted}>
										{b.nvr}
										{b.built_at && ` · built ${when(b.built_at)}`}
									</span>
									{b.runs.length > 0 && (
										<List isPlain style={{ marginLeft: "1.5rem" }}>
											{b.runs.map((r) => (
												<ListItem key={r.prow_url}>
													<a href={r.sippy_url} title={r.job} {...external}>
														{labels[jobs.indexOf(r.job)]}
													</a>{" "}
													{r.state}{" "}
													<a href={r.prow_url} style={muted} {...external}>
														{when(r.started_at)}
													</a>
												</ListItem>
											))}
										</List>
									)}
								</ListItem>
							))}
						</List>
					)}
				</CardBody>
			</Card>
		</PageSection>
	);
}
