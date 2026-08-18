import { SignalWatcher } from "@lit-labs/signals";
import { provide } from "@lit/context";
import { LitElement, html, nothing, type PropertyValues } from "lit";
import { customElement, property } from "lit/decorators.js";
import { repeat } from "lit/directives/repeat.js";
import { renderMarkdown } from "@a2ui/markdown-it";
import { basicCatalog, Context, type LitComponentApi } from "@a2ui/lit/v0_9";
import {
  type A2uiClientAction,
  type A2uiMessage,
  A2uiMessageSchema,
  MessageProcessor,
} from "@a2ui/web_core/v0_9";
import type { MarkdownRenderer } from "@a2ui/web_core";

export const UI_ACTION_PREFIX = "[UI_ACTION]";

export function buildQueryFromAction(action: A2uiClientAction): string {
  return `${UI_ACTION_PREFIX} ${action.name}\ncontext: ${JSON.stringify(action.context ?? {})}`;
}

// Lit port of the React A2uiView (@swifty.js/a2ui-shadcn): validates raw
// protocol messages, feeds them incrementally into a per-instance
// MessageProcessor, and renders the resulting surfaces. Uses the Lit basic
// catalog instead of the shadcn extension catalog.
@customElement("a2ui-view")
export class A2uiView extends SignalWatcher(LitElement) {
  @property({ attribute: false })
  messages: unknown[] = [];

  /** Receives serialized A2UI surface actions to auto-send as chat messages. */
  @property({ attribute: false })
  onAction?: (query: string) => void;

  /** When set, receives the raw action instead of the serialized query. */
  @property({ attribute: false })
  onRawAction?: (action: A2uiClientAction) => void;

  @provide({ context: Context.markdown })
  markdownRenderer: MarkdownRenderer = (value, options) =>
    renderMarkdown(value, options);

  #processedCount = 0;
  #processor = new MessageProcessor<LitComponentApi>(
    [basicCatalog],
    (action) => {
      if (this.onRawAction) {
        this.onRawAction(action);
      } else {
        this.onAction?.(buildQueryFromAction(action));
      }
    },
  );

  /* Render into light DOM so global Tailwind utilities apply. */
  createRenderRoot() {
    return this;
  }

  protected willUpdate(changedProperties: PropertyValues) {
    if (changedProperties.has("messages")) {
      this.#processPendingMessages();
    }
  }

  #processPendingMessages() {
    if (this.messages.length <= this.#processedCount) return;
    const pending = this.messages.slice(this.#processedCount);
    this.#processedCount = this.messages.length;

    const valid: A2uiMessage[] = [];
    for (const message of pending) {
      const parsed = A2uiMessageSchema.safeParse(message);
      if (parsed.success) {
        valid.push(parsed.data);
      } else {
        console.error("[a2ui] dropping invalid message:", parsed.error.issues);
      }
    }
    if (valid.length === 0) return;
    try {
      this.#processor.processMessages(valid);
    } catch (error) {
      console.error("[a2ui] failed to process messages:", error);
    }
  }

  render() {
    const surfaces = Array.from(this.#processor.model.surfacesMap.entries());
    if (surfaces.length === 0) return nothing;
    return html`<div class="mt-3 flex flex-col gap-3">
      ${repeat(
        surfaces,
        ([surfaceId]) => surfaceId,
        ([, surface]) =>
          html`<a2ui-surface .surface=${surface}></a2ui-surface>`,
      )}
    </div>`;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "a2ui-view": A2uiView;
  }
}
