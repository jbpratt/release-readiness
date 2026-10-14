import {
	Button,
	EmptyState,
	EmptyStateActions,
	EmptyStateBody,
	EmptyStateFooter,
	type EmptyStateProps,
} from "@patternfly/react-core";
import { ExclamationCircleIcon } from "@patternfly/react-icons";

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
