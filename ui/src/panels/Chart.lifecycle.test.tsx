// @vitest-environment happy-dom
import { MantineProvider } from "@mantine/core";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Chart } from "./Chart";

const engine = vi.hoisted(() => ({ init: vi.fn() }));
vi.mock("./charts", async importOriginal => {
  const actual = await importOriginal<typeof import("./charts")>();
  return { ...actual, echarts: { ...actual.echarts, init: engine.init } };
});
let root: Root;
let container: HTMLDivElement;
let instances: { setOption: ReturnType<typeof vi.fn>; on: ReturnType<typeof vi.fn>; resize: ReturnType<typeof vi.fn>; dispose: ReturnType<typeof vi.fn> }[];
beforeEach(() => {
  vi.clearAllMocks();
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  instances = [];
  engine.init.mockImplementation(() => { const instance = { setOption: vi.fn(), on: vi.fn(), resize: vi.fn(), dispose: vi.fn() }; instances.push(instance); return instance; });
  container = document.createElement("div"); document.body.append(container); root = createRoot(container);
});
afterEach(async () => { await act(async () => root.unmount()); container.remove(); });

it("reapplies the option when small recreates a chart with a different renderer", async () => {
  const option = vi.fn(() => ({ animation: false, series: [{ type: "line", data: [1, 2, 3] }] }));
  const render = async (small: boolean) => { await act(async () => { root.render(<MantineProvider><Chart option={option} label="Tool calls" small={small} /></MantineProvider>); }); };
  await render(false);
  expect(engine.init.mock.calls[0][2]).toEqual({ renderer: "canvas" });
  expect(instances[0].setOption).toHaveBeenCalledTimes(1);
  await render(true);
  expect(instances[0].dispose).toHaveBeenCalledTimes(1);
  expect(engine.init.mock.calls[1][2]).toEqual({ renderer: "svg" });
  expect(instances[1].setOption).toHaveBeenCalledTimes(1);
  expect(instances[1].setOption).toHaveBeenCalledWith(option.mock.results[1].value, { notMerge: true });
  await render(false);
  expect(instances[1].dispose).toHaveBeenCalledTimes(1);
  expect(engine.init.mock.calls[2][2]).toEqual({ renderer: "canvas" });
  expect(instances[2].setOption).toHaveBeenCalledTimes(1);
  expect(option).toHaveBeenCalledTimes(3);
});
