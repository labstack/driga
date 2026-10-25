import "@mantine/notifications/styles.css";
import "@mantine/spotlight/styles.css";
import "../src/panels/panels.css";
import "../src/host/host.css";
import { Notifications } from "@mantine/notifications";
import { type Args, INTERNAL_DEFAULT_PROJECT_ANNOTATIONS, type Preview, type StoryContext } from "@storybook/react-vite";
import { UPDATE_STORY_ARGS } from "storybook/internal/core-events";
import { addons } from "storybook/preview-api";
import { configure, spyOn } from "storybook/test";
import { storyNow } from "../src/fixtures/fragments";
import { unexpected, type Service } from "../src/fixtures/service";
import { Provider } from "../src/theme/Provider";
import { assertTextLayout } from "./checks";
import { viewportOptions } from "./viewports";

// findBy* waits 1 s by default; shared runners need longer under load.
configure({ asyncUtilTimeout: 10_000 });

const renderedStories = new WeakSet<StoryContext>();

const preview: Preview = {
  renderToCanvas: async (renderContext, canvas) => {
    const render = INTERNAL_DEFAULT_PROJECT_ANNOTATIONS.renderToCanvas;
    if (!render) throw new Error("Storybook React renderer is unavailable");
    const context = renderContext.storyContext;
    // Portable stories force a remount on every call; an args update must
    // keep component state and focus, as a normal Storybook re-render does.
    const unmount = await render({ ...renderContext, forceRemount: context.parameters.__isPortableStory ? !renderedStories.has(context) : renderContext.forceRemount }, canvas);
    renderedStories.add(context);
    return () => { renderedStories.delete(context); return unmount?.(); };
  },
  // One Docs page per story file, showing every state for review.
  tags: ["autodocs"],
  parameters: {
    layout: "padded",
    a11y: { test: "error" },
    viewport: { options: viewportOptions },
    options: { storySort: { order: ["Pages", "Flows", "Components", "Embedded", "Theme"] } },
  },
  globalTypes: {
    colorScheme: { description: "Color scheme", toolbar: { title: "Color scheme", items: ["light", "dark"], dynamicTitle: true } },
  },
  initialGlobals: { colorScheme: "light", viewport: { value: "desktop", isRotated: false } },
  beforeEach: context => {
    // Time is the fixtures' own, so "12m ago" reads the same every run.
    const realNow = Date.now;
    Date.now = () => storyNow;
    // Storage is per story and in memory: no story sees another's settings.
    const store = new Map<string, string>();
    spyOn(Storage.prototype, "getItem").mockImplementation(key => store.get(key) ?? null);
    spyOn(Storage.prototype, "setItem").mockImplementation((key, value) => { store.set(key, String(value)); });
    spyOn(Storage.prototype, "removeItem").mockImplementation(key => { store.delete(key); });
    // The service is a story's fake, or nothing: any request a story does not
    // answer fails the story rather than reaching a real Driga.
    // A story whose fake keeps state across requests passes makeService, so
    // every run starts from a fresh one.
    const make = context.parameters.makeService as (() => Service) | undefined;
    const service = make ? make() : context.parameters.service as Service | undefined;
    spyOn(window, "fetch").mockImplementation((input, init) => (service ?? unexpected)(input, init));
    spyOn(window, "confirm").mockImplementation(() => true);
    // Portable stories have no manager args store; re-render updates in Vitest.
    const channel = addons.getChannel();
    const onUpdate = async ({ storyId, updatedArgs }: { storyId: string; updatedArgs: Args }) => {
      if (storyId !== context.id || !context.renderToCanvas) return;
      Object.assign(context.args, updatedArgs);
      await context.renderToCanvas();
    };
    if (context.parameters.__isPortableStory) channel.on(UPDATE_STORY_ARGS, onUpdate);
    return () => { Date.now = realNow; channel.off(UPDATE_STORY_ARGS, onUpdate); };
  },
  afterEach: async () => {
    await document.fonts.ready;
    assertTextLayout(document.body);
  },
  decorators: [
    (Story, { globals }) => <Provider scheme={globals.colorScheme === "dark" ? "dark" : "light"}>
      <Notifications position="bottom-center" />
      <Story />
    </Provider>,
  ],
};

export default preview;
