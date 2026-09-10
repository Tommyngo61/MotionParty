import { Route, Routes } from "react-router-dom";
import { Shell } from "@/components/Shell";
import { Fleet } from "@/pages/Fleet";
import { NodeDetailPage } from "@/pages/NodeDetail";
import { Alerts } from "@/pages/Alerts";
import { Placeholder } from "@/pages/Placeholder";

export default function App() {
  return (
    <Shell>
      <Routes>
        <Route path="/" element={<Fleet />} />
        <Route path="/nodes/:id" element={<NodeDetailPage />} />
        <Route path="/alerts" element={<Alerts />} />
        <Route
          path="/releases"
          element={
            <Placeholder
              title="Agent releases"
              milestone="M5 — command dispatch and release channels"
              what="Signed agent builds, canary and stable channels, staged rollout by percentage, and an automatic halt when the canary cohort's crash or reconnect rate regresses. The schema for this already exists in the agent_releases and agent_rollouts tables."
            />
          }
        />
        <Route
          path="/audit"
          element={
            <Placeholder
              title="Audit log"
              milestone="Backed by the audit_log table, which is already written on every operator action"
              what="Every operator-initiated action, with actor, node, command and result. The rows are being written today by enrollment, token minting and revocation; this view reads them back."
            />
          }
        />
      </Routes>
    </Shell>
  );
}
