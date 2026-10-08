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
	Nav,
	NavItem,
	NavList,
	Page,
	Popover,
	Spinner,
	Toolbar,
	ToolbarContent,
	ToolbarItem,
} from "@patternfly/react-core";
import {
	MoonIcon,
	OutlinedQuestionCircleIcon,
	SunIcon,
} from "@patternfly/react-icons";
import { lazy, Suspense, useEffect, useState } from "react";
import {
	BrowserRouter,
	Link,
	Route,
	Routes,
	useLocation,
} from "react-router-dom";
import "@patternfly/react-core/dist/styles/base.css";
import ErrorBoundary from "./components/ErrorBoundary";
import SyncStatusWarning from "./components/SyncStatusWarning";
import "./theme.css";

const ReleasesOverview = lazy(() => import("./pages/ReleasesOverview"));
const ReleaseDetail = lazy(() => import("./pages/ReleaseDetail"));
const ReleaseSnapshotHistory = lazy(
	() => import("./pages/ReleaseSnapshotHistory"),
);
const KonfluxReleases = lazy(() => import("./pages/KonfluxReleases"));

const glossary: [string, string][] = [
	["Upstream commit", "A commit in the public upstream repo, e.g. quay/quay."],
	[
		"ART build",
		"ART builds each image in Konflux from its build repo: the openshift-priv rebase fork that mirrors the upstream commit.",
	],
	[
		"Konflux snapshot",
		"The set of component images (digest and build repo commit) Konflux records for an application after a build. Konflux tests and releases snapshots.",
	],
	[
		"FBC",
		"File-based catalog: the operator index entries that publish the operator bundles, built as the fbc-quay-X-Y application.",
	],
	[
		"z-stream",
		"The patch releases of one minor version, e.g. 3.18.z. The next release per stream is its lowest unshipped z.",
	],
	[
		"Release pipeline vs shipped",
		"A Konflux Release runs the release pipeline for one snapshot. The product version has shipped only once the Red Hat catalog publishes it or JIRA marks it released.",
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
	const { pathname } = useLocation();

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
						<ToolbarItem isOverflowContainer>
							<Nav variant="horizontal" aria-label="Primary">
								<NavList>
									<NavItem
										isActive={
											pathname === "/" || pathname.startsWith("/releases/")
										}
									>
										<Link to="/">Releases</Link>
									</NavItem>
									<NavItem isActive={pathname === "/konflux-releases"}>
										<Link to="/konflux-releases">Konflux Releases</Link>
									</NavItem>
								</NavList>
							</Nav>
						</ToolbarItem>
						<ToolbarItem align={{ default: "alignEnd" }}>
							<SyncStatusWarning />
						</ToolbarItem>
						<ToolbarItem>
							<Popover
								headerContent="About this dashboard"
								maxWidth="36rem"
								bodyContent={
									<>
										<Content>
											<Content component="p">
												Tracks release readiness for Quay by combining two data
												sources:
											</Content>
											<Content component="ul">
												<Content component="li">
													<strong>JIRA</strong> — syncs active releases by
													Target Version and their issues. Tracks status,
													priority, type, and assignee to compute a readiness
													signal.
												</Content>
												<Content component="li">
													<strong>Build Snapshots</strong> — reads Konflux
													Snapshots and Releases from Kubernetes. Each snapshot
													lists its component builds (git SHA, image).
												</Content>
											</Content>
											<Content component="p">
												A change flows: upstream merge → ART build → snapshot →
												FBC / release.
											</Content>
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
								element={<ReleaseSnapshotHistory />}
							/>
							<Route path="/konflux-releases" element={<KonfluxReleases />} />
						</Routes>
					</Suspense>
				</ErrorBoundary>
			</AppLayout>
		</BrowserRouter>
	);
}
