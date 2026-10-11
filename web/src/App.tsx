import {
	Bullseye,
	Button,
	Content,
	DescriptionList,
	DescriptionListDescription,
	DescriptionListGroup,
	DescriptionListTerm,
	Masthead,
	MastheadBrand,
	MastheadContent,
	MastheadMain,
	Page,
	Popover,
	Spinner,
	Toolbar,
	ToolbarContent,
	ToolbarItem,
} from "@patternfly/react-core";
import {
	ExternalLinkAltIcon,
	MoonIcon,
	OutlinedQuestionCircleIcon,
	SunIcon,
} from "@patternfly/react-icons";
import { lazy, Suspense, useEffect, useState } from "react";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import "@patternfly/react-core/dist/styles/base.css";
import ErrorBoundary from "./components/ErrorBoundary";
import SyncStatusWarning from "./components/SyncStatusWarning";
import "./theme.css";
import { ART_PIPELINES_HEALTH_URL } from "./utils/links";

const ReleasesOverview = lazy(() => import("./pages/ReleasesOverview"));
const ReleaseDetail = lazy(() => import("./pages/ReleaseDetail"));

const glossary: [string, string][] = [
	[
		"Candidate build",
		"The version's build in stage, else its newest build: the newest build whose images' NVRs all carry the version.",
	],
	[
		"Stage",
		"A Konflux Release through the stream's quay-advisory-stage-X-Y plan. A build is in stage once every image is in a Snapshot such a Release succeeded with.",
	],
	[
		"CI",
		"Periodic Prow jobs whose tested-images.json lists the build's quay, clair, operator and bundle digests.",
	],
	["In build", "The build's commits whose message names the ticket."],
	[
		"z-stream",
		"The patch releases of one minor version, e.g. 3.18.z. The next release per stream is its lowest unshipped z.",
	],
];

type Theme = "light" | "dark";

function getInitialTheme(): Theme {
	const stored = localStorage.getItem("theme-preference");
	if (stored === "light" || stored === "dark") return stored;
	if (window.matchMedia("(prefers-color-scheme: dark)").matches) return "dark";
	return "light";
}

function AppLayout({ children }: { children: React.ReactNode }) {
	const [theme, setTheme] = useState<Theme>(getInitialTheme);

	useEffect(() => {
		const root = document.documentElement;
		if (theme === "dark") {
			root.classList.add("pf-v6-theme-dark");
		} else {
			root.classList.remove("pf-v6-theme-dark");
		}
		localStorage.setItem("theme-preference", theme);
	}, [theme]);

	const toggleTheme = () => setTheme((t) => (t === "light" ? "dark" : "light"));

	const header = (
		<Masthead>
			<MastheadMain>
				<MastheadBrand>
					<a
						href="/"
						style={{
							color: "inherit",
							textDecoration: "none",
							fontWeight: 600,
							display: "flex",
							alignItems: "center",
							gap: 8,
						}}
					>
						<img src="/favicon.png" alt="Quay" style={{ height: 32 }} />
						Release Readiness
					</a>
				</MastheadBrand>
			</MastheadMain>
			<MastheadContent>
				<Toolbar>
					<ToolbarContent>
						<ToolbarItem align={{ default: "alignEnd" }}>
							<SyncStatusWarning />
						</ToolbarItem>
						<ToolbarItem>
							<Button
								component="a"
								variant="plain"
								href={ART_PIPELINES_HEALTH_URL}
								target="_blank"
								rel="noopener noreferrer"
								aria-label="ART pipelines health (opens in a new tab)"
								icon={<ExternalLinkAltIcon />}
								iconPosition="end"
							>
								ART pipelines
							</Button>
						</ToolbarItem>
						<ToolbarItem>
							<Popover
								headerContent="About this dashboard"
								maxWidth="36rem"
								bodyContent={
									<>
										<Content component="p">
											One page per version answers: can it ship? Data: Konflux
											Snapshots and Releases, ART build history, Prow periodics,
											Jira, GitHub.
										</Content>
										<DescriptionList isCompact style={{ marginTop: "1rem" }}>
											{glossary.map(([term, desc]) => (
												<DescriptionListGroup key={term}>
													<DescriptionListTerm>{term}</DescriptionListTerm>
													<DescriptionListDescription>
														{desc}
													</DescriptionListDescription>
												</DescriptionListGroup>
											))}
										</DescriptionList>
									</>
								}
							>
								<Button variant="plain" aria-label="About this dashboard">
									<OutlinedQuestionCircleIcon />
								</Button>
							</Popover>
						</ToolbarItem>
						<ToolbarItem>
							<Button
								variant="plain"
								aria-label="Toggle dark mode"
								onClick={toggleTheme}
							>
								{theme === "light" ? <MoonIcon /> : <SunIcon />}
							</Button>
						</ToolbarItem>
					</ToolbarContent>
				</Toolbar>
			</MastheadContent>
		</Masthead>
	);

	return <Page masthead={header}>{children}</Page>;
}

export default function App() {
	return (
		<BrowserRouter>
			<AppLayout>
				<ErrorBoundary>
					<Suspense
						fallback={
							<Bullseye>
								<Spinner />
							</Bullseye>
						}
					>
						<Routes>
							<Route path="/" element={<ReleasesOverview />} />
							<Route path="/releases/:version" element={<ReleaseDetail />} />
							<Route
								path="/releases/:version/snapshots"
								element={<Navigate to=".." relative="path" replace />}
							/>
						</Routes>
					</Suspense>
				</ErrorBoundary>
			</AppLayout>
		</BrowserRouter>
	);
}
