import {
	Bullseye,
	Button,
	Content,
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
	Navigate,
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
const KonfluxReleases = lazy(() => import("./pages/KonfluxReleases"));

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
												<strong>Build Snapshots</strong> — reads Konflux
												Snapshots and Releases from Kubernetes. Each snapshot
												lists its component builds (git SHA, image).
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
								element={<Navigate to=".." relative="path" replace />}
							/>
							<Route path="/konflux-releases" element={<KonfluxReleases />} />
						</Routes>
					</Suspense>
				</ErrorBoundary>
			</AppLayout>
		</BrowserRouter>
	);
}
