import { LitElement, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { ChatMessage, Mode } from "../chat/chat-store.js";
import "./msg-list.js";
import "./chat-input.js";

@customElement("chat-container")
export class ChatContainer extends LitElement {
  @property({ attribute: false })
  messages: ChatMessage[] = [];

  @property({ type: Boolean })
  isStreaming = false;

  @property()
  mode: Mode = "quick";

  @property({ attribute: false })
  onModeChange?: (m: Mode) => void;

  @property({ attribute: false })
  onSend?: (text: string) => void;

  @property({ attribute: false })
  onUpload?: (file: File) => void;

  /* Render into light DOM so global Tailwind utilities apply. */
  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.style.display = "contents";
  }

  render() {
    const centered = this.messages.length === 0;
    return html`
      <div
        class="${
          centered ? "items-center justify-center" : ""
        } flex flex-1 flex-col overflow-hidden"
      >
        ${
          centered
            ? html`
                <div class="px-6 text-center text-sky-600">
                  <p class="text-2xl">
                    Hello! I am the Swifty Agent OnCall assistant
                  </p>
                  <p class="mt-3 text-sm text-zinc-500">
                    If this is your first time, upload a file from the docs
                    directory via the "..." menu before chatting, otherwise you
                    may get a search error.
                  </p>
                </div>
              `
            : html`
                <msg-list
                  .messages=${this.messages}
                  .isStreaming=${this.isStreaming}
                  .onAction=${this.onSend}
                ></msg-list>
              `
        }
        <div class="w-full px-6 pb-5">
          <chat-input
            .isStreaming=${this.isStreaming}
            .mode=${this.mode}
            .onModeChange=${this.onModeChange}
            .onSend=${this.onSend}
            .onUpload=${this.onUpload}
          ></chat-input>
        </div>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "chat-container": ChatContainer;
  }
}
