"use strict";
(() => {
 const duration=ms=>{const m=Math.max(0,Math.floor(ms/60000));return m>=60?Math.floor(m/60)+"h "+m%60+"m":m?m+"m":Math.max(0,Math.floor(ms/1000))+"s";};
 // State lives outside the morphed subtree. Identity, never a row index, owns
 // selection; new waits cannot take focus or change the selected session.
 const state={view:typeof location!=="undefined"&&["map","timeline","sessions"].includes(new URLSearchParams(location.search).get("view"))?new URLSearchParams(location.search).get("view"):"map",key:"",query:"",sort:"title",descending:false,needsExpanded:false,collapsed:new Set()};
 const nextIndex=(index,key,length)=>key==="Home"?0:key==="End"?length-1:Math.max(0,Math.min(length-1,index+(["ArrowUp","k"].includes(key)?-1:1)));
 globalThis.DrigaBoard=Object.freeze({duration,nextIndex});
 if(typeof document==="undefined")return;
 const armed=new Map(),inFlight=new Set();let scroll=[],focusId="",caret=null;
 const root=()=>document.querySelector("[data-flow-board]");
 const nodes=()=>Array.from(root()?.querySelectorAll('[data-flow-surface="'+state.view+'"] [data-board-key]')||[]).filter(n=>!n.hidden);
 const row=key=>root()?.querySelector('[data-board-key="'+CSS.escape(key)+'"]');
 function select(key){const r=row(key);if(!r)return;state.key=key;const host=root();host.dataset.selectedMachine=r.dataset.machine;
  for(const n of host.querySelectorAll("[data-board-key]")){const on=n.dataset.boardKey===key;n.classList.toggle("selected",on);if(n.getAttribute("role")==="treeitem")n.tabIndex=on?0:-1;n.setAttribute(n.getAttribute("role")==="button"?"aria-pressed":"aria-selected",String(on));}for(const e of host.querySelectorAll("[data-overlap-a]"))e.classList.toggle("on",e.dataset.overlapA===key||e.dataset.overlapB===key);
  for(const n of host.querySelectorAll("[data-detail]"))n.hidden=n.dataset.detail!==key;
  for(const n of host.querySelectorAll("[data-family-root]")){n.hidden=n.dataset.familyRoot!==r.dataset.root;n.querySelector("button")?.classList.toggle("selected",n.dataset.familyRow===key);}
  document.dispatchEvent(new CustomEvent("driga:session-selected",{detail:{id:Number(r.dataset.session)}}));
 }
 function show(view){state.view=view;const host=root();if(!host)return;for(const n of host.querySelectorAll("[data-flow-surface]"))n.hidden=n.dataset.flowSurface!==view;for(const b of host.querySelectorAll("[data-flow-view]")){const on=b.dataset.flowView===view;b.setAttribute("aria-selected",String(on));b.tabIndex=on?0:-1;}filter();select(state.key);}
 function filter(){const all=Array.from(root()?.querySelectorAll("[data-board-key]")||[]),timeline=all.filter(n=>n.getAttribute("role")==="treeitem");const parents=new Map(timeline.map(n=>[n.dataset.boardKey,n.dataset.parent]));for(const n of all){let parent=n.dataset.parent,folded=false;while(parent){if(state.collapsed.has(parent)){folded=true;break;}parent=parents.get(parent);}n.hidden=folded||!!state.query&&!(n.dataset.title+" "+n.dataset.project).toLocaleLowerCase().includes(state.query);if(timeline.some(c=>c.dataset.parent===n.dataset.boardKey))n.setAttribute("aria-expanded",String(!state.collapsed.has(n.dataset.boardKey)));}}
 function order(){const body=root()?.querySelector("tbody");if(!body)return;const rows=Array.from(body.children);rows.sort((a,b)=>{const x=a.dataset[state.sort]||"",y=b.dataset[state.sort]||"";const d=state.sort==="age"?Number(x)-Number(y):x.localeCompare(y);return(state.descending?-1:1)*d;});body.append(...rows);}
 function hydrate(){if(!root())return;const input=root().querySelector("[data-board-search]");input.value=state.query;
  if(!state.key||!row(state.key)){const q=new URLSearchParams(location.search);state.key=Array.from(root().querySelectorAll("[data-board-key]")).find(n=>n.dataset.session===(q.get("id")||location.pathname.match(/^\/flow\/(\d+)$/)?.[1])&&(!q.get("machine")||n.dataset.machine===q.get("machine")))?.dataset.boardKey||root().querySelector("[data-board-key]")?.dataset.boardKey||"";}
  show(state.view);for(const n of root().querySelectorAll("[data-need-overflow]"))n.hidden=!state.needsExpanded;order();tick();
 }
 function tick(){const host=root();if(!host)return;const at=Date.parse(host.dataset.observed);host.querySelector("[data-board-updated]").textContent="updated "+duration(Date.now()-at)+" ago";
  for(const n of host.querySelectorAll("[data-prompt]")){const id=n.dataset.prompt;if(!armed.has(id))armed.set(id,Date.now()+600);for(const b of n.querySelectorAll("button"))b.disabled=Date.now()<armed.get(id)||inFlight.has(id);}
 }
 async function post(path,body){const response=await fetch(path,{method:"POST",credentials:"same-origin",headers:{"Content-Type":"application/json","X-CSRF-Token":document.querySelector('meta[name="chat-csrf"]').content},body:JSON.stringify(body)});if(!response.ok)throw Error(await response.text());return response;}
 const note=text=>{const n=root()?.querySelector("[data-board-status]");if(n)n.textContent=text;};
 document.addEventListener("click",async event=>{const host=root();if(!host||!host.contains(event.target))return;const t=event.target;
  const view=t.closest("[data-flow-view]");if(view){show(view.dataset.flowView);return;}
  const sort=t.closest("[data-sort]");if(sort){state.descending=state.sort===sort.dataset.sort?!state.descending:false;state.sort=sort.dataset.sort;order();return;}
  const more=t.closest("[data-show-needs]");if(more){state.needsExpanded=!state.needsExpanded;for(const n of host.querySelectorAll("[data-need-overflow]"))n.hidden=!state.needsExpanded;return;}
  const fold=t.closest("[data-fold]");if(fold){show("timeline");if(fold.dataset.fold)select(fold.dataset.fold);nodes().find(n=>n.dataset.boardKey===state.key)?.focus();return;}
  const pick=t.closest("[data-board-key],[data-select]");if(pick){select(pick.dataset.boardKey||pick.dataset.select);const need=t.closest("[data-need-key]");if(need){for(const n of host.querySelectorAll(".fb-need"))n.classList.toggle("primary",n===need);}return;}
  const answer=t.closest("[data-answer]");if(answer){const id=answer.closest("[data-prompt]").dataset.prompt;if(answer.disabled||inFlight.has(id)||Date.now()<armed.get(id))return;inFlight.add(id);tick();try{await post("/chat/api/prompt-answer",{id,allow:answer.dataset.answer==="allow"});note("Prompt answered");if(window.htmx)htmx.trigger(document.getElementById("main"),"console:refresh");}catch(e){inFlight.delete(id);note(e.message);tick();}return;}
  const stop=t.closest("[data-stop]");if(stop){const r=row(stop.dataset.stop);if(!confirm("Stop session \u201c"+r.dataset.title+"\u201d? Driga checks it is still the same idle process first."))return;try{await post("/chat/api/flow-action",{action:"kill",session_id:Number(r.dataset.session),machine:r.dataset.machine,confirmed:true});note("Session stopped");}catch(e){note(e.message);}return;}const terminal=t.closest("[data-terminal]");if(terminal){const r=row(terminal.dataset.terminal);try{await post("/chat/api/flow-action",{action:"jump",session_id:Number(r.dataset.session),machine:r.dataset.machine});note("Opened terminal");}catch(e){note(e.message);}return;}
  const chat=t.closest("[data-ask-chat]");if(chat){select(chat.dataset.askChat);const r=row(state.key);window.drigaChat?.open("Help me with “"+r.dataset.title+"” in "+r.dataset.project+" (session #"+r.dataset.session+").");}
 });
 document.addEventListener("input",event=>{if(event.target.matches("[data-board-search]")){state.query=event.target.value.toLocaleLowerCase();filter();}});
 document.addEventListener("keydown",event=>{if(event.defaultPrevented||event.metaKey||event.ctrlKey||event.altKey||!root())return;if(event.target.closest("input,textarea,select,[contenteditable=true],#chat"))return;
  if(event.key==="/"){event.preventDefault();root().querySelector("[data-board-search]").focus();return;}
  if(["m","t","s"].includes(event.key)){event.preventDefault();show({m:"map",t:"timeline",s:"sessions"}[event.key]);return;}
  const tab=event.target.closest("[data-flow-view]");if(tab&&["ArrowLeft","ArrowRight"].includes(event.key)){event.preventDefault();const views=["map","timeline","sessions"];show(views[(views.indexOf(state.view)+(event.key==="ArrowLeft"?2:1))%3]);root().querySelector('[data-flow-view="'+state.view+'"]').focus();return;}
  const target=event.target.closest("[data-board-key]");if(["ArrowDown","ArrowUp","Home","End","j","k"].includes(event.key)){const rows=nodes();if(!rows.length)return;event.preventDefault();const index=Math.max(0,rows.findIndex(n=>n.dataset.boardKey===(target?.dataset.boardKey||state.key)));const next=rows[nextIndex(index,event.key,rows.length)];select(next.dataset.boardKey);next.focus();return;}
  if(target&&["ArrowLeft","ArrowRight"].includes(event.key)&&state.view==="timeline"){event.preventDefault();const rows=nodes(),child=rows.find(n=>n.dataset.parent===target.dataset.boardKey);if(event.key==="ArrowLeft"&&child){state.collapsed.add(target.dataset.boardKey);filter();return;}if(event.key==="ArrowRight"&&state.collapsed.has(target.dataset.boardKey)){state.collapsed.delete(target.dataset.boardKey);filter();return;}const next=event.key==="ArrowLeft"?rows.find(n=>n.dataset.boardKey===target.dataset.parent):child;if(next){select(next.dataset.boardKey);next.focus();}return;}
  if(["Enter"," "].includes(event.key)&&target){event.preventDefault();select(target.dataset.boardKey);if(state.view==="sessions"&&event.key==="Enter")window.location.assign("/agents/"+target.dataset.session+"?machine="+encodeURIComponent(target.dataset.machine));return;}
  const fold=event.target.closest("[data-fold]");if(fold&&["Enter"," "].includes(event.key)){event.preventDefault();fold.dispatchEvent(new MouseEvent("click",{bubbles:true}));}
 });
 document.addEventListener("htmx:beforeSwap",()=>{const active=document.activeElement;focusId=root()?.contains(active)?active.id:"";caret=active?.matches("input")?[active.selectionStart,active.selectionEnd]:null;scroll=Array.from(root()?.querySelectorAll(".fb-canvas,.fb-timeline,.fb-sessions,.fb-panel")||[]).map(n=>[n.className,n.scrollTop,n.scrollLeft]);});
 document.addEventListener("htmx:afterSwap",()=>{hydrate();for(const [cls,top,left]of scroll){const n=root()?.querySelector("."+cls);if(n){n.scrollTop=top;n.scrollLeft=left;}}if(focusId){const n=document.getElementById(focusId);if(n){n.focus({preventScroll:true});if(caret&&typeof n.setSelectionRange==="function"){try{n.setSelectionRange(...caret);}catch(_){}}}}});
 document.addEventListener("driga:select-session",e=>{const n=Array.from(root()?.querySelectorAll("[data-board-key]")||[]).find(n=>Number(n.dataset.session)===Number(e.detail.id));if(n)select(n.dataset.boardKey);});
 hydrate();setInterval(tick,1000);
})();
