"use strict";
(() => {
 const panel=document.getElementById("chat"); if(!panel)return;
 const input=document.getElementById("chat-input"),log=document.getElementById("chat-log"),status=document.getElementById("chat-status"),history=document.getElementById("chat-history");
 let id="",source=null,messages=new Map(),cursor=0,removed=new Set(),drafts=[],draftIndex=0,selectedSession=0,opening=null;
 try{id=sessionStorage.getItem("driga-chat-conversation")||"";}catch(_){}
 const remember=()=>{try{sessionStorage.setItem("driga-chat-conversation",id);}catch(_){}};
 const csrf=document.querySelector('meta[name="chat-csrf"]').content;
 async function api(path,body){const res=await fetch("/chat/api/"+path,{method:body===undefined?"GET":"POST",headers:body===undefined?{}:{"Content-Type":"application/json","X-CSRF-Token":csrf},body:body===undefined?undefined:JSON.stringify(body),credentials:"same-origin"});if(!res.ok)throw new Error(await res.text());return res.json();}
 function note(text){status.textContent=text;}
 function scope(){const q=new URLSearchParams(location.search),ctx={page:location.pathname,machine:q.get("machine")||"local"};const row=document.querySelector('[data-worker].sel,[data-row].sel,[data-map-row].selected');const href=row&&row.getAttribute("href");let session=selectedSession||Number(row&&(row.dataset.session||row.dataset.mapRow))||Number(q.get("id"))||0;if(href&&/^\/agents\/\d+/.test(href))session=Number(href.match(/\d+/)[0]);if(/^\/agents\/\d+$/.test(location.pathname))session=Number(location.pathname.split("/").pop());if(session)ctx.session_id=session;const map=document.querySelector("[data-flow-map]");if(map?.dataset.selectedMachine)ctx.machine=map.dataset.selectedMachine;const project=q.get("project")||document.querySelector(".status")?.dataset.chatProject||row?.dataset.chatProject;if(project)ctx.project=project;for(const key of removed)delete ctx[key];return ctx;}
 function contextChips(){const node=document.getElementById("chat-context");node.replaceChildren(document.createTextNode("Context "));for(const [key,value]of Object.entries(scope())){const b=document.createElement("button");b.type="button";b.className="chat-chip";b.textContent=(key==="session_id"?"session":key)+": "+value+" ×";b.addEventListener("click",()=>{removed.add(key);contextChips();});node.append(b);}}
 function content(node,text){const pattern=/\[\[session:([1-9][0-9]*)\]\]/g;let end=0;for(const match of text.matchAll(pattern)){node.append(document.createTextNode(text.slice(end,match.index)));const chip=document.createElement("button");chip.type="button";chip.className="chat-chip";chip.textContent="session #"+match[1];chip.addEventListener("click",()=>{selectedSession=Number(match[1]);removed.delete("session_id");document.dispatchEvent(new CustomEvent("driga:select-session",{detail:{id:match[1]}}));contextChips();});node.append(chip);end=match.index+match[0].length;}node.append(document.createTextNode(text.slice(end)));}
 // All agent text enters the DOM through text nodes. Links stay plain text,
 // so even malformed Markdown cannot introduce URLs or HTML attributes.
 function inline(node,text,depth=0){
  if(depth>16){content(node,text);return;}
  const pattern=/(`+)([\s\S]*?)\1|\*\*([^\n]+?)\*\*|__([^\n]+?)__|\*([^*\n]+?)\*|_([^_\n]+?)_|\[([^\]\n]+)\]\(([^)\n]*)\)/g;
  let end=0;
  for(const m of text.matchAll(pattern)){
   // Underscores inside identifiers are literal, not Markdown emphasis.
   if((m[4]!==undefined||m[6]!==undefined)&&(/[\p{L}\p{N}]/u.test(text[m.index-1]||"")||/[\p{L}\p{N}]/u.test(text[m.index+m[0].length]||"")))continue;
   content(node,text.slice(end,m.index));
   if(m[1]){const code=document.createElement("code");code.textContent=m[2];node.append(code);}
   else if(m[7]!==undefined){content(node,m[7]+" ("+m[8]+")");}
   else{const child=document.createElement(m[3]!==undefined||m[4]!==undefined?"strong":"em");inline(child,m[3]??m[4]??m[5]??m[6],depth+1);node.append(child);}
   end=m.index+m[0].length;
  }
  content(node,text.slice(end));
 }
 function markdown(node,text){
  const lines=text.replace(/\r\n?/g,"\n").split("\n");
  const list=line=>line.match(/^\s*(?:([-+*])|([0-9]+)[.)])\s+(.*)$/);
  const fence=line=>line.match(/^\s*(`{3,}|~{3,})/);
  for(let i=0;i<lines.length;){
   if(!lines[i].trim()){i++;continue;}
   const start=fence(lines[i]);
   if(start){
    const body=[];i++;
    while(i<lines.length){const end=fence(lines[i]);if(end&&end[1][0]===start[1][0]&&end[1].length>=start[1].length){i++;break;}body.push(lines[i++]);}
    const pre=document.createElement("pre"),code=document.createElement("code");code.textContent=body.join("\n");pre.append(code);node.append(pre);continue;
   }
   const first=list(lines[i]);
   if(first){
    const ordered=!!first[2],ul=document.createElement(ordered?"ol":"ul");
    while(i<lines.length){const item=list(lines[i]);if(!item||!!item[2]!==ordered)break;const li=document.createElement("li");inline(li,item[3]);ul.append(li);i++;}
    node.append(ul);continue;
   }
   const body=[lines[i++]];
   while(i<lines.length&&lines[i].trim()&&!fence(lines[i])&&!list(lines[i]))body.push(lines[i++]);
   const p=document.createElement("p");inline(p,body.join("\n"));node.append(p);
  }
 }
 function render(){const bottom=log.scrollHeight-log.scrollTop-log.clientHeight<60;log.replaceChildren();let tools=null;for(const m of messages.values()){if(m.role==="tool"){if(!tools){const details=document.createElement("details"),summary=document.createElement("summary");details.className="chat-tools";summary.textContent="Tools used";tools=document.createElement("ul");details.append(summary,tools);log.append(details);}const item=document.createElement("li");item.textContent=m.text||"driga read tool";tools.append(item);continue;}tools=null;const node=document.createElement("article");node.className="chat-message "+m.role+(m.status==="error"?" error":"");const who=document.createElement("span");who.className="chat-who";who.textContent=(m.role==="user"?"You":"Claude")+(m.status==="queued"?" · queued":m.status==="streaming"?" · streaming":m.status==="stopped"||m.status==="interrupted"?" · "+m.status:"");node.append(who);(m.role==="assistant"?markdown:content)(node,m.text||((m.status==="streaming")?"…":"Message body not retained by capture policy."));log.append(node);}if(!messages.size){const empty=document.createElement("p");empty.className="chat-note";empty.textContent="Ask what needs you, why a session is stalled, or what this screen means.";log.append(empty);}if(bottom)log.scrollTop=log.scrollHeight;}
 function apply(m){messages.set(m.id,m);while(messages.size>256)messages.delete(messages.keys().next().value);}
 function connect(){if(source)source.close();source=new EventSource("/chat/api/events?id="+encodeURIComponent(id)+"&after="+cursor);source.addEventListener("reset",event=>{const s=JSON.parse(event.data);cursor=s.cursor;messages.clear();s.messages.forEach(apply);render();document.getElementById("chat-lifetime").hidden=!s.ephemeral;});source.addEventListener("change",event=>{const e=JSON.parse(event.data);if(e.seq<=cursor)return;cursor=e.seq;if(e.kind==="idle")note("Ready");if(e.kind==="delta"){const m=messages.get(e.message_id);if(m){apply({...m,text:(m.text||"")+e.text});render();note("Claude is answering");}}if(e.message){apply(e.message);render();if(e.message.status==="streaming")note("Claude is answering");}});source.addEventListener("status",event=>{const s=JSON.parse(event.data);note(s.active?"Claude is answering"+(s.queued?" · "+s.queued+" queued":""):"Ready");});source.onerror=()=>note("Reconnecting…");}
 async function loadHistory(){const rows=await api("history");history.replaceChildren();for(const row of rows){const option=document.createElement("option");option.value=row.id;option.textContent="Claude · "+new Date(row.updated_at).toLocaleString();history.append(option);}history.value=id;}
 async function fresh(){const row=await api("new",{});id=row.id;remember();cursor=0;messages.clear();connect();await loadHistory();}
 async function open(text){try{sessionStorage.setItem("driga-chat-open","1");}catch(_){}panel.hidden=false;document.body.classList.add("chat-open");contextChips();if(text!==undefined)input.value=text;input.focus();if(!source&&!opening){opening=(async()=>{if(id){const rows=await api("history");if(rows.some(row=>row.id===id)){connect();await loadHistory();return;}}await fresh();})().catch(e=>note(e.message)).finally(()=>opening=null);}await opening;}
 function close(){if(location.pathname==="/chat")return;panel.hidden=true;document.body.classList.remove("chat-open");try{sessionStorage.setItem("driga-chat-open","0");}catch(_){}}
 window.drigaChat={open,toggle:()=>panel.hidden?open():close()};
 document.getElementById("chat-close").addEventListener("click",close);document.getElementById("chat-new").addEventListener("click",()=>fresh().catch(e=>note(e.message)));
 history.addEventListener("change",()=>{id=history.value;remember();cursor=0;messages.clear();connect();});
 async function stop(){if(id)try{await api("stop",{id});note("Stopping…");}catch(e){note(e.message);}}
 document.getElementById("chat-stop").addEventListener("click",stop);
 document.getElementById("chat-form").addEventListener("submit",async event=>{event.preventDefault();const text=input.value;if(!text.trim())return;try{await open();if(!id)return;const before=cursor;await api("send",{id,text,context:scope()});drafts.push(text);drafts=drafts.slice(-100);draftIndex=drafts.length;if(input.value===text)input.value="";if(cursor===before)note("Queued");}catch(e){note(e.message);}});
 input.addEventListener("keydown",event=>{if(event.isComposing)return;if(event.key==="Enter"&&(event.metaKey||event.ctrlKey)){event.preventDefault();document.getElementById("chat-form").requestSubmit();}if(event.key==="Escape"){event.preventDefault();stop();}if(event.key==="ArrowUp"&&input.selectionStart===0&&drafts.length){event.preventDefault();draftIndex=Math.max(0,draftIndex-1);input.value=drafts[draftIndex];}});
 document.addEventListener("keydown",event=>{if(event.defaultPrevented||event.metaKey||event.ctrlKey||event.altKey||event.isComposing)return;if(event.key==="c"&&!event.target.closest("input,textarea,select,[contenteditable='true']")){event.preventDefault();window.drigaChat.toggle();}else if(event.key==="Escape"&&!panel.hidden){event.preventDefault();stop();}});
 document.addEventListener("click",event=>{if(event.target.closest("[data-chat-toggle]"))window.drigaChat.toggle();if(!panel.hidden)contextChips();});
 document.body.addEventListener("htmx:afterSwap",contextChips);
 document.addEventListener("driga:session-selected",event=>{selectedSession=Number(event.detail.id);contextChips();});
 if(location.pathname==="/chat"){document.body.classList.add("chat-page");open();}else{try{if(sessionStorage.getItem("driga-chat-open")==="1")open();}catch(_){}}
 window.addEventListener("pagehide",()=>{if(source)source.close();});
})();
