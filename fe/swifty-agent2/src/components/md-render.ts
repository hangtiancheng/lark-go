import { LitElement, html, type PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { unsafeHTML } from "lit/directives/unsafe-html.js";
import { renderMarkdown } from "@a2ui/markdown-it";

// Markdown renderer built on @a2ui/markdown-it (markdown-it + DOMPurify),
// replacing the React app's Streamdown. Output is sanitized before being
// injected with unsafeHTML; styling comes from the global .md-content rules.
@customElement("md-render")
export class MdRender extends LitElement {
  @property()
  content = "";

  @property({ attribute: false })
  mdClass?: string;

  @state()
  private _html = "";

  #renderToken = 0;

  /* Render into light DOM so global Tailwind utilities apply. */
  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.style.display = "contents";
  }

  protected willUpdate(changedProperties: PropertyValues) {
    if (!changedProperties.has("content")) return;
    const token = ++this.#renderToken;
    void renderMarkdown(this.content).then((rendered) => {
      // Drop stale renders that resolve after a newer content update.
      if (token === this.#renderToken) this._html = rendered;
    });
  }

  render() {
    const classes =
      this.mdClass ??
      "max-w-none text-sm leading-relaxed wrap-break-word text-zinc-800";
    return html`<div class="md-content ${classes}">
      ${unsafeHTML(this._html)}
    </div>`;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "md-render": MdRender;
  }
}
