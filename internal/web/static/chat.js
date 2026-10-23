"use strict";
(() => {
 const panel=document.getElementById("chat"); if(!panel)return;
 const input=document.getElementById("chat-input"),log=document.getElementById("chat-log"),status=document.getElementById("chat-status"),history=document.getElementById("chat-history");
 let actions=new Map(),mode="confirm";
 let id="",source=null,messages=new Map(),cursor=0,removed=new Set(),drafts=[],draftIndex=0,selectedSession=0,opening=null,active=false,started=0,replyID="",tool="",timer=null,stickToBottom=true,rendered=new Map();
 try{id=sessionStorage.getItem("driga-chat-conversation")||"";}catch(_){}
 const remember=()=>{try{sessionStorage.setItem("driga-chat-conversation",id);}catch(_){}};
 const csrf=document.querySelector('meta[name="chat-csrf"]').content;
 async function api(path,body){const res=await fetch("/chat/api/"+path,{method:body===undefined?"GET":"POST",headers:body===undefined?{}:{"Content-Type":"application/json","X-CSRF-Token":csrf},body:body===undefined?undefined:JSON.stringify(body),credentials:"same-origin",keepalive:body!==undefined});if(!res.ok)throw new Error(await res.text());return res.json();}
 function note(text){status.textContent=text;}
 const stopButton=document.getElementById("chat-stop");
 stopButton.hidden=true;
 const motion=window.matchMedia("(prefers-reduced-motion: reduce)");
 const reduceMotion=event=>panel.classList.toggle("reduced-motion",event.matches);
 reduceMotion(motion);motion.addEventListener("change",reduceMotion);
 log.addEventListener("scroll",()=>{stickToBottom=log.scrollHeight-log.scrollTop-log.clientHeight<60;});
 function elapsed(){const seconds=Math.max(0,Math.floor((Date.now()-started)/1000));return seconds<60?seconds+"s":Math.floor(seconds/60)+"m "+String(seconds%60).padStart(2,"0")+"s";}
 function activity(){if([...actions.values()].some(a=>a.status==="pending"))return "action waiting";if(tool){const name=tool.replace(/^mcp__driga__/,"").replace(/^(list|get)_/,"").replace(/_/g," ");return "reading "+name+"…";}return messages.get(replyID)?.text?"writing…":"thinking…";}
 function progress(){
  if(!active)return;
  const text=activity()+" · "+elapsed();
  const dot=document.createElement("span");dot.className="chat-spinner";dot.setAttribute("aria-hidden","true");
  status.replaceChildren(dot,document.createTextNode(text));
  const placeholder=rendered.get(replyID||"pending");
  if(placeholder&&!messages.get(replyID)?.text)placeholder.body.textContent=text;
 }
 function setActive(value){
  if(value&&!active){started=Date.now();tool="";replyID="";}
  active=value;stopButton.hidden=!active;
  if(active){if(timer===null)timer=setInterval(progress,1000);progress();}
  else{if(timer!==null)clearInterval(timer);timer=null;tool="";note("Ready");}
 }
 function resetActivity(){actions.clear();setMode("confirm");setActive(false);replyID="";rendered.clear();stickToBottom=true;}

 let until=0,untilTimer=null;
 // Full auto lasts 30 minutes on the broker; show what is left.
 function showLeft(){const left=document.getElementById("chat-mode-left"),ms=until-Date.now();left.hidden=mode!=="full auto"||!until;if(!left.hidden)left.textContent=Math.max(0,Math.ceil(ms/60000))+"m left";}
 function setMode(value,end){until=end?Date.parse(end)||0:0;if(untilTimer!==null)clearInterval(untilTimer);untilTimer=null;mode=value||"confirm";if(mode==="full auto"&&until)untilTimer=setInterval(showLeft,15000);showLeft();for(const [key,buttonID] of [["confirm","chat-mode-confirm"],["auto","chat-mode-auto"],["full auto","chat-mode-full"]]){const button=document.getElementById(buttonID);button.setAttribute("aria-pressed",String(mode===key));button.classList.toggle("danger",mode==="full auto"&&key===mode);}}
 for(const [value,buttonID] of [["confirm","chat-mode-confirm"],["auto","chat-mode-auto"],["full auto","chat-mode-full"]])document.getElementById(buttonID).addEventListener("click",async()=>{if(!id)return;if(value==="full auto"&&!confirm("Full auto runs every action without asking, including shell commands, kill and cleanup, for 30 minutes. Text Chat reads can try to steer it. Turn it on?"))return;try{await api("mode",{id,mode:value});}catch(e){note(e.message);}});
 function actionCard(a){
  const node=document.createElement("article"),title=document.createElement("strong"),details=document.createElement("pre"),state=document.createElement("p");
  node.className="chat-message chat-action "+a.status;node.setAttribute("aria-label",a.title);
  title.textContent=a.title;details.textContent=a.directory+"\n"+JSON.stringify(a.input,null,2);state.textContent=a.status+(a.decided_by?" · "+a.decided_by:"")+(a.result?" · "+a.result:"");
  node.append(title,details,state);
  if(a.status==="pending")for(const allow of [true,false]){const button=document.createElement("button");button.type="button";button.textContent=allow?"Allow":"Deny";button.setAttribute("aria-label",button.textContent+" "+a.title);button.addEventListener("click",async()=>{button.disabled=true;try{await api("decide",{id,action:a.id,allow});}catch(e){button.disabled=false;note(e.message);}});node.append(button);}
  return node;
 }
 function scope(){const q=new URLSearchParams(location.search),ctx={page:location.pathname,machine:q.get("machine")||"local"};const row=document.querySelector('[data-worker].sel,[data-row].sel,[data-map-row].selected');const href=row&&row.getAttribute("href");let session=selectedSession||Number(row&&(row.dataset.session||row.dataset.mapRow))||Number(q.get("id"))||0;if(href&&/^\/agents\/\d+/.test(href))session=Number(href.match(/\d+/)[0]);if(/^\/agents\/\d+$/.test(location.pathname))session=Number(location.pathname.split("/").pop());if(session)ctx.session_id=session;const map=document.querySelector("[data-flow-map]");if(map?.dataset.selectedMachine)ctx.machine=map.dataset.selectedMachine;const project=q.get("project")||document.querySelector(".status")?.dataset.chatProject||row?.dataset.chatProject;if(project)ctx.project=project;for(const key of removed)delete ctx[key];return ctx;}
 function contextChips(){const node=document.getElementById("chat-context");node.replaceChildren(document.createTextNode("Context "));for(const [key,value]of Object.entries(scope())){const b=document.createElement("button");b.type="button";b.className="chat-chip";b.textContent=(key==="session_id"?"session":key)+": "+value+" ×";b.addEventListener("click",()=>{removed.add(key);contextChips();});node.append(b);}}
 function selectSession(session){selectedSession=Number(session);removed.delete("session_id");document.dispatchEvent(new CustomEvent("driga:select-session",{detail:{id:session}}));contextChips();}
 log.addEventListener("click",event=>{const chip=event.target.closest("[data-chat-session]");if(chip)selectSession(chip.dataset.chatSession);});
 function content(node,text){const pattern=/\[\[session:([1-9][0-9]*)\]\]/g;let end=0;for(const match of text.matchAll(pattern)){node.append(document.createTextNode(text.slice(end,match.index)));const chip=document.createElement("button");chip.type="button";chip.className="chat-chip";chip.textContent="session #"+match[1];chip.addEventListener("click",()=>{selectedSession=Number(match[1]);removed.delete("session_id");document.dispatchEvent(new CustomEvent("driga:select-session",{detail:{id:match[1]}}));contextChips();});node.append(chip);end=match.index+match[0].length;}node.append(document.createTextNode(text.slice(end)));}
 function render(){
  const scrollTop=log.scrollTop,nodes=[],live=new Set();let tools=null;
  const rows=[...messages.values()];
  if(active&&!rows.some(m=>m.role==="assistant"&&m.status==="streaming"))rows.push({id:"pending",role:"assistant",status:"streaming",text:""});
  for(const m of rows){
   if(m.role==="action"&&actions.has(m.id)){
    live.add(m.id);const a=actions.get(m.id),signature=JSON.stringify(a);let entry=rendered.get(m.id);
    if(!entry||entry.signature!==signature){entry={node:actionCard(a),signature};rendered.set(m.id,entry);}
    nodes.push(entry.node);tools=null;continue;
   }
   if(m.role==="tool"){
    if(!tools){live.add(m.id);let entry=rendered.get(m.id);
     if(!entry){const details=document.createElement("details"),summary=document.createElement("summary"),list=document.createElement("ul");details.className="chat-tools";summary.textContent="Tools used";details.append(summary,list);entry={node:details,list};rendered.set(m.id,entry);}
     tools=entry.list;tools.replaceChildren();nodes.push(entry.node);
    }
    const item=document.createElement("li");item.textContent=m.text||"driga read tool";tools.append(item);continue;
   }
   tools=null;live.add(m.id);
   let entry=rendered.get(m.id);
   if(!entry&&m.role==="assistant"&&rendered.has("pending")){entry=rendered.get("pending");rendered.delete("pending");}
   if(!entry){const node=document.createElement("article"),who=document.createElement("span"),body=document.createElement("div");who.className="chat-who";body.className="chat-body";node.append(who,body);entry={node,who,body};}
   rendered.set(m.id,entry);
   const {node,who,body}=entry,streaming=active&&m.role==="assistant"&&m.status==="streaming";
   node.className="chat-message "+m.role+(m.status==="error"?" error":"");
   who.replaceChildren();
   if(m.role==="assistant"){const mark=document.createElement("img");mark.className="chat-mark";mark.src="/static/brand/murmur.svg";mark.alt="";mark.width=14;mark.height=14;who.append(mark);}
   who.append(document.createTextNode((m.role==="user"?"You":"Driga")+(m.status==="queued"?" · queued":m.status==="stopped"||m.status==="interrupted"?" · "+m.status:"")));
   body.replaceChildren();
   // Only the same-origin server's safe goldmark renderer supplies this field.
   if(m.role==="assistant"&&m.status==="streaming")body.textContent=m.text||(activity()+" · "+elapsed());
   else if(m.role==="assistant"&&typeof m.html==="string"&&m.html)body.innerHTML=m.html;
   else content(body,m.text||(streaming?activity()+" · "+elapsed():"Message body not retained by capture policy."));
   // Keep the cursor outside server Markdown, including code blocks and lists.
   if(node.children.length>2)node.replaceChildren(who,body);
   if(streaming&&m.text){const cursor=document.createElement("span");cursor.className="chat-cursor";cursor.textContent="▍";cursor.setAttribute("aria-hidden","true");node.append(cursor);}
   nodes.push(node);
  }
  for(const key of rendered.keys())if(!live.has(key))rendered.delete(key);
  if(!rows.length){const empty=document.createElement("p");empty.className="chat-note";empty.textContent="Ask what needs you, why a session is stalled, or what this screen means.";nodes.push(empty);}
  for(let i=0;i<nodes.length;i++)if(log.children[i]!==nodes[i])log.insertBefore(nodes[i],log.children[i]||null);
  while(log.children.length>nodes.length)log.removeChild(log.lastChild);
  log.scrollTop=stickToBottom?log.scrollHeight:scrollTop;progress();
 }
 function apply(m){
  messages.set(m.id,m);while(messages.size>256)messages.delete(messages.keys().next().value);
  if(active&&m.role==="tool")tool=m.text;
  if(m.role==="assistant"&&m.status==="streaming"){
   if(!active)setActive(true);
   if(replyID!==m.id){replyID=m.id;tool="";started=Date.parse(m.created_at)||Date.now();}
  }
 }
 function connect(){
  if(source)source.close();
  source=new EventSource("/chat/api/events?id="+encodeURIComponent(id)+"&after="+cursor);
  source.addEventListener("reset",event=>{
   const s=JSON.parse(event.data);cursor=s.cursor;resetActivity();setMode(s.mode,s.until);for(const a of s.actions||[])actions.set(a.id,a);messages.clear();s.messages.forEach(apply);setActive(s.active===undefined?[...messages.values()].some(m=>m.status==="streaming"):!!s.active);render();document.getElementById("chat-lifetime").hidden=!s.ephemeral;
  });
  source.addEventListener("change",event=>{
   const e=JSON.parse(event.data);if(e.seq<=cursor)return;cursor=e.seq;
   if(e.kind==="mode")setMode(e.mode,e.until);
   if(e.action){actions.set(e.action.id,e.action);if(!e.message)apply({id:e.action.id,role:"action",status:e.action.status,text:e.action.title});render();}
   if(e.kind==="idle"){setActive(false);render();}
   if(e.kind==="delta"){
    const m=messages.get(e.message_id);if(m){apply({...m,text:(m.text||"")+e.text,html:""});tool="";render();}
   }
   if(e.message){apply(e.message);if(e.message.role==="assistant"&&e.message.id===replyID&&e.message.status!=="streaming")setActive(false);render();}
  });
  source.addEventListener("status",event=>{const s=JSON.parse(event.data),changed=active!==!!s.active;setActive(!!s.active);if(changed)render();});
  source.onerror=()=>{setActive(false);render();note(source.readyState===EventSource.CLOSED?"Disconnected · authenticate the new web URL, then reload":"Reconnecting…");};
 }
 async function loadHistory(){const rows=await api("history");history.replaceChildren();for(const row of rows){const option=document.createElement("option");option.value=row.id;option.textContent="Driga · "+new Date(row.updated_at).toLocaleString();history.append(option);}history.value=id;}
 async function fresh(){await leaveConversation();const row=await api("new",{});id=row.id;remember();cursor=0;resetActivity();messages.clear();connect();await loadHistory();}
 async function open(text){try{sessionStorage.setItem("driga-chat-open","1");}catch(_){}panel.hidden=false;document.body.classList.add("chat-open");contextChips();if(text!==undefined)input.value=text;input.focus();if(!source&&!opening){opening=(async()=>{if(id){const rows=await api("history");if(rows.some(row=>row.id===id)){connect();await loadHistory();return;}}await fresh();})().catch(e=>note(e.message)).finally(()=>opening=null);}await opening;}
 async function leaveConversation(){if(id&&mode==="full auto")await api("mode",{id,mode:"confirm"});}
 function close(){leaveConversation().catch(e=>note(e.message));panel.hidden=true;document.body.classList.remove("chat-open");try{sessionStorage.setItem("driga-chat-open","0");}catch(_){}}
 window.drigaChat={open,toggle:()=>panel.hidden?open():close()};
 document.getElementById("chat-close").addEventListener("click",close);document.getElementById("chat-new").addEventListener("click",()=>fresh().catch(e=>note(e.message)));
 history.addEventListener("change",async()=>{try{await leaveConversation();}catch(e){note(e.message);return;}id=history.value;remember();cursor=0;resetActivity();messages.clear();connect();});
 async function stop(){if(id)try{await api("stop",{id});setActive(false);render();note("Stopped");}catch(e){note(e.message);}}
 document.getElementById("chat-stop").addEventListener("click",stop);
 document.getElementById("chat-form").addEventListener("submit",async event=>{event.preventDefault();const text=input.value;if(!text.trim())return;try{await open();if(!id)return;const before=cursor;await api("send",{id,text,context:scope()});drafts.push(text);drafts=drafts.slice(-100);draftIndex=drafts.length;if(input.value===text)input.value="";if(cursor===before)note("Queued");}catch(e){note(e.message);}});
 input.addEventListener("keydown",event=>{if(event.isComposing)return;if(event.key==="Enter"&&(event.metaKey||event.ctrlKey)){event.preventDefault();document.getElementById("chat-form").requestSubmit();}if(event.key==="Escape"){event.preventDefault();if(input.value)input.blur();else close();}if(event.key==="ArrowUp"&&input.selectionStart===0&&drafts.length){event.preventDefault();draftIndex=Math.max(0,draftIndex-1);input.value=drafts[draftIndex];}});
 document.addEventListener("keydown",event=>{if(event.defaultPrevented||event.metaKey||event.ctrlKey||event.altKey||event.isComposing)return;if(event.key==="c"&&!event.target.closest("input,textarea,select,[contenteditable='true']")){event.preventDefault();window.drigaChat.toggle();}else if(event.key==="Escape"&&!panel.hidden){event.preventDefault();close();}});
 document.addEventListener("click",event=>{if(event.target.closest("[data-chat-toggle]"))window.drigaChat.toggle();if(!panel.hidden)contextChips();});
 document.body.addEventListener("htmx:afterSwap",contextChips);
 document.addEventListener("driga:session-selected",event=>{selectedSession=Number(event.detail.id);contextChips();});
 if(location.pathname==="/chat"){document.body.classList.add("chat-page");open();}else{try{if(sessionStorage.getItem("driga-chat-open")==="1")open();}catch(_){}}
 window.addEventListener("pagehide",()=>{leaveConversation().catch(()=>{});if(source)source.close();if(timer!==null)clearInterval(timer);});
})();
