const SURFACE_ID = "gallery-surface";
const CATALOG_ID =
  "https://a2ui.org/specification/v0_9/catalogs/basic/catalog.json";

const heading = (id: string, text: string) => ({
  id,
  component: "Text",
  text,
  variant: "h3",
});
const text = (id: string, t: string, variant?: string) => ({
  id,
  component: "Text",
  text: t,
  ...(variant ? { variant } : {}),
});
const button = (id: string, child: string, actionName: string) => ({
  id,
  component: "Button",
  child,
  action: { event: { name: actionName } },
});

// One surface referencing every Lit basic-catalog component at least once
// (Text, Image, Icon, Video, AudioPlayer, Row, Column, List, Card, Tabs,
// Modal, Divider, Button, TextField, CheckBox, ChoicePicker, Slider,
// DateTimeInput).
export function createGalleryMessages(): unknown[] {
  const components = [
    {
      id: "root",
      component: "Column",
      children: [
        "sec-display-h",
        "g-text-variants",
        "g-icon-row",
        "g-image-card",
        "g-divider",
        "sec-structure-h",
        "g-row",
        "g-list",
        "g-tabs",
        "g-modal",
        "sec-forms-h",
        "g-textfield",
        "g-checkbox",
        "g-choice-single",
        "g-choice-multi",
        "g-slider",
        "g-date",
        "g-submit",
        "sec-media-h",
        "g-video",
        "g-audio",
      ],
    },

    heading("sec-display-h", "Display"),
    {
      id: "g-text-variants",
      component: "Column",
      children: ["g-text-h1", "g-text-body", "g-text-caption"],
    },
    text("g-text-h1", "Heading text", "h1"),
    text(
      "g-text-body",
      "Body text with **Markdown** support rendered through markdown-it.",
    ),
    text("g-text-caption", "Caption text", "caption"),
    {
      id: "g-icon-row",
      component: "Row",
      align: "center",
      children: ["g-icon-1", "g-icon-2", "g-icon-3", "g-icon-label"],
    },
    { id: "g-icon-1", component: "Icon", name: "star" },
    { id: "g-icon-2", component: "Icon", name: "favorite" },
    { id: "g-icon-3", component: "Icon", name: "search" },
    text("g-icon-label", "Icon: star / favorite / search"),
    { id: "g-image-card", component: "Card", child: "g-image" },
    {
      id: "g-image",
      component: "Image",
      url: "/hero.svg",
      description: "Swifty hero artwork",
      fit: "contain",
      variant: "mediumFeature",
    },
    { id: "g-divider", component: "Divider", axis: "horizontal" },

    heading("sec-structure-h", "Structure"),
    {
      id: "g-row",
      component: "Row",
      justify: "spaceBetween",
      align: "center",
      children: ["g-row-left", "g-row-right"],
    },
    text("g-row-left", "Row start"),
    text("g-row-right", "Row end"),
    {
      id: "g-list",
      component: "List",
      direction: "vertical",
      children: ["g-list-1", "g-list-2", "g-list-3"],
    },
    text("g-list-1", "List item one"),
    text("g-list-2", "List item two"),
    text("g-list-3", "List item three"),
    {
      id: "g-tabs",
      component: "Tabs",
      tabs: [
        { title: "Overview", child: "g-tab-1" },
        { title: "Details", child: "g-tab-2" },
      ],
    },
    text("g-tab-1", "A streaming-first protocol for agent-generated UI."),
    text("g-tab-2", "Rendered by the @a2ui/lit basic catalog."),
    {
      id: "g-modal",
      component: "Modal",
      trigger: "g-modal-trigger",
      content: "g-modal-body",
    },
    { id: "g-modal-trigger", component: "Card", child: "g-modal-trigger-t" },
    text("g-modal-trigger-t", "Open modal"),
    { id: "g-modal-body", component: "Column", children: ["g-modal-text"] },
    text("g-modal-text", "Modal body content."),

    heading("sec-forms-h", "Forms"),
    {
      id: "g-textfield",
      component: "TextField",
      label: "Party size",
      value: { path: "/gallery/party" },
      variant: "number",
    },
    {
      id: "g-checkbox",
      component: "CheckBox",
      label: "Email notifications",
      value: { path: "/gallery/notify" },
    },
    {
      id: "g-choice-single",
      component: "ChoicePicker",
      label: "Cuisine",
      variant: "mutuallyExclusive",
      options: [
        { label: "Chinese", value: "chinese" },
        { label: "Italian", value: "italian" },
        { label: "Japanese", value: "japanese" },
      ],
      value: { path: "/gallery/cuisine" },
    },
    {
      id: "g-choice-multi",
      component: "ChoicePicker",
      label: "Any extras?",
      variant: "multipleSelection",
      displayStyle: "chips",
      options: [
        { label: "Outdoor seating", value: "outdoor" },
        { label: "Vegetarian menu", value: "veg" },
      ],
      value: { path: "/gallery/extras" },
    },
    {
      id: "g-slider",
      component: "Slider",
      label: "Budget",
      min: 0,
      max: 100,
      value: { path: "/gallery/budget" },
    },
    {
      id: "g-date",
      component: "DateTimeInput",
      label: "Booking date",
      value: { path: "/gallery/date" },
      enableDate: true,
      enableTime: false,
    },
    button("g-submit", "g-submit-t", "submit_gallery"),
    text("g-submit-t", "Submit"),

    heading("sec-media-h", "Media"),
    { id: "g-video", component: "Video", url: "/sample.mp4" },
    {
      id: "g-audio",
      component: "AudioPlayer",
      url: "/sample.mp3",
      description: "Sample audio track",
    },
  ];

  return [
    {
      version: "v0.9",
      createSurface: { surfaceId: SURFACE_ID, catalogId: CATALOG_ID },
    },
    {
      version: "v0.9",
      updateDataModel: {
        surfaceId: SURFACE_ID,
        path: "/gallery",
        value: {
          party: "2",
          notify: true,
          cuisine: ["chinese"],
          extras: [],
          budget: 40,
          date: "2026-08-20",
        },
      },
    },
    {
      version: "v0.9",
      updateComponents: { surfaceId: SURFACE_ID, components },
    },
  ];
}
