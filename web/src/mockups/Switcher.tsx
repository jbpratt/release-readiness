import { Label } from "@patternfly/react-core";
import { Fragment } from "react";
import { Link, useSearchParams } from "react-router-dom";

const VERSIONS = ["quay-v3.18.1", "quay-v3.17.6"];

// The redesign's open questions and their options; A is the recommendation.
const QUESTIONS = {
	q1: {
		title: "Stream snapshot card and ART image builds",
		options: {
			A: "Move them, unchanged, to the Builds page; the version page keeps one link.",
			B: "Keep them on the version page, under the checklist.",
			C: "Delete them.",
		},
	},
	q2: {
		title: "The verdict",
		options: {
			A: "Name the blocker and show lateness as a date in the header.",
			B: 'Keep "Past due date" as the verdict.',
		},
	},
	q3: {
		title: "Catalog (FBC) detail",
		options: {
			A: "One row: the quay-operator catalog, newest of its OCP versions.",
			B: "One row per operator: quay-operator, container-security-operator, quay-bridge-operator.",
			C: "One row per OCP version.",
		},
	},
	q4: {
		title: "The Shipped row",
		options: {
			A: "Include the production Release, which RR already syncs.",
			B: "Show catalog and Jira status only.",
		},
	},
};

type Question = keyof typeof QUESTIONS;
export type Picks = Record<Question, string>;

/** The URL's ?q1= to ?q4= picks; a missing or unknown one is A. */
export function usePicks(): Picks {
	const [params] = useSearchParams();
	const pick = (q: Question) => {
		const value = params.get(q) ?? "";
		return Object.keys(QUESTIONS[q].options).includes(value) ? value : "A";
	};
	return { q1: pick("q1"), q2: pick("q2"), q3: pick("q3"), q4: pick("q4") };
}

export const mockupPath = (version: string, picks: Picks, builds = false) =>
	`/mockups/releases/${encodeURIComponent(version)}${builds ? "/snapshots" : ""}?${new URLSearchParams(picks)}`;

/** Mockup chrome: the version and the pick for each question, as links. */
export default function Switcher({
	version,
	picks,
	builds = false,
}: {
	version: string;
	picks: Picks;
	/** On the Builds page mockup, which Q1-A links to. */
	builds?: boolean;
}) {
	const live = `/releases/${encodeURIComponent(version)}${builds ? "/snapshots" : ""}`;
	return (
		<div
			style={{
				border:
					"2px dashed var(--pf-t--global--border--color--status--warning--default)",
				borderRadius: 6,
				padding: "0.5rem 0.75rem",
				fontSize: "var(--pf-t--global--font--size--sm)",
				lineHeight: 1.6,
			}}
		>
			<div>
				<Label color="orange" isCompact>
					Mockup
				</Label>{" "}
				Proposed release page on live data, one variant per pick. Version:{" "}
				{VERSIONS.map((v) => (
					<Fragment key={v}>
						<Option to={mockupPath(v, picks, builds)} selected={v === version}>
							{v}
						</Option>{" "}
					</Fragment>
				))}
				· <Link to={live}>Live page</Link>
			</div>
			{(Object.keys(QUESTIONS) as Question[]).map((q) => (
				<div key={q}>
					<strong>
						{q.toUpperCase()} {QUESTIONS[q].title}:
					</strong>{" "}
					{Object.entries(QUESTIONS[q].options).map(([option, text]) => (
						<Fragment key={option}>
							<Option
								to={mockupPath(version, { ...picks, [q]: option })}
								selected={picks[q] === option}
							>
								<strong>{option}</strong>
								{option === "A" && " (rec.)"} {text}
							</Option>{" "}
						</Fragment>
					))}
				</div>
			))}
		</div>
	);
}

function Option({
	to,
	selected,
	children,
}: {
	to: string;
	selected: boolean;
	children: React.ReactNode;
}) {
	return (
		<Link
			to={to}
			aria-current={selected ? "page" : undefined}
			style={{
				padding: "0 0.3rem",
				borderRadius: 4,
				...(selected && {
					color: "var(--pf-t--global--text--color--on-brand--default)",
					background: "var(--pf-t--global--color--brand--default)",
				}),
			}}
		>
			{children}
		</Link>
	);
}
