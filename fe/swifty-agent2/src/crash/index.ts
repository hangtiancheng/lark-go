/**
 * Lit port of the React <RandomCrash /> component.
 *
 * In React, RandomCrash throws during the render phase inside a
 * <ReactErrorBoundary>, which catches the error and reports it as
 * EventType.React. Lit has no error-boundary mechanism, so this component
 * throws asynchronously (via setTimeout) instead — the SDK's global
 * `window "error"` listener captures it as EventType.Error without
 * disturbing the UI.
 *
 * Mount once anywhere in the tree (DEV only) to seed probabilistic
 * render-crash-equivalent reports.
 */
import { LitElement } from "lit";
import { customElement } from "lit/decorators.js";

/** Interval between crash probability rolls. */
const ROLL_INTERVAL_MS = 20_000;

/** Probability that a single roll triggers a crash (4%). */
const CRASH_PROBABILITY = 0.04;

@customElement("random-crash")
export class RandomCrash extends LitElement {
  #timerId: ReturnType<typeof setInterval> | undefined;

  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.style.display = "none";
    this.#timerId = setInterval(() => {
      if (Math.random() < CRASH_PROBABILITY) {
        console.log("[error-seeder] firing: Lit render crash");
        setTimeout(() => {
          throw new Error("Seeded Lit render crash: probe component exploded");
        }, 0);
      }
    }, ROLL_INTERVAL_MS);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    if (this.#timerId !== undefined) {
      clearInterval(this.#timerId);
      this.#timerId = undefined;
    }
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "random-crash": RandomCrash;
  }
}
