import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, HashRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import App from "./App";
import "./index.css";

/**
 * TanStack Query is wired in now even though the screens read fixtures, so
 * swapping `lib/fixtures` for real endpoints (M2/M4) is a change of data
 * source rather than a change of architecture.
 */
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Telemetry is live data; a stale cache showing a node as healthy after
      // it has faulted is the failure mode to avoid.
      staleTime: 5_000,
      refetchOnWindowFocus: true,
    },
  },
});

// A standalone preview build has no server to rewrite deep links onto
// index.html, so it routes on the hash instead. The deployed app uses history
// routing, where /nodes/<id> is a real URL an operator can paste to a colleague.
const Router = import.meta.env.VITE_HASH_ROUTER ? HashRouter : BrowserRouter;

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <Router>
        <App />
      </Router>
    </QueryClientProvider>
  </StrictMode>,
);
