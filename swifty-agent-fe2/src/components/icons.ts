import { createElement, type IconNode } from "lucide";

// Lit stand-in for lucide-react components: returns a styled inline SVG
// element that can be interpolated into lit-html templates.
export function icon(node: IconNode, classes = "h-4 w-4"): SVGElement {
  const el = createElement(node);
  el.setAttribute("class", classes);
  return el;
}
