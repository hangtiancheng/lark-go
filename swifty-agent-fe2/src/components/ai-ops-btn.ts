import { LitElement, html } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { styleMap } from "lit/directives/style-map.js";
import { Layers } from "lucide";
import { icon } from "./icons.js";

// Pointer movement below this many pixels counts as a click, not a drag.
const DRAG_THRESHOLD = 4;

interface DragState {
  pointerId: number;
  startX: number;
  startY: number;
  offsetX: number;
  offsetY: number;
  width: number;
  height: number;
  dragged: boolean;
}

@customElement("ai-ops-btn")
export class AIOpsBtn extends LitElement {
  @property({ attribute: false })
  onTrigger?: () => void;

  @property({ type: Boolean })
  disabled = false;

  // null = never dragged: keep the default centered spot in the chat header.
  @state()
  private _pos: { x: number; y: number } | null = null;

  #drag: DragState | null = null;
  #suppressClick = false;

  /* Render into light DOM so global Tailwind utilities apply. */
  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.style.display = "contents";
  }

  #handlePointerDown = (e: PointerEvent) => {
    if (e.pointerType === "mouse" && e.button !== 0) return;
    const target = e.currentTarget as HTMLButtonElement;
    const rect = target.getBoundingClientRect();
    this.#drag = {
      pointerId: e.pointerId,
      startX: e.clientX,
      startY: e.clientY,
      offsetX: e.clientX - rect.left,
      offsetY: e.clientY - rect.top,
      width: rect.width,
      height: rect.height,
      dragged: false,
    };
    target.setPointerCapture(e.pointerId);
  };

  #handlePointerMove = (e: PointerEvent) => {
    const drag = this.#drag;
    if (!drag || drag.pointerId !== e.pointerId) return;
    if (
      !drag.dragged &&
      Math.abs(e.clientX - drag.startX) < DRAG_THRESHOLD &&
      Math.abs(e.clientY - drag.startY) < DRAG_THRESHOLD
    ) {
      return;
    }
    drag.dragged = true;
    this._pos = {
      x: Math.min(
        Math.max(e.clientX - drag.offsetX, 0),
        window.innerWidth - drag.width,
      ),
      y: Math.min(
        Math.max(e.clientY - drag.offsetY, 0),
        window.innerHeight - drag.height,
      ),
    };
  };

  #handlePointerEnd = (e: PointerEvent) => {
    const drag = this.#drag;
    if (!drag || drag.pointerId !== e.pointerId) return;
    // The click event fires after pointerup; swallow it if this was a drag.
    this.#suppressClick = drag.dragged;
    this.#drag = null;
  };

  #handleClick = () => {
    if (this.#suppressClick) {
      this.#suppressClick = false;
      return;
    }
    if (this.disabled) return;
    this.onTrigger?.();
  };

  render() {
    const pos = this._pos;
    return html`
      <button
        @click=${this.#handleClick}
        @pointerdown=${this.#handlePointerDown}
        @pointermove=${this.#handlePointerMove}
        @pointerup=${this.#handlePointerEnd}
        @pointercancel=${this.#handlePointerEnd}
        aria-disabled=${this.disabled}
        style=${styleMap(pos ? { left: `${pos.x}px`, top: `${pos.y}px` } : {})}
        class="${pos ? "fixed" : "absolute top-4 left-1/2 -translate-x-1/2"} ${
          this.disabled ? "opacity-50" : "hover:bg-green-600"
        } z-10 flex cursor-grab touch-none items-center gap-2 rounded-full bg-green-500 px-4 py-2 text-sm font-medium text-white shadow-md transition select-none active:cursor-grabbing"
      >
        ${icon(Layers)}
        <span>AI Ops</span>
      </button>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "ai-ops-btn": AIOpsBtn;
  }
}
