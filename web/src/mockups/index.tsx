import { Route, Routes } from "react-router-dom";
import FbcCiTable from "./FbcCiTable";

/** Design mockups on live data, served under /mockups/ in dev builds only. */
const mockups = [{ path: "fbc-ci-b/:version", element: <FbcCiTable /> }];

export default function Mockups() {
	return (
		<Routes>
			{mockups.map((m) => (
				<Route key={m.path} path={m.path} element={m.element} />
			))}
		</Routes>
	);
}
