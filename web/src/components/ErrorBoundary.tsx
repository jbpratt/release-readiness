import {
	Button,
	EmptyState,
	EmptyStateActions,
	EmptyStateBody,
	EmptyStateFooter,
} from "@patternfly/react-core";
import { ExclamationCircleIcon } from "@patternfly/react-icons";
import { Component } from "react";
import { Link } from "react-router-dom";

interface Props {
	children: React.ReactNode;
}

interface State {
	error: Error | null;
}

export default class ErrorBoundary extends Component<Props, State> {
	state: State = { error: null };

	static getDerivedStateFromError(error: Error): State {
		return { error };
	}

	render() {
		if (this.state.error) {
			return (
				<EmptyState
					status="danger"
					icon={ExclamationCircleIcon}
					titleText="Something went wrong"
					headingLevel="h2"
				>
					<EmptyStateBody>{this.state.error.message}</EmptyStateBody>
					<EmptyStateFooter>
						<EmptyStateActions>
							<Button
								variant="primary"
								onClick={() => window.location.reload()}
							>
								Reload
							</Button>
							<Link to="/">Releases</Link>
						</EmptyStateActions>
					</EmptyStateFooter>
				</EmptyState>
			);
		}
		return this.props.children;
	}
}
