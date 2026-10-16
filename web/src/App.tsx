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
	GithubIcon,
	MoonIcon,
	OutlinedQuestionCircleIcon,
	SunIcon,
} from "@patternfly/react-icons";
import { lazy, Suspense, useEffect, useState } from "react";
import {
	BrowserRouter,
	Navigate,
	Route,
	Routes,
	useLocation,
	useSearchParams,
} from "react-router-dom";
import "@patternfly/react-core/dist/styles/base.css";
import ErrorBoundary from "./components/ErrorBoundary";
import SyncStatusWarning from "./components/SyncStatusWarning";
import "./theme.css";

const ReleasesOverview = lazy(() => import("./pages/ReleasesOverview"));
const ReleaseDetail = lazy(() => import("./pages/ReleaseDetail"));
// Dev builds only: the condition is false in a production build, so the
// mockups chunk is never emitted.
const Mockups = import.meta.env.DEV ? lazy(() => import("./mockups")) : null;

const glossary: [string, string][] = [
	[
		"Release candidate",
		"The pinned build that ships: the build release-from-fbc last staged for the version, whose stage Release succeeded. Running release-from-fbc with a build picks it.",
	],
	[
		"Staged",
		"In the version's stage release, a Konflux Release through the stream's quay-advisory-stage-X-Y plan. Only the release candidate is.",
	],
	[
		"In CI",
		"Reached the catalog CI installs from: a stage Release holding its operator bundle succeeded, a per-build fbc-ri-stage Release or the version's image shipment.",
	],
	[
		"CI",
		"Periodic Prow jobs, grouped by the build each installed: from its tested-images.json, or for runs before 2026-10-09 its olm-csvs.yaml.",
	],
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
		// Inline at every width, so phone width keeps the masthead on one row.
		<Masthead display={{ default: "inline" }}>
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
						<ToolbarItem
							align={{ default: "alignEnd" }}
							visibility={{ default: "hidden", md: "visible" }}
						>
							<SyncStatusWarning />
						</ToolbarItem>
						<ToolbarItem
							align={{ default: "alignEnd" }}
							visibility={{ default: "visible", md: "hidden" }}
						>
							<SyncStatusWarning compact />
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
											Jira.
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
						<ToolbarItem>
							<Button
								variant="plain"
								component="a"
								href="https://github.com/quay/release-readiness"
								target="_blank"
								rel="noopener noreferrer"
								aria-label="GitHub repository"
							>
								<GithubIcon />
							</Button>
						</ToolbarItem>
					</ToolbarContent>
				</Toolbar>
			</MastheadContent>
		</Masthead>
	);

	return <Page masthead={header}>{children}</Page>;
}

/** The old CI page's links open the release page's CI drawer on their cell. */
function CIRedirect() {
	const [params] = useSearchParams();
	const operator = params.get("operator");
	const ocp = params.get("ocp");
	const ci = operator && ocp ? `${operator}@${ocp}` : "all";
	return (
		<Navigate
			to={{ pathname: "..", search: `ci=${ci}` }}
			relative="path"
			replace
		/>
	);
}

function AppRoutes() {
	// Keyed on the path, so navigating away clears a caught error.
	const { pathname } = useLocation();
	return (
		<ErrorBoundary key={pathname}>
			<Suspense
				fallback={
					<Bullseye>
						<Spinner aria-label="Loading page" />
					</Bullseye>
				}
			>
				<Routes>
					<Route path="/" element={<ReleasesOverview />} />
					<Route path="/releases/:version" element={<ReleaseDetail />} />
					<Route path="/releases/:version/ci" element={<CIRedirect />} />
					<Route
						path="/releases/:version/snapshots"
						element={<Navigate to=".." relative="path" replace />}
					/>
					{Mockups && <Route path="/mockups/*" element={<Mockups />} />}
				</Routes>
			</Suspense>
		</ErrorBoundary>
	);
}

export default function App() {
	return (
		<BrowserRouter>
			<AppLayout>
				<AppRoutes />
			</AppLayout>
		</BrowserRouter>
	);
}
