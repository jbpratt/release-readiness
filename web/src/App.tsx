import {
	Bullseye,
	Button,
	Content,
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
import { BrowserRouter, Route, Routes } from "react-router-dom";
import "@patternfly/react-core/dist/styles/base.css";
import ErrorBoundary from "./components/ErrorBoundary";
import "./theme.css";
import { ART_PIPELINES_HEALTH_URL } from "./utils/links";

const ReleasesOverview = lazy(() => import("./pages/ReleasesOverview"));
const ReleaseDetail = lazy(() => import("./pages/ReleaseDetail"));
const SnapshotsList = lazy(() => import("./pages/SnapshotsList"));

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
							<Button
								component="a"
								variant="link"
								href={ART_PIPELINES_HEALTH_URL}
								target="_blank"
								rel="noopener noreferrer"
								icon={<ExternalLinkAltIcon />}
								iconPosition="end"
							>
								ART pipelines
							</Button>
						</ToolbarItem>
						<ToolbarItem>
							<Popover
								headerContent="About this dashboard"
								bodyContent={
									<Content>
										<Content component="p">
											Tracks release readiness for Quay by combining two data
											sources:
										</Content>
										<Content component="ul">
											<Content component="li">
												<strong>JIRA</strong> — syncs active releases by Target
												Version and their issues. Tracks status, priority, type,
												and assignee to compute a readiness signal.
											</Content>
											<Content component="li">
												<strong>Build Snapshots</strong> — polls S3 for Konflux
												snapshot manifests. Each contains component builds (git
												SHA, image) and JUnit test results.
											</Content>
										</Content>
									</Content>
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
								element={<SnapshotsList />}
							/>
						</Routes>
					</Suspense>
				</ErrorBoundary>
			</AppLayout>
		</BrowserRouter>
	);
}
