import { LitElement, html, nothing, type PropertyValues } from "lit";
import { customElement, property, query } from "lit/decorators.js";
import { repeat } from "lit/directives/repeat.js";
import { LoaderCircle, Sparkles } from "lucide";
import { icon } from "./icons.js";
import type { ChatMessage } from "../chat/chat-store.js";
import "./md-render.js";
import "./a2ui-view.js";

@customElement("msg-list")
export class MsgList extends LitElement {
  @property({ attribute: false })
  messages: ChatMessage[] = [];

  @property({ type: Boolean })
  isStreaming = false;

  /** Receives serialized A2UI surface actions to auto-send as chat messages. */
  @property({ attribute: false })
  onAction?: (query: string) => void;

  @query("[data-scroller]")
  private _scroller?: HTMLDivElement;

  /* Render into light DOM so global Tailwind utilities apply. */
  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.style.display = "contents";
  }

  protected updated(changedProperties: PropertyValues) {
    if (changedProperties.has("messages") && this._scroller) {
      this._scroller.scrollTop = this._scroller.scrollHeight;
    }
  }

  render() {
    return html`
      <div data-scroller class="flex-1 overflow-y-auto px-6 py-4">
        ${repeat(
          this.messages,
          (_, i) => i,
          (m) => this.#renderMessage(m),
        )}
      </div>
    `;
  }

  #renderMessage(message: ChatMessage) {
    if (message.type === "user") {
      return html`
        <div class="mb-6 flex flex-col items-end">
          <div
            class="max-w-[70%] rounded-2xl rounded-br-sm bg-zinc-100 px-4 py-3 text-sm whitespace-pre-wrap text-zinc-800"
          >
            ${message.content}
          </div>
        </div>
      `;
    }
    return html`
      <div class="mb-6 flex items-start gap-3">
        <div
          class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-linear-to-br from-blue-500 to-green-500"
        >
          ${icon(Sparkles, "h-5 w-5 text-white")}
        </div>
        <div class="min-w-0 flex-1">
          ${
            message.detail && message.detail.length > 0
              ? html`
                  <details
                    class="mb-2 rounded-xl border border-sky-200 bg-sky-50 px-4 py-3 text-sm"
                  >
                    <summary class="cursor-pointer font-medium text-sky-600">
                      View details (${message.detail.length} steps)
                    </summary>
                    <div class="mt-2 flex flex-col gap-2">
                      ${message.detail.map(
                        (d, idx) => html`
                          <div
                            class="border-l-2 border-sky-400 bg-white p-2 text-xs text-zinc-700"
                          >
                            <strong class="text-sky-600"
                              >Step ${idx + 1}:</strong
                            >
                            <md-render
                              .content=${d}
                              .mdClass=${"max-w-none text-xs leading-relaxed wrap-break-word text-zinc-700"}
                            ></md-render>
                          </div>
                        `,
                      )}
                    </div>
                  </details>
                `
              : nothing
          }
          <div class="text-sm text-zinc-800">
            ${
              message.pending
                ? html`
                    <div class="flex items-center gap-2 py-1 text-zinc-400">
                      ${icon(LoaderCircle, "h-4 w-4 animate-spin")}
                      <span>Thinking...</span>
                    </div>
                  `
                : html`<md-render .content=${message.content}></md-render>`
            }
            ${
              message.a2ui && message.a2ui.length > 0
                ? html`<a2ui-view
                    .messages=${message.a2ui}
                    .onAction=${this.onAction}
                  ></a2ui-view>`
                : nothing
            }
          </div>
        </div>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "msg-list": MsgList;
  }
}
