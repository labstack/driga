// @vitest-environment happy-dom
import { MantineProvider } from "@mantine/core";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect, it, vi } from "vitest";
import { ActionCard } from "./Chat";
import type { ChatAction } from "./api";

const api = vi.hoisted(() => ({ decide: vi.fn(), decideSimilar:vi.fn(), revokeGrant:vi.fn() }));
vi.mock("./api", () => ({ chat: api }));
const command = { requested: { argv: ["pkill", "-f", "maestro.cli.AppKt mcp"], cwd: "/work/project", timeout_seconds: 120, network: false }, effective: [], cwd: "/work/project", class: "Process", boundary: "sandboxed: writes only to /work/project and /scratch, no network", summary: "Stop 65 processes owned by you matching `maestro.cli.AppKt mcp` (about 5 GB)", targets: Array.from({ length:65 }, (_,i) => ({ pid:100+i, owner:"UID 501", started_at:"2026-10-29T00:00:00Z", executable:"/usr/bin/java", argv:["java", "maestro.cli.AppKt mcp"], command_line:"maestro.cli.AppKt mcp", memory_bytes:77_000_000 })), count:65, memory_bytes:5_000_000_000, force:false };
export const action: ChatAction = { id:"one", tool:"mcp__driga__run_command", title:command.summary, input:command.requested, directory:command.cwd, status:"pending", command };
async function render(a:ChatAction, run:(el:HTMLDivElement, update:(a:ChatAction)=>Promise<void>)=>Promise<void>) {
 (globalThis as { IS_REACT_ACT_ENVIRONMENT?:boolean }).IS_REACT_ACT_ENVIRONMENT=true;
 const el=document.createElement("div");document.body.append(el); const root=createRoot(el);
 const update=async (a:ChatAction)=> {await act(async()=>root.render(<MantineProvider><ActionCard conversation="c" action={a}/></MantineProvider>));};
 try { await update(a); await run(el,update); } finally { await act(async()=>root.unmount());el.remove();vi.clearAllMocks(); }
}
it("shows the broker command and all 65 targets", async()=>render(action,async el=>{
 expect(el.textContent).toContain(command.boundary); expect(el.textContent).toContain("pkill");
 await act(async()=>el.querySelector<HTMLButtonElement>('[aria-label="Inspect 65 process targets"]')!.click());
 expect(el.textContent).toContain("164");expect(el.textContent).toContain("UID 501");expect(el.textContent).toContain("2026-10-29");
}));
it("shows sandbox failure without an executable Allow",async()=>render({...action,command:{...command,error:"sandbox unavailable"}},async el=>{
 expect(el.textContent).toContain("sandbox unavailable");expect(el.querySelector<HTMLButtonElement>('[aria-label^="Allow "]')?.disabled).toBe(true);
}));
it("keeps force on a second card",async()=>render({...action,command:{...command,force:true,summary:"Force stop 65 surviving processes"}},async el=>{expect(el.textContent).toContain("Force stop");}));
it("ignores stale decision responses",async()=>{
 let reject!:(e:Error)=>void;api.decide.mockReturnValue(new Promise((_,no)=>{reject=no;}));
 await render(action,async(el,update)=>{
 await act(async()=>el.querySelector<HTMLButtonElement>('[aria-label^="Allow "]')!.click());
 await update({...action,id:"two"});await act(async()=>reject(new Error("old failure")));
 expect(el.textContent).not.toContain("old failure");expect(el.querySelector<HTMLButtonElement>('[aria-label^="Allow "]')?.disabled).toBe(false);
 });
});

it("offers a bounded conversation grant and creates it by click",async()=>render({...action,grant_eligible:true,grant_pattern:'Read · ["cat","read.txt"] · in /work/project',grant_expires_at:"2026-10-29T00:30:00Z",command:{...command,class:"Read"}},async el=>{
 expect(el.textContent).toContain('Read · ["cat","read.txt"]');expect(el.textContent).toContain("2026-10-29T00:30:00Z");
 await act(async()=>[...el.querySelectorAll("button")].find(b=>b.textContent==="Allow similar for this conversation")!.click());expect(api.decideSimilar).toHaveBeenCalledWith("c","one");
}));
it("attributes and revokes an automatic grant decision",async()=>render({...action,status:"done",decided_by:"allowed by your grant",grant_id:"g"},async el=>{
 expect(el.textContent).toContain("allowed by your grant");await act(async()=>[...el.querySelectorAll("button")].find(b=>b.textContent==="Revoke grant")!.click());expect(api.revokeGrant).toHaveBeenCalledWith("g");
}));

it("shows every process result category",async()=>render({...action,status:"done",command:{...command,stopped:[command.targets[0]],vanished:[command.targets[1]],skipped:[{...command.targets[2],reason:"identity changed"}],survivors:[command.targets[3]]}},async el=>{for(const label of ["stopped","vanished","skipped","survivors"]){expect(el.textContent).toContain(label);}expect(el.textContent).toContain("identity changed");}));
