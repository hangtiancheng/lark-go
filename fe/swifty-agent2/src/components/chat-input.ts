import { LitElement, html, nothing, type PropertyValues } from "lit";
import { customElement, property, query, state } from "lit/decorators.js";
import { ChevronDown, Ellipsis, Paperclip, Send } from "lucide";
import { icon } from "./icons.js";
import type { Mode } from "../chat/chat-store.js";

const MODES: Mode[] = ["quick", "stream"];

@customElement("chat-input")
export class ChatInput extends LitElement {
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

  @state()
  private _text = "";

  @state()
  private _showTools = false;

  @state()
  private _showMode = false;

  @query("textarea")
  private _textarea!: HTMLTextAreaElement;

  @query("input[type=file]")
  private _fileInput!: HTMLInputElement;

  @query("[data-input-container]")
  private _container!: HTMLDivElement;

  /* Render into light DOM so global Tailwind utilities apply. */
  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.style.display = "contents";
    document.addEventListener("mousedown", this.#handleClickOutside);
    document.addEventListener("keydown", this.#handleEscape);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener("mousedown", this.#handleClickOutside);
    document.removeEventListener("keydown", this.#handleEscape);
  }

  // Close dropdowns on outside click or Escape key.
  #handleClickOutside = (e: MouseEvent) => {
    if (!this._showTools && !this._showMode) return;
    if (this._container && !this._container.contains(e.target as Node)) {
      this._showTools = false;
      this._showMode = false;
    }
  };

  #handleEscape = (e: KeyboardEvent) => {
    if (!this._showTools && !this._showMode) return;
    if (e.key === "Escape") {
      this._showTools = false;
      this._showMode = false;
    }
  };

  protected updated(changedProperties: PropertyValues) {
    // Auto-resize textarea to fit content (up to ~10 lines).
    if (changedProperties.has("_text") && this._textarea) {
      this._textarea.style.height = "auto";
      this._textarea.style.height = `${this._textarea.scrollHeight}px`;
    }
  }

  #send() {
    const t = this._text.trim();
    if (!t || this.isStreaming) return;
    this.onSend?.(t);
    this._text = "";
  }

  render() {
    return html`
      <div
        data-input-container
        class="relative rounded-3xl border border-zinc-200 bg-white p-3 shadow-sm"
      >
        <textarea
          .value=${this._text}
          @input=${(e: Event) => {
            this._text = (e.target as HTMLTextAreaElement).value;
          }}
          @keydown=${(e: KeyboardEvent) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              this.#send();
            }
          }}
          ?disabled=${this.isStreaming}
          placeholder="Ask the Swifty Agent OnCall assistant"
          class="max-h-40 w-full resize-none bg-transparent text-base text-zinc-900 outline-none placeholder:text-zinc-400"
          rows="1"
        ></textarea>
        <div class="mt-2 flex items-center justify-between">
          <div class="relative">
            <button
              @click=${() => {
                this._showTools = !this._showTools;
              }}
              class="flex h-9 w-9 items-center justify-center rounded-full text-zinc-500 hover:bg-zinc-100"
              aria-label="Tools"
              aria-expanded=${this._showTools}
            >
              ${icon(Ellipsis, "h-5 w-5")}
            </button>
            ${
              this._showTools
                ? html`
                    <div
                      class="absolute bottom-full left-0 mb-2 rounded-xl border border-zinc-200 bg-white p-2 shadow-lg"
                    >
                      <button
                        @click=${() => {
                          this._fileInput?.click();
                          this._showTools = false;
                        }}
                        class="flex w-48 items-center gap-3 rounded-lg px-3 py-2 text-sm text-zinc-800 hover:bg-zinc-100"
                      >
                        ${icon(Paperclip, "h-5 w-5")}
                        <span>Upload file</span>
                      </button>
                    </div>
                  `
                : nothing
            }
          </div>
          <div class="flex items-center gap-2">
            <div class="relative">
              <button
                @click=${() => {
                  this._showMode = !this._showMode;
                }}
                class="flex items-center gap-1 text-sm text-zinc-500 hover:text-zinc-800"
                aria-expanded=${this._showMode}
                aria-label="Chat mode"
              >
                <span>${this.mode === "quick" ? "Quick" : "Stream"}</span>
                ${icon(ChevronDown)}
              </button>
              ${
                this._showMode
                  ? html`
                      <div
                        class="absolute right-0 bottom-full mb-2 rounded-xl border border-zinc-200 bg-white p-1 shadow-lg"
                      >
                        ${MODES.map(
                          (m) => html`
                            <button
                              @click=${() => {
                              this.onModeChange?.(m);
                              this._showMode = false;
                            }}
                              class="${
                              m === this.mode
                                ? "bg-sky-50 text-sky-600"
                                : "text-zinc-800 hover:bg-zinc-100"
                            } block w-40 rounded-lg px-3 py-2 text-left text-sm"
                            >
                              ${m === "quick" ? "Quick" : "Stream"}
                            </button>
                          `,
                        )}
                      </div>
                    `
                  : nothing
              }
            </div>
            <button
              @click=${() => this.#send()}
              ?disabled=${this.isStreaming || !this._text.trim()}
              class="flex h-9 w-9 items-center justify-center rounded-full bg-zinc-100 text-zinc-600 transition hover:bg-zinc-200 disabled:opacity-40 disabled:hover:bg-zinc-100"
              aria-label="Send"
            >
              ${icon(Send, "h-5 w-5")}
            </button>
          </div>
        </div>
        <input
          type="file"
          accept=".txt,.md,.markdown"
          class="hidden"
          @change=${(e: Event) => {
            const input = e.target as HTMLInputElement;
            const f = input.files?.[0];
            if (f) this.onUpload?.(f);
            input.value = "";
          }}
        />
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "chat-input": ChatInput;
  }
}
