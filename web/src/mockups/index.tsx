import type { ReactNode } from "react";
import { Route, Routes } from "react-router-dom";

/** Design mockups on live data, served under /mockups/ in dev builds only. */
const mockups: { path: string; element: ReactNode }[] = [];

export default function Mockups() {
	return (
		<Routes>
			{mockups.map((m) => (
				<Route key={m.path} path={m.path} element={m.element} />
			))}
		</Routes>
	);
}
