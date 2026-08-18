import { LitElement, html } from "lit";
import { customElement, state } from "lit/decorators.js";
import { setupSentry } from "./sentry.js";
import "./index.css";
import "./components/chat-app.js";
import "./gallery.js";

if (import.meta.env.DEV) {
  import("./crash/index.js");
}

setupSentry();

// Port of the React use-location hook: notifies on popstate and on
// pushState/replaceState calls so the pathname branch below stays in sync.
function subscribeLocation(onChange: () => void): () => void {
  window.addEventListener("popstate", onChange);

  const originalPushState = history.pushState.bind(history);
  const originalReplaceState = history.replaceState.bind(history);

  history.pushState = (...args: Parameters<History["pushState"]>) => {
    originalPushState(...args);
    onChange();
  };
  history.replaceState = (...args: Parameters<History["replaceState"]>) => {
    originalReplaceState(...args);
    onChange();
  };

  return () => {
    window.removeEventListener("popstate", onChange);
    history.pushState = originalPushState;
    history.replaceState = originalReplaceState;
  };
}

// No router in this app: /gallery is a backend-free A2UI verification page
// selected by pathname.
@customElement("app-router")
export class AppRouter extends LitElement {
  @state()
  private _pathname = window.location.pathname;

  #unsubscribe?: () => void;

  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.style.display = "contents";
    this.#unsubscribe = subscribeLocation(() => {
      this._pathname = window.location.pathname;
    });
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    this.#unsubscribe?.();
    this.#unsubscribe = undefined;
  }

  render() {
    const page =
      this._pathname === "/gallery"
        ? html`<gallery-page></gallery-page>`
        : html`<chat-app></chat-app>`;
    return html`${page}${
      import.meta.env.DEV ? html`<random-crash></random-crash>` : ""
    }`;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "app-router": AppRouter;
  }
}
