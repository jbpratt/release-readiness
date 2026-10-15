import {
	Button,
	EmptyState,
	EmptyStateActions,
	EmptyStateBody,
	EmptyStateFooter,
	type EmptyStateProps,
	Spinner,
} from "@patternfly/react-core";
import { ExclamationCircleIcon, SearchIcon } from "@patternfly/react-icons";
import { useMemo } from "react";
import { Link } from "react-router-dom";
import { formatReleaseName } from "../utils/links";

/** A failed fetch: what could not load, why it matters, and a Retry. */
export default function LoadError({
	title,
	children,
	onRetry,
	variant,
}: {
	title: string;
	children: React.ReactNode;
	onRetry: () => void;
	variant?: EmptyStateProps["variant"];
}) {
	return (
		<EmptyState
			status="danger"
			icon={ExclamationCircleIcon}
			titleText={title}
			headingLevel={variant === "xs" ? "h4" : "h2"}
			variant={variant}
		>
			<EmptyStateBody>{children}</EmptyStateBody>
			<EmptyStateFooter>
				<EmptyStateActions>
					<Button variant="primary" onClick={onRetry}>
						Retry
					</Button>
				</EmptyStateActions>
			</EmptyStateFooter>
		</EmptyState>
	);
}

/** A page still loading: a spinner over what it loads. */
export function Loading({
	title,
	variant,
}: {
	title: string;
	variant?: EmptyStateProps["variant"];
}) {
	// EmptyState passes no props to its icon, so the label is bound here; the
	// memo keeps the spinner mounted across renders.
	const icon = useMemo(() => () => <Spinner aria-label={title} />, [title]);
	return <EmptyState icon={icon} titleText={title} variant={variant} />;
}

/** A release the server does not know, and the way back to the list. */
export function ReleaseNotFound({ version }: { version: string }) {
	return (
		<EmptyState
			icon={SearchIcon}
			titleText={`${formatReleaseName(version)} not found`}
			headingLevel="h2"
		>
			<EmptyStateBody>No release by this name is tracked.</EmptyStateBody>
			<EmptyStateFooter>
				<EmptyStateActions>
					<Link to="/">Back to releases</Link>
				</EmptyStateActions>
			</EmptyStateFooter>
		</EmptyState>
	);
}
