import { LitElement, html, nothing } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { OverlayState } from "../chat/chat-store.js";

@customElement("loading-overlay")
export class LoadingOverlay extends LitElement {
  @property({ attribute: false })
  overlay: OverlayState = { show: false, text: "", subtext: "" };

  /* Render into light DOM so global Tailwind utilities apply. */
  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.style.display = "contents";
  }

  render() {
    if (!this.overlay.show) return nothing;
    return html`
      <div
        class="fixed inset-0 z-9999 flex items-center justify-center bg-black/70 backdrop-blur"
      >
        <div class="rounded-2xl bg-white/95 px-12 py-10 text-center shadow-2xl">
          <div
            class="mx-auto mb-5 h-12 w-12 animate-spin rounded-full border-4 border-sky-200 border-t-sky-500"
          ></div>
          <div class="text-lg font-semibold text-sky-600">
            ${this.overlay.text}
          </div>
          <div class="mt-2 text-sm text-zinc-600">${this.overlay.subtext}</div>
        </div>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "loading-overlay": LoadingOverlay;
  }
}
