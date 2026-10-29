import React from "react";
import { createRoot } from "react-dom/client";
import "@fontsource-variable/inter";
import "./styles/tokens.css";
import "./styles/base.css";
import App from "./App";

class ErrorBoundary extends React.Component<{ children: React.ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    if (this.state.failed) {
      return (
        <main className="app-fatal">
          <h1>This session stopped.</h1>
          <p>Reload to reconnect to your library.</p>
          <button className="btn btn--primary" type="button" onClick={() => location.reload()}>
            Reload OhMyAnime
          </button>
        </main>
      );
    }
    return this.props.children;
  }
}

createRoot(document.getElementById("root")!).render(<ErrorBoundary><App /></ErrorBoundary>);
