/* Session map: geometry is a pure projection of schema-2 stored truth. */
(() => {
  "use strict";
  const minute=60000, stamp=x=>x?Date.parse(x):NaN;
  const valid=x=>Number.isFinite(x)&&x>-60000000000000;
  const glyph=s=>({working:"▶",waiting:"◆",stalled:"‖",unknown:"?",idle:"○",finished:"●",dead:"●"})[s]||"?";
  const stateName=s=>s==="waiting"?"needs you":s==="stalled"?"stalled?":s;
  const state=n=>n.mixer?.verdict||n.label||"unknown";
  function eventMark(adapter,kind) {
    return ({claude:{"otlp:tool_result":"tool","otlp:user_prompt":"prompt",Stop:"end"},copilot:{postToolUse:"tool",userPromptSubmitted:"prompt",agentStop:"end"}})[adapter]?.[kind]||"";
  }
  function quiet(n,from,now) {
    if((n.waits||[]).some(w=>!w.ended_at))return false;
    if((n.events||[]).some(e=>stamp(e.occurred_at)>=from&&stamp(e.occurred_at)<=now))return false;
    if((n.handoffs||[]).some(e=>stamp(e.occurred_at)>=from&&stamp(e.occurred_at)<=now))return false;
    const o=n.observed;
    return !o||!(o.log_bytes_per_min||[]).some((v,i)=>{
      const at=stamp(o.from_at)+i*minute;
      return at>=from&&at<=now&&(v>0||(o.cpu_ms_per_min||[])[i]>0);
    });
  }
  function geometry(data,width,expanded=false) {
    const now=stamp(data.observed_at),wanted=data.window_minutes||60;
    const left=width<620?142:216,right=130,plotWidth=Math.max(4,width-left-right);
    const minutes=Math.max(1,Math.min(wanted,Math.floor(plotWidth/4))),from=now-minutes*minute;
    const x=at=>Math.max(0,Math.min(plotWidth,(at-from)/(minutes*minute)*plotWidth));
    const inWindow=at=>at>=from&&at<=now;
    const span=(start,end)=>({from:x(start),to:Math.min(plotWidth,Math.max(x(start)+1,x(end)))});
    let hidden=0,earlierAsks=0;
    const children=[...(data.children||[])].sort((a,b)=>(stamp(a.started_at)||0)-(stamp(b.started_at)||0)||a.id-b.id);
    const nodes=data.lead?[data.lead,...children.filter(n=>{if(!expanded&&quiet(n,now-wanted*minute,now)){hidden++;return false;}return true;})]:[];
    const rows=nodes.map((node,index)=>{
      const start=stamp(node.started_at),retained=stamp(node.retained_from_at);
      const row={node,index,y:34+index*66,start:x(start),hatches:[],waits:[],tools:[],marks:[],activity:[]};
      if(!valid(retained)||retained>from){const a=valid(start)?Math.max(from,start):from,b=valid(retained)?retained:now;if(b>a)row.hatches.push(span(a,b));}
      for(const gap of data.gaps||[]) {
        const a=stamp(gap.started_at),b=stamp(gap.ended_at);
        const begin=valid(start)?Math.max(a,start):a;if(b>=from&&begin<=now&&b>begin)row.hatches.push(span(begin,b));
      }
      for(const w of node.waits||[]) {
        const a=stamp(w.started_at),b=w.ended_at?stamp(w.ended_at):now;
        if(a<from)earlierAsks++;
        if(b<from||b<a||a>now)continue;
        row.waits.push({...span(a,b),wait:w,open:!w.ended_at});
        row.marks.push({x:x(a),kind:"wait",at:a,description:[w.reason,w.detail].filter(Boolean).join(" · ")});
      }
      const bins=new Map();
      for(const e of node.events||[]) {
        const at=stamp(e.occurred_at);if(!inWindow(at))continue;
        const kind=eventMark(node.adapter,e.kind);
        if(kind==="tool") {
          const bin=Math.min(Math.floor(plotWidth/4)-1,Math.floor(x(at)/4));
          const old=bins.get(bin)||{x:bin*4,count:0,events:[]};old.count++;old.events.push(e);bins.set(bin,old);
        } else if(kind) row.marks.push({x:x(at),kind,at,description:[e.kind,e.summary].filter(Boolean).join(" · ")});
      }
      row.tools=Array.from(bins.values()).sort((a,b)=>a.x-b.x);
      if(node.adapter==="codex"||node.adapter==="gemini")for(const h of node.handoffs||[]) {
        const at=stamp(h.occurred_at);if(inWindow(at))row.marks.push({x:x(at),kind:"end",at,description:h.text||"Turn finished · handoff"});
      }
      const o=node.observed;
      if(o)for(let i=0;i<(o.log_bytes_per_min||[]).length;i++) {
        const at=stamp(o.from_at)+i*minute;
        if(inWindow(at)&&!row.hatches.some(h=>x(at)>=h.from&&x(at)<h.to))row.activity.push({x:x(at),log:o.log_bytes_per_min[i],cpu:(o.cpu_ms_per_min||[])[i]||0,at});
      }
      return row;
    });
    return {rows,quiet:hidden,now,from,minutes,left,right,plotWidth,width,height:Math.max(110,rows.length*66+72),earlierMinutes:wanted-minutes,earlierAsks};
  }
  globalThis.DrigaFlow=Object.freeze({geometry,eventMark,glyph});
  if(typeof document==="undefined")return;
  const ns="http://www.w3.org/2000/svg";
  function svg(tag,attrs={},label) {
    const el=document.createElementNS(ns,tag);
    for(const [key,v] of Object.entries(attrs))el.setAttribute(key,String(v));
    if(label!=null)el.textContent=label;
    return el;
  }
  function html(tag,cls,text) {const el=document.createElement(tag);if(cls)el.className=cls;if(text!=null)el.textContent=text;return el;}
  const age=(at,now=Date.now())=>{const t=stamp(at);if(!valid(t))return "unknown";const s=Math.max(0,Math.floor((now-t)/1000));return s<60?s+"s ago":s<3600?Math.floor(s/60)+"m ago":Math.floor(s/3600)+"h ago";};
  const clock=at=>new Date(at).toLocaleTimeString([],{hour:"2-digit",minute:"2-digit",hour12:false});
  const quote=s=>"'"+s.replaceAll("'","'\"'\"'")+"'";
  const command=j=>j?.command?(j.cwd?"cd -- "+quote(j.cwd)+" && ":"")+j.command:"";
  const trim=(s,n)=>s.length>n?s.slice(0,n-1)+"…":s;
  let root=null,data=null,selected=null,expanded=false,geo=null,inflight=false,pending=false,seen=null,workerIDs=null,requestID=0;
  const waitPulseUntil=new Map();
  const reduced=()=>matchMedia("(prefers-reduced-motion: reduce)").matches;
  function info(text){const el=root?.querySelector("[data-flow-status]");if(el)el.textContent=text;}
  function rowSelect(id,scroll=true) {
    selected=id;
    for(const el of root.querySelectorAll("[data-map-row]")){const on=el.dataset.mapRow===String(id);el.classList.toggle("selected",on);el.setAttribute("aria-selected",String(on));if(on&&scroll)el.scrollIntoView({block:"nearest"});}
    detail();
  }
  function detail() {
    if(!root||!data)return;
    const node=[data.lead,...(data.children||[])].find(n=>n?.id===selected),panel=root.querySelector("[data-flow-detail]");panel.replaceChildren();
    panel.append(html("h2","","Session detail · signal mixer"));
    if(!node){panel.append(html("p","d","Select a session."));return;}
    panel.append(html("h3","",node.title||node.adapter+" thread"),html("p","d",[node.adapter,node.project,node.branch,node.parent_id?"parent #"+node.parent_id:"lead"].filter(Boolean).join(" · ")));
    const st=state(node);panel.append(html("span","metro-pill "+st,glyph(st)+" "+stateName(st)+" · as of "+age(node.mixer?.observed_at)));
    const dl=html("dl");
    const add=(label,value)=>{dl.append(html("dt","",label),html("dd","",value));};
    add("running",node.elapsed_text||"—");
    if(["claude","copilot"].includes(node.adapter))add("tool calls",(node.events||[]).filter(e=>eventMark(node.adapter,e.kind)==="tool").length+" · retained window");
    if(node.input_tokens!=null)add("input tokens",String(node.input_tokens));if(node.output_tokens!=null)add("output tokens",String(node.output_tokens));
    if(node.cost_usd!=null)add("cost","$"+node.cost_usd.toFixed(4)+" · retained events");panel.append(dl);
    const why=html("div","metro-why");why.append(html("p","","Why "+stateName(st)+": "+(node.mixer?.evidence||"No stored explanation.")));
    for(const r of node.mixer?.rows||[])why.append(html("p","d",r.name+": "+r.reading));panel.append(why);
    if(!["claude","copilot"].includes(node.adapter))panel.append(html("p","d","no tool events for "+node.adapter),html("p","d","no prompt events for "+node.adapter));
    if(node.adapter==="gemini")panel.append(html("p","d","no wait source for gemini"));
    if(node.observed==null)panel.append(html("p","d","Activity not observed locally."));
    const cmd=command(node.jump);if(cmd){const b=html("button","","Copy resume command");b.type="button";b.addEventListener("click",()=>copy(cmd));panel.append(b,html("pre","metro-command",cmd));}
    panel.append(html("h2","","Recent events"));const log=html("ul","metro-log");
    for(const e of [...(node.events||[])].slice(-7).reverse()){const li=html("li");li.append(html("time","",clock(stamp(e.occurred_at))),html("span","",[e.kind,e.metadata?.tool,e.summary].filter(Boolean).join(" · ")));log.append(li);}
    if(!log.childElementCount)log.append(html("li","d","No retained events in window."));panel.append(log);
  }
  async function copy(cmd) {try {await navigator.clipboard.writeText(cmd);info("Resume command copied.");}catch{info("Clipboard unavailable. Select the resume command in the detail panel.");}}
  async function jump(item) {
    const node=[data.lead,...data.children].find(n=>n?.id===item.session_id&&n.machine_id===item.machine_id);
    if(node){if(!geo.rows.some(r=>r.node.id===node.id))expanded=true;selected=node.id;render();rowSelect(node.id);return;}
    const url=new URL(location.href);url.searchParams.set("id",String(item.session_id));url.searchParams.set("machine",item.machine_id);history.replaceState(null,"",url);selected=item.session_id;expanded=true;await fetchFlow();rowSelect(selected);
  }
  function attention() {
    const cards=root.querySelector("[data-flow-attention]");cards.replaceChildren();
    for(const item of data.attention?.waits||[]) {
      const card=html("article","metro-card");card.append(html("b","",item.title||item.adapter+" session"));
      const start=item.waiting_since_at||item.occurred_at;const when=html("span","waiting",age(start));when.dataset.waitSince=start;card.append(when,html("p","",[item.reason,item.tool,item.detail].filter(Boolean).join(" · ")));
      const actions=html("div","metro-actions"),j=html("button","","Jump to session"),c=html("button","","Copy resume command");j.type=c.type="button";j.addEventListener("click",()=>jump(item));c.addEventListener("click",()=>copy(command(item.jump)));c.disabled=!command(item.jump);actions.append(j,c);card.append(actions);cards.append(card);
    }
    if(!cards.childElementCount)cards.append(html("p","metro-calm","Nothing needs you · "+(data.attention?.working||0)+" working"));
  }
  function markTitle(el,text) {el.append(svg("title",{},text));el.addEventListener("mouseenter",()=>{const box=root.querySelector("[data-flow-hover]");box.textContent=text;box.hidden=false;});el.addEventListener("mouseleave",()=>{root.querySelector("[data-flow-hover]").hidden=true;});}
  function render(fresh=new Map(),newWorkers=new Set()) {
    if(!root||!data)return;
    const canvas=root.querySelector("[data-flow-svg]"),width=Math.max(360,canvas.parentElement.clientWidth);
    geo=geometry(data,width,expanded);if(!geo.rows.some(r=>r.node.id===selected))selected=geo.rows[0]?.node.id;
    canvas.setAttribute("viewBox",`0 0 ${width} ${geo.height}`);canvas.setAttribute("width",width);canvas.setAttribute("height",geo.height);canvas.replaceChildren();
    const defs=svg("defs");
    for(const [id,cls] of [["no-data","metro-no-data"],["wait-data","metro-wait-data"]]) {const pattern=svg("pattern",{id,width:6,height:6,patternUnits:"userSpaceOnUse"});pattern.append(svg("path",{d:"M-1 1L1 -1M0 6L6 0M5 7L7 5",class:cls}));defs.append(pattern);}canvas.append(defs);
    const X=x=>geo.left+x;
    for(let m=0;m<=geo.minutes;m+=geo.minutes<=15?5:15){const x=X(m/geo.minutes*geo.plotWidth);canvas.append(svg("line",{x1:x,x2:x,y1:14,y2:geo.height-30,class:"metro-grid"}),svg("text",{x,y:geo.height-10,class:"metro-axis"},clock(geo.from+m*minute)));}
    const byID=new Map(geo.rows.map(r=>[r.node.id,r]));
    for(const r of geo.rows){const p=byID.get(r.node.parent_id);if(!p||stamp(r.node.started_at)<geo.from)continue;
      const x=X(r.start),path=svg("path",{d:`M${x} ${p.y}V${r.y-8}Q${x} ${r.y} ${x+8} ${r.y}`,class:"metro-branch"+(newWorkers.has(r.node.id)&&!reduced()?" metro-spark":"")});markTitle(path,"Parent #"+r.node.parent_id+" · child started "+clock(stamp(r.node.started_at)));canvas.append(path);
    }
    for(const r of geo.rows) {
      const n=r.node,st=state(n),group=svg("g",{class:"metro-row"+(n.id===selected?" selected":""),"data-map-row":n.id,role:"option","aria-selected":n.id===selected});
      group.append(svg("rect",{x:0,y:r.y-24,width,height:66,class:"metro-row-bg"}));
      const label=svg("text",{x:12,y:r.y-3,class:"metro-title"},trim((n.kind==="lead"?"▸ ":"  ")+(n.title||n.adapter+" thread"),Math.floor((geo.left-22)/7.4)));markTitle(label,n.title||n.adapter);group.append(label,svg("text",{x:12,y:r.y+15,class:"metro-sub "+st},n.adapter+" · "+glyph(st)+" "+stateName(st)));
      const end=n.ended_at?Math.max(r.start,Math.min(geo.plotWidth,(stamp(n.ended_at)-geo.from)/(geo.minutes*minute)*geo.plotWidth)):geo.plotWidth;
      if(r.start<=end)group.append(svg("line",{x1:X(r.start),x2:X(end),y1:r.y,y2:r.y,class:"metro-baseline"}));
      const logPeak=Math.max(1,...r.activity.map(a=>a.log)),cpuPeak=Math.max(1,...r.activity.map(a=>a.cpu));
      for(const a of r.activity){if(a.log>0){const b=svg("line",{x1:X(a.x),x2:X(a.x),y1:r.y,y2:r.y-3-a.log/logPeak*15,class:"metro-log-band"});markTitle(b,clock(a.at)+" · "+a.log+" transcript bytes/min");group.append(b);}if(a.cpu>0){const b=svg("line",{x1:X(a.x),x2:X(a.x)+Math.max(2,geo.plotWidth/geo.minutes),y1:r.y+12,y2:r.y+12,class:"metro-cpu"});markTitle(b,clock(a.at)+" · "+a.cpu+" CPU ms/min (separate track)");group.append(b);}}
      for(const h of r.hatches){const hatch=svg("rect",{x:X(h.from),y:r.y-20,width:Math.max(0,h.to-h.from),height:39,fill:"url(#no-data)",class:"metro-hatch"});markTitle(hatch,"No retained data or collector gap");group.append(hatch);}
      for(const w of r.waits){const span=svg("rect",{x:X(w.from),y:r.y-5,width:Math.max(1,w.to-w.from),height:10,fill:"url(#wait-data)",class:"metro-wait"+(w.open&&Date.now()<(waitPulseUntil.get(w.wait.id)||0)?" open":"")});markTitle(span,"Waiting · "+(w.wait.reason||"input")+" · "+clock(stamp(w.wait.started_at))+"–"+(w.wait.ended_at?clock(stamp(w.wait.ended_at)):"open"));group.append(span);}
      for(const b of r.tools){const tick=svg("line",{x1:X(b.x),x2:X(b.x),y1:r.y-8,y2:r.y+8,class:"metro-tool"});markTitle(tick,b.count+" tool call"+(b.count===1?"":"s")+" · "+b.events.map(e=>e.metadata?.tool||e.kind).join(", "));group.append(tick);}
      for(const m of r.marks){const el=svg("text",{x:X(m.x),y:r.y+4,class:"metro-mark "+m.kind},({wait:"◆",prompt:"○",end:"●"})[m.kind]);markTitle(el,clock(m.at)+" · "+m.description);group.append(el);}
      const asof=svg("text",{x:X(geo.plotWidth)+12,y:r.y+16,class:"metro-asof","data-head-at":n.mixer?.observed_at||""},"as of "+age(n.mixer?.observed_at));
      group.append(svg("line",{x1:X(geo.plotWidth)-8,x2:X(geo.plotWidth)+7,y1:r.y-5,y2:r.y-5,class:"metro-head-pattern "+st}));
      group.append(svg("text",{x:X(geo.plotWidth)+12,y:r.y-1,class:"metro-head "+st+(fresh.has(n.id)&&!reduced()?" metro-glow":"")},glyph(st)+" "+stateName(st)),asof);
      if(fresh.has(n.id)&&!reduced())for(const event of fresh.get(n.id)){
        const at=stamp(event.occurred_at);if(at<geo.from||at>geo.now)continue;
        const ring=svg("circle",{cx:X((at-geo.from)/(geo.minutes*minute)*geo.plotWidth),cy:r.y,r:12,class:"metro-ripple"});group.append(ring);
      }
      group.addEventListener("click",()=>rowSelect(n.id,false));canvas.append(group);
    }
    canvas.append(svg("line",{x1:X(geo.plotWidth),x2:X(geo.plotWidth),y1:8,y2:geo.height-30,class:"metro-now"}),svg("text",{x:X(geo.plotWidth),y:12,class:"metro-axis"},"now"));
    const earlier=root.querySelector("[data-flow-earlier]");earlier.textContent=geo.earlierMinutes?`${geo.earlierMinutes}m earlier · ${geo.earlierAsks} asks`:"";earlier.hidden=!geo.earlierMinutes;
    const toggle=root.querySelector("[data-flow-quiet]");toggle.hidden=!geo.quiet&&!expanded;toggle.textContent=expanded?"Collapse quiet threads":"+"+geo.quiet+" quiet threads";
    attention();detail();
  }
  async function fetchFlow() {
    if(!root)return;if(inflight){pending=true;return;}inflight=true;
    const target=root,generation=requestID,href=location.href,url=new URL("/flow.json",location.origin),query=new URL(location.href).searchParams;
    for(const key of ["id","machine"])if(query.has(key))url.searchParams.set(key,query.get(key));
    url.searchParams.set("minutes",target.querySelector("[data-flow-zoom]").value);
    try {
      const response=await fetch(url,{credentials:"same-origin",cache:"no-store"});if(!response.ok)throw Error("Flow unavailable ("+response.status+")");
      const envelope=await response.json();if(generation!==requestID||target!==root||href!==location.href)return;
      if(envelope.schema!==2)throw Error("Unsupported flow schema");
      const next=envelope.data,fresh=new Map(),newWorkers=new Set(),nextSeen=new Set(),nextIDs=new Set();
      for(const n of [next.lead,...next.children].filter(Boolean)){
        nextIDs.add(n.id);for(const w of n.waits||[]){if(!w.ended_at&&!waitPulseUntil.has(w.id))waitPulseUntil.set(w.id,Date.now()+8000);}
        if(workerIDs&&!workerIDs.has(n.id))newWorkers.add(n.id);
        for(const e of n.events||[]){const key=n.id+"/"+e.occurred_at+"/"+e.kind+"/"+(e.summary||"");nextSeen.add(key);if(seen&&!seen.has(key)){if(!fresh.has(n.id))fresh.set(n.id,[]);fresh.get(n.id).push(e);}}
      }
      seen=nextSeen;workerIDs=nextIDs;data=next;info("");render(fresh,newWorkers);
    } catch(err){info(err.message);}finally{inflight=false;if(pending){pending=false;fetchFlow();}}
  }
  function mount() {
    const next=document.querySelector("[data-flow-map]");if(next===root)return;
    root=next;requestID++;data=null;seen=null;workerIDs=null;waitPulseUntil.clear();if(!root)return;
    expanded=new URL(location.href).searchParams.get("quiet")==="1";selected=null;
    root.querySelector("[data-flow-quiet]").addEventListener("click",()=>{expanded=!expanded;render();});
    root.querySelector("[data-flow-zoom]").addEventListener("change",fetchFlow);
    fetchFlow();
  }
  document.addEventListener("keydown",event=>{
    if(!root||!data||event.ctrlKey||event.altKey||event.metaKey||event.target.closest("input,textarea,select,[contenteditable='true']")||document.querySelector("dialog[open]"))return;
    const key=event.key;if(!["j","k","ArrowDown","ArrowUp","Enter","[","]","-","+","="].includes(key))return;
    event.preventDefault();event.stopImmediatePropagation();
    if(key==="["||key==="]"){const link=root.querySelector(key==="["?"[data-flow-prev]":"[data-flow-next]");if(link?.getAttribute("href"))location.assign(link.href);return;}
    if(["-","+","="].includes(key)){const zoom=root.querySelector("[data-flow-zoom]"),values=["15","60","240"],i=values.indexOf(zoom.value);zoom.value=values[Math.max(0,Math.min(2,i+(key==="-"?1:-1)))];fetchFlow();return;}
    if(key==="Enter"){const n=geo.rows.find(r=>r.node.id===selected)?.node;if(n)info(command(n.jump)||"No stored jump destination.");return;}
    const i=geo.rows.findIndex(r=>r.node.id===selected),delta=key==="j"||key==="ArrowDown"?1:-1;
    if(geo.rows.length)rowSelect(geo.rows[(i+delta+geo.rows.length)%geo.rows.length].node.id);
  },true);
  document.addEventListener("driga:flow-refresh",fetchFlow);document.addEventListener("htmx:afterSwap",mount);
  let resizeTimer;window.addEventListener("resize",()=>{clearTimeout(resizeTimer);resizeTimer=setTimeout(()=>{if(data)render();},100);});
  const clockTimer=setInterval(()=>{if(!root)return;for(const el of root.querySelectorAll("[data-head-at]"))el.textContent="as of "+age(el.dataset.headAt);for(const el of root.querySelectorAll("[data-wait-since]"))el.textContent=age(el.dataset.waitSince);},1000);
  window.addEventListener("pagehide",()=>clearInterval(clockTimer));mount();
})();
