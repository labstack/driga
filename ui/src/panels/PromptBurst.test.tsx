// @vitest-environment happy-dom
import { MantineProvider } from "@mantine/core";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect,it,vi } from "vitest";
import { PromptBurstItem } from "./PanelView";
import type { PromptBurst } from "./types";
const burst:PromptBurst={id:"b",session:"s",cwd:"/work/project",opened_at:"2026-10-29T00:00:00Z",members:Array.from({length:9},(_,i)=>({id:`p${i+1}`,input_hash:`hash${i+1}`,prompt:{id:`p${i+1}`,session:"s",cwd:"/work/project",tool:"Bash",input:{command:`echo item-${i+1}`,description:`exact-${i+1}`},created_at:"2026-10-29T00:00:00Z"}}))};
async function render(run:(el:HTMLDivElement,update:(b:PromptBurst)=>Promise<void>,decide:ReturnType<typeof vi.fn>,answer:ReturnType<typeof vi.fn>)=>Promise<void>){
 (globalThis as {IS_REACT_ACT_ENVIRONMENT?:boolean}).IS_REACT_ACT_ENVIRONMENT=true;vi.useFakeTimers();const el=document.createElement("div");document.body.append(el);const root=createRoot(el);const decide=vi.fn(async()=>undefined),answer=vi.fn(async()=>undefined);
 const update=async(b:PromptBurst)=>{await act(async()=>root.render(<MantineProvider><PromptBurstItem burst={b} answer={answer} decide={decide}/></MantineProvider>));};
 try{await update(burst);await run(el,update,decide,answer);}finally{await act(async()=>root.unmount());el.remove();vi.useRealTimers();}
}
it("lists every burst item and denies one independently",async()=>render(async(el,_update,_decide,answer)=>{
 for(let i=1;i<=9;i++){expect(el.textContent).toContain(`echo item-${i}`);expect(el.textContent).toContain(`exact-${i}`);}
 await act(async()=>{vi.advanceTimersByTime(600);});const denies=[...el.querySelectorAll("button")].filter(b=>b.textContent?.startsWith("Deny"));await act(async()=>denies[3].click());expect(answer).toHaveBeenCalledWith(burst.members[3].prompt,false);
}));
it("Allow all 9 submits exactly the displayed nine",async()=>render(async(el,_update,decide)=>{
 const allow=[...el.querySelectorAll("button")].find(b=>b.textContent==="Allow all 9")!;expect(allow.disabled).toBe(true);await act(async()=>{vi.advanceTimersByTime(600);});await act(async()=>allow.click());expect(decide).toHaveBeenCalledWith("b",["p1","p2","p3","p4","p5","p6","p7","p8","p9"],true);
}));
it("late tenth prompt is not approved",async()=>render(async(el,update,decide)=>{
 let resolve!:()=>void;decide.mockImplementation(()=>new Promise<void>(yes=>{resolve=yes}));await act(async()=>{vi.advanceTimersByTime(600);});await act(async()=>[...el.querySelectorAll("button")].find(b=>b.textContent==="Allow all 9")!.click());
 const tenth={...burst.members[0],id:"p10",input_hash:"hash10",prompt:{...burst.members[0].prompt,id:"p10",input:{command:"late tenth"}}};await update({...burst,members:[...burst.members,tenth]});await act(async()=>resolve());
 expect(decide.mock.calls[0][1]).not.toContain("p10");expect([...el.querySelectorAll("button")].find(b=>b.textContent==="Allow all 10")?.disabled).toBe(true);
}));
