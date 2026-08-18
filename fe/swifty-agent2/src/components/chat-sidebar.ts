import { LitElement, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import { repeat } from "lit/directives/repeat.js";
import { Plus, X } from "lucide";
import { icon } from "./icons.js";
import type { ChatHistory } from "../chat/chat-store.js";

@customElement("chat-sidebar")
export class ChatSidebar extends LitElement {
  @property({ attribute: false })
  histories: ChatHistory[] = [];

  @property()
  activeId = "";

  @property({ attribute: false })
  onNewChat?: () => void;

  @property({ attribute: false })
  onLoad?: (id: string) => void;

  @property({ attribute: false })
  onDelete?: (id: string) => void;

  /* Render into light DOM so global Tailwind utilities apply. */
  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.style.display = "contents";
  }

  render() {
    return html`
      <aside class="flex w-56 flex-col border-r border-zinc-200 bg-sky-50">
        <div class="border-b border-zinc-200 px-3 py-2.5">
          <h2 class="text-sm font-semibold text-zinc-800">Swifty Agent</h2>
        </div>
        <div class="flex flex-1 flex-col gap-1.5 p-2">
          <button
            @click=${() => this.onNewChat?.()}
            class="flex items-center gap-2 rounded-lg px-2.5 py-2 text-sm font-medium text-zinc-800 transition hover:bg-blue-100"
          >
            ${icon(Plus)}
            <span>New chat</span>
          </button>
          <div class="mt-2 flex-1 overflow-y-auto">
            <div
              class="px-2.5 py-1.5 text-xs font-semibold tracking-wide text-zinc-500 uppercase"
            >
              Recent
            </div>
            <div class="flex flex-col gap-0.5">
              ${repeat(
                this.histories,
                (h) => h.id,
                (h) => html`
                  <div
                    class="group ${
                      h.id === this.activeId ? "bg-blue-100" : ""
                    } flex items-center rounded-md px-2.5 py-1.5 transition hover:bg-blue-100"
                  >
                    <button
                      @click=${() => this.onLoad?.(h.id)}
                      class="flex-1 truncate text-left text-sm text-zinc-800"
                    >
                      ${h.title}
                    </button>
                    <button
                      @click=${() => this.onDelete?.(h.id)}
                      class="ml-1.5 text-zinc-400 opacity-0 transition group-hover:opacity-100 hover:text-red-500"
                      aria-label="Delete"
                    >
                      ${icon(X, "h-3.5 w-3.5")}
                    </button>
                  </div>
                `,
              )}
            </div>
          </div>
        </div>
      </aside>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "chat-sidebar": ChatSidebar;
  }
}
