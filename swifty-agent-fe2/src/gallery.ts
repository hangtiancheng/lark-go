import { LitElement, html, nothing } from "lit";
import { customElement, state } from "lit/decorators.js";
import { createGalleryMessages } from "./gallery-messages.js";
import "./components/a2ui-view.js";

// Backend-free A2UI verification page: renders every Lit basic-catalog
// component from a canned message set through the real catalog pipeline.
// Reached via the pathname branch in main.ts (no router in this app).
@customElement("gallery-page")
export class GalleryPage extends LitElement {
  @state()
  private _lastAction: string | null = null;

  #messages = createGalleryMessages();

  /* Render into light DOM so global Tailwind utilities apply. */
  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.className =
      "mx-auto flex min-h-dvh w-full max-w-3xl flex-col gap-4 px-6 py-8";
  }

  #handleAction = (query: string) => {
    console.log("[gallery] action:", query);
    this._lastAction = query;
  };

  render() {
    return html`
      <h1 class="text-2xl font-semibold tracking-tight">
        A2UI Catalog Gallery
      </h1>
      <p class="text-sm text-zinc-500">
        Renders all Lit basic-catalog components from a mock message set — no
        backend required. Triggered actions are logged below and in the console.
      </p>
      ${
        this._lastAction
          ? html`<pre
              class="overflow-x-auto rounded-lg bg-zinc-100 px-4 py-3 text-xs text-zinc-700"
            >
${this._lastAction}</pre>`
          : nothing
      }
      <a2ui-view
        .messages=${this.#messages}
        .onAction=${this.#handleAction}
      ></a2ui-view>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "gallery-page": GalleryPage;
  }
}
