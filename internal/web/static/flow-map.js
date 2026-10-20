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
    const left=width<620?142:232,right=150,plotWidth=Math.max(4,width-left-right);
    const minutes=Math.max(1,Math.min(wanted,Math.floor(plotWidth/4))),from=now-minutes*minute;
    const x=at=>Math.max(0,Math.min(plotWidth,(at-from)/(minutes*minute)*plotWidth));
    const inWindow=at=>at>=from&&at<=now;
    const span=(start,end)=>({from:x(start),to:Math.min(plotWidth,Math.max(x(start)+1,x(end)))});
    let hidden=0,earlierAsks=0;
    const children=[...(data.children||[])].sort((a,b)=>(stamp(a.started_at)||0)-(stamp(b.started_at)||0)||a.id-b.id);
    const nodes=data.lead?[data.lead,...children.filter(n=>{if(!expanded&&quiet(n,now-wanted*minute,now)){hidden++;return false;}return true;})]:[];
    const rows=nodes.map((node,index)=>{
      const start=stamp(node.started_at),retained=stamp(node.retained_from_at),end=node.ended_at?Math.min(now,stamp(node.ended_at)):now;
      const row={node,index,y:62+index*60,start:x(valid(start)?start:from),rail:{from:x(valid(start)?start:from),to:x(end)},head:{x:plotWidth,radius:7,pattern:headPattern(state(node))},hatches:[],waits:[],tools:[],marks:[],activity:[]};
      if(!valid(retained)||retained>from){const a=valid(start)?Math.max(from,start):from,b=Math.min(end,valid(retained)?retained:now);if(b>a)row.hatches.push(span(a,b));}
      for(const gap of data.gaps||[]) {
        const a=stamp(gap.started_at),b=Math.min(end,stamp(gap.ended_at));
        const begin=valid(start)?Math.max(a,start):a;if(b>=from&&begin<=now&&b>begin)row.hatches.push(span(begin,b));
      }
      for(const w of node.waits||[]) {
        const a=stamp(w.started_at),b=w.ended_at?stamp(w.ended_at):now;
        if(a<from)earlierAsks++;
        if(b<from||b<a||a>now)continue;
        row.waits.push({...span(a,b),wait:w,open:!w.ended_at,label:"needs you · "+duration(b-a)+" · "+(w.ended_at?"answered":"open")});
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
        if(inWindow(at)&&at>=start&&at<=end&&!row.hatches.some(h=>x(at)>=h.from&&x(at)<h.to))row.activity.push({x:x(at),log:o.log_bytes_per_min[i],cpu:(o.cpu_ms_per_min||[])[i]||0,at});
      }
      return row;
    });
    return {rows,quiet:hidden,now,from,minutes,left,right,plotWidth,width,height:30+(rows.length+(hidden||expanded?1:0))*60+30,earlierMinutes:wanted-minutes,earlierAsks};
  }
  const duration=ms=>{const s=Math.max(0,Math.floor(ms/1000));return s<60?s+"s":s<3600?Math.floor(s/60)+"m":Math.floor(s/3600)+"h";};
  const waitStart=w=>valid(stamp(w.waiting_since_at))?stamp(w.waiting_since_at):stamp(w.occurred_at);
  const sortAttention=waits=>[...waits].sort((a,b)=>waitStart(a)-waitStart(b)||String(a.machine_id||"").localeCompare(String(b.machine_id||""))||String(a.id||"").localeCompare(String(b.id||"")));
  const waitReason=w=>w.reason==="permission"?"◆ Permission"+(w.tool?" · "+w.tool:"")+(w.detail?": "+w.detail:""):"◆ Asked for input · what it asked wasn't captured";
  const signalGlyph=r=>!r.strength?"–":r.supports?"✓":"✗";
  function headPattern(st){return st==="idle"?"ring":st==="stalled"?"dashed":!["working","waiting","finished","dead"].includes(st)?"question":"filled";}
  // Collapse only consecutive identical stored events, preserving the newest three.
  function recentEvents(events) {
    const sorted=[...events].sort((a,b)=>stamp(b.occurred_at)-stamp(a.occurred_at)),out=[];
    const key=e=>[e.kind,e.metadata?.tool,e.summary].join("/");
    for(let i=0;i<sorted.length;) {
      let end=i+1;while(end<sorted.length&&key(sorted[end])===key(sorted[i]))end++;
      out.push(...sorted.slice(i,Math.min(end,i+3)));
      if(end-i>3)out.push({kind:sorted[i].kind,more:end-i-3,from:sorted[end-1].occurred_at,to:sorted[i+3].occurred_at});
      i=end;
    }
    return out.slice(0,7);
  }
  globalThis.DrigaFlow=Object.freeze({geometry,eventMark,glyph,sortAttention,waitReason,signalGlyph,recentEvents});
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
  const cardFirstSeen=new Map(),waitFirstSeen=new Map();
  const reduced=()=>matchMedia("(prefers-reduced-motion: reduce)").matches;
  function info(text){const el=root?.querySelector("[data-flow-status]");if(el)el.textContent=text;}
  function rowSelect(id,scroll=true) {
    selected=id;
    for(const el of root.querySelectorAll("[data-map-row]")){const on=el.dataset.mapRow===String(id);el.classList.toggle("selected",on);el.setAttribute("aria-selected",String(on));if(on&&scroll)el.scrollIntoView({block:"nearest"});}
    root.dataset.selectedSession=String(id);
    const current=[data.lead,...(data.children||[])].find(n=>n?.id===id);root.dataset.selectedMachine=current?.machine_id||"";
    document.dispatchEvent(new CustomEvent("driga:session-selected",{detail:{id}}));
    detail();
  }
  function detail() {
    if(!root||!data)return;
    const node=[data.lead,...(data.children||[])].find(n=>n?.id===selected),panel=root.querySelector("[data-flow-detail]");panel.replaceChildren();
    panel.append(html("h2","","Session detail"));
    if(!node){panel.append(html("p","d","Select a session."));return;}
    panel.append(html("h3","",node.title||node.adapter+" thread"));
    const chips=html("div","metro-chips");
    if(node.kind==="lead"||node.id===data.lead?.id)chips.append(html("span","metro-chip lead","LEAD"));
    for(const text of [node.adapter,node.project,node.branch,jumpTarget(node.jump)])if(text)chips.append(html("span","metro-chip",text));panel.append(chips);
    const st=state(node),pill=html("span","metro-pill "+st,glyph(st)+" "+stateName(st));pill.append(html("small",""," · verdict as of "+age(node.mixer?.observed_at)));panel.append(pill);
    const cmd=command(node.jump),actions=html("div","metro-actions"),j=html("button","primary","Jump to session"),c=html("button","ghost","Copy resume");
    j.type=c.type="button";c.disabled=!cmd;j.addEventListener("click",()=>jump({session_id:node.id,machine_id:node.machine_id}));c.addEventListener("click",()=>copy(cmd));actions.append(j,c);panel.append(actions);
    const dl=html("dl"),add=(label,value,note)=>{const dd=html("dd","",value);if(note)dd.append(html("i",""," · "+note));dl.append(html("dt","",label),dd);};
    add("running",node.elapsed_text||"—");
    const tools=(node.events||[]).filter(e=>eventMark(node.adapter,e.kind)==="tool").length;
    const available=tools>0||(node.mixer?.rows||[]).some(r=>r.name===(node.adapter==="copilot"?"hooks":"telemetry")&&r.strength>0);
    add("tool calls",available&&["claude","copilot"].includes(node.adapter)?String(tools):"—",available&&["claude","copilot"].includes(node.adapter)?"retained window":"telemetry unavailable");
    if(valid(stamp(node.observed?.last_growth_at))){const bytes=node.observed.last_growth_bytes;add("last growth",bytes>=1024?(bytes/1024).toFixed(1)+" KB":bytes+" B",age(node.observed.last_growth_at));}
    if(node.input_tokens!=null)add("input tokens",String(node.input_tokens));if(node.output_tokens!=null)add("output tokens",String(node.output_tokens));
    if(node.cost_usd!=null)add("cost","$"+node.cost_usd.toFixed(4),"retained events");
    if(node.input_tokens==null&&node.output_tokens==null&&node.cost_usd==null)add("tokens · cost","—","no retained OTLP");panel.append(dl);
    const why=html("div","metro-why"),heading=html("h4","","Why "+stateName(st));heading.append(html("small","",(node.mixer?.agreement||0)+" of 4 signals agree"));why.append(heading);
    const signals=html("ul","metro-signals");
    for(const r of node.mixer?.rows||[]){const li=html("li"),g=signalGlyph(r);li.append(html("span",g==="✓"?"ok":g==="✗"?"no":"na",g),html("span","d",r.name),html("span","",r.reading||"unavailable"));signals.append(li);}why.append(signals);
    why.append(html("p","metro-evidence",node.mixer?.evidence||"No stored explanation."));panel.append(why);
    if(!["claude","copilot"].includes(node.adapter))panel.append(html("p","d","no tool events for "+node.adapter),html("p","d","no prompt events for "+node.adapter));
    if(node.adapter==="gemini")panel.append(html("p","d","no wait source for gemini"));
    if(node.observed==null)panel.append(html("p","d","Activity not observed locally."));
    if(cmd)panel.append(html("pre","metro-command",cmd));
    panel.append(html("h2","","Recent events"));const log=html("ul","metro-log");
    for(const e of recentEvents(node.events||[])){const li=html("li");li.append(html("time","",e.more?"":clock(stamp(e.occurred_at))),html("span",e.more?"d":"",e.more?"+"+e.more+" more "+e.kind+" · "+clock(stamp(e.from))+" – "+clock(stamp(e.to)):[e.kind,e.metadata?.tool,e.summary].filter(Boolean).join(" · ")));log.append(li);}
    if(!log.childElementCount)log.append(html("li","d","No retained events in window."));panel.append(log);
  }
  const jumpTarget=j=>j?.target?[j.kind,j.target].join(" "):j?.kind||"";
  async function copy(cmd) {try {await navigator.clipboard.writeText(cmd);info("Resume command copied.");}catch{info("Clipboard unavailable. Select the resume command in the detail panel.");}}
  async function jump(item) {
    const node=[data.lead,...data.children].find(n=>n?.id===item.session_id&&n.machine_id===item.machine_id);
    if(node){if(!geo.rows.some(r=>r.node.id===node.id))expanded=true;selected=node.id;render();rowSelect(node.id);return;}
    const url=new URL(location.href);url.searchParams.set("id",String(item.session_id));url.searchParams.set("machine",item.machine_id);history.replaceState(null,"",url);selected=item.session_id;expanded=true;await fetchFlow();rowSelect(selected);
  }
  function attention() {
    const cards=root.querySelector("[data-flow-attention]");cards.replaceChildren();
    const waits=sortAttention(data.attention?.waits||[]),now=Date.now();
    root.querySelector("[data-flow-count]").textContent=String(waits.length);
    root.querySelector("[data-flow-needs-note]").textContent="· machine-wide"+(waits.length?" · oldest "+duration(now-waitStart(waits[0]))+" · longest wait first":"");
    const live=new Set();
    for(const item of waits) {
      const key=(item.machine_id||"")+"/"+(item.id||item.session_id);live.add(key);
      if(!cardFirstSeen.has(key))cardFirstSeen.set(key,now);
      const elapsed=now-cardFirstSeen.get(key),card=html("article","metro-card"+(elapsed<7200&&!reduced()?" fresh":""));
      if(card.style)card.style.animationDelay=-elapsed+"ms";
      card.append(html("b","metro-card-title",item.title||item.adapter+" session"));
      const start=item.waiting_since_at||item.occurred_at,when=html("span","metro-age waiting"),number=html("span","",duration(now-stamp(start)));number.dataset.waitSince=start;
      when.append(number,html("small","","since "+clock(stamp(start))));card.append(when,html("span","metro-card-sub",[item.adapter,item.project,item.branch,jumpTarget(item.jump)].filter(Boolean).join(" · ")),html("p","metro-reason",waitReason(item)));
      const actions=html("div","metro-actions"),j=html("button","primary","Jump to session"),c=html("button","ghost","Copy resume");j.type=c.type="button";j.addEventListener("click",()=>jump(item));c.addEventListener("click",()=>copy(command(item.jump)));c.disabled=!command(item.jump);actions.append(j,c);card.append(actions);cards.append(card);
    }
    for(const key of cardFirstSeen.keys())if(!live.has(key))cardFirstSeen.delete(key);
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
    root.querySelector("[data-flow-map-summary]").textContent=" · "+clock(geo.from)+" – now · "+geo.minutes+" min · "+geo.rows.length+" of "+(data.lead?1+(data.children||[]).length:0)+" sessions";
    root.querySelector("[data-flow-family-summary]").textContent=data.lead?[data.lead.adapter+" lead",(data.children||[]).length+" workers",valid(stamp(data.lead.started_at))?"family since "+clock(stamp(data.lead.started_at)):""].filter(Boolean).join(" · "):"";
    const X=x=>geo.left+x;
    for(let m=0;m<=geo.minutes;m+=geo.minutes<=15?5:15){const x=X(m/geo.minutes*geo.plotWidth);canvas.append(svg("line",{x1:x,x2:x,y1:14,y2:geo.height-30,class:"metro-grid"}),svg("text",{x,y:geo.height-10,class:"metro-axis"},clock(geo.from+m*minute)));}
    const byID=new Map(geo.rows.map(r=>[r.node.id,r])),liveWaits=new Set();
    for(const r of geo.rows){const p=byID.get(r.node.parent_id);if(!p||stamp(r.node.started_at)<geo.from)continue;
      const x=X(r.start),path=svg("path",{d:`M${x} ${p.y}V${r.y-8}Q${x} ${r.y} ${x+8} ${r.y}`,class:"metro-branch"+(newWorkers.has(r.node.id)&&!reduced()?" metro-spark":"")});markTitle(path,"Parent #"+r.node.parent_id+" · child started "+clock(stamp(r.node.started_at)));canvas.append(path);
    }
    for(const r of geo.rows) {
      const n=r.node,st=state(n),group=svg("g",{class:"metro-row"+(n.id===selected?" selected":""),"data-map-row":n.id,role:"option","aria-selected":n.id===selected});
      group.append(svg("rect",{x:0,y:r.y-28,width,height:60,class:"metro-row-bg"}));
      const label=svg("text",{x:14,y:r.y-5,class:"metro-title"},trim(n.title||n.adapter+" thread",Math.floor((geo.left-22)/7.4)));markTitle(label,n.title||n.adapter);group.append(label);
      const lead=n.kind==="lead"||n.id===data.lead?.id,subText=[n.adapter,n.project,n.branch].filter(Boolean).join(" · ");
      if(lead)group.append(svg("rect",{x:14,y:r.y+5,width:34,height:15,rx:3,class:"metro-lead-chip"}),svg("text",{x:18,y:r.y+16,class:"metro-lead-label"},"LEAD"));
      const sub=svg("text",{x:lead?54:14,y:r.y+16,class:"metro-sub"},trim(subText,Math.floor((geo.left-(lead?64:24))/6.6)));markTitle(sub,subText);group.append(sub);
      group.append(svg("line",{x1:X(r.rail.from),x2:X(r.rail.to),y1:r.y,y2:r.y,class:"metro-rail"}));
      const logPeak=Math.max(1,...r.activity.map(a=>a.log)),cpuPeak=Math.max(1,...r.activity.map(a=>a.cpu)),barWidth=geo.plotWidth/geo.minutes;
      for(const a of r.activity){const w=Math.max(0,Math.min(barWidth,r.rail.to-a.x));if(a.log>0){const h=3+a.log/logPeak*18,b=svg("rect",{x:X(a.x)+.5,y:r.y-2-h,width:Math.max(0,w-1),height:h,rx:1,class:"metro-log-band"});markTitle(b,clock(a.at)+" · "+a.log+" transcript bytes/min");group.append(b);}if(a.cpu>0){const h=2+a.cpu/cpuPeak*3,b=svg("rect",{x:X(a.x)+.5,y:r.y+5,width:Math.max(0,w-1),height:h,class:"metro-cpu"});markTitle(b,clock(a.at)+" · "+a.cpu+" CPU ms/min (separate track)");group.append(b);}}
      for(const h of r.hatches){const hatch=svg("rect",{x:X(h.from),y:r.y-11,width:Math.max(0,h.to-h.from),height:22,fill:"url(#no-data)",class:"metro-hatch"});markTitle(hatch,"No retained data or collector gap");group.append(hatch);if(h.to-h.from>62)group.append(svg("text",{x:X(h.from)+8,y:r.y+4,class:"metro-nodata-label"},"no data"));}
      for(const w of r.waits){const key=n.id+"/"+w.wait.id;
        if(w.open){liveWaits.add(key);if(!waitFirstSeen.has(key))waitFirstSeen.set(key,Date.now());}
        const elapsed=Date.now()-(waitFirstSeen.get(key)||0),span=svg("rect",{x:X(w.from),y:r.y-6,width:Math.max(1,w.to-w.from),height:12,fill:"url(#wait-data)",class:"metro-wait"+(w.open&&elapsed<7200&&!reduced()?" open":"")});if(span.style)span.style.animationDelay=-elapsed+"ms";markTitle(span,w.label+" · "+(w.wait.reason||"input"));group.append(span);const label=svg("text",{x:X(Math.max(0,Math.min(w.from,geo.plotWidth-w.label.length*6.6))),y:r.y+25,class:"metro-wait-label"},w.label);markTitle(label,w.label);group.append(label);}
      for(const b of r.tools){const tick=svg("line",{x1:X(b.x),x2:X(b.x),y1:r.y-8,y2:r.y+8,class:"metro-tool"});markTitle(tick,b.count+" tool call"+(b.count===1?"":"s")+" · "+b.events.map(e=>e.metadata?.tool||e.kind).join(", "));group.append(tick);}
      for(const m of r.marks){const x=X(m.x),el=m.kind==="wait"?svg("path",{d:`M${x} ${r.y-8}L${x+8} ${r.y}L${x} ${r.y+8}L${x-8} ${r.y}Z`,class:"metro-wait-diamond"}):svg("circle",{cx:x,cy:r.y,r:4,class:"metro-event "+m.kind});markTitle(el,clock(m.at)+" · "+m.description);group.append(el);}
      const asof=svg("text",{x:X(geo.plotWidth)+12,y:r.y+22,class:"metro-asof","data-head-at":n.mixer?.observed_at||""},"as of "+age(n.mixer?.observed_at));
      const disc=svg("circle",{cx:X(r.head.x),cy:r.y,r:r.head.radius,class:"metro-head-disc "+st+" "+r.head.pattern});group.append(disc);
      if(r.head.pattern==="question")group.append(svg("text",{x:X(r.head.x),y:r.y+4,class:"metro-head-question"},"?"));
      group.append(svg("text",{x:X(geo.plotWidth)+14,y:r.y+4,class:"metro-head "+st},glyph(st)+" "+stateName(st)),asof);
      if(fresh.has(n.id)&&!reduced())for(const event of fresh.get(n.id)){
        const at=stamp(event.occurred_at);if(at<geo.from||at>geo.now)continue;
        const ring=svg("circle",{cx:X((at-geo.from)/(geo.minutes*minute)*geo.plotWidth),cy:r.y,r:12,class:"metro-ripple"});group.append(ring);
      }
      group.addEventListener("click",()=>rowSelect(n.id,false));canvas.append(group);
    }
    canvas.append(svg("line",{x1:X(geo.plotWidth),x2:X(geo.plotWidth),y1:8,y2:geo.height-30,class:"metro-now"}),svg("text",{x:X(geo.plotWidth),y:12,class:"metro-axis"},"now · "+clock(geo.now)));
    const earlier=root.querySelector("[data-flow-earlier]");earlier.textContent=geo.earlierMinutes?`${geo.earlierMinutes}m earlier · ${geo.earlierAsks} asks`:"";earlier.hidden=!geo.earlierMinutes;
    if(geo.quiet||expanded){const fold=svg("g",{class:"metro-quiet",role:"button",tabindex:0,"aria-expanded":expanded,"aria-label":expanded?"Hide quiet threads":"Show quiet threads"}),y=62+geo.rows.length*60;fold.append(svg("rect",{x:0,y:y-28,width,height:60,class:"metro-fold-bg"}),svg("text",{x:14,y:y+4,class:"metro-sub"},expanded?"quiet threads · hide ▴":"+"+geo.quiet+" quiet threads"),svg("text",{x:width-14,y:y+4,class:"metro-fold-label","text-anchor":"end"},expanded?"hide ▴":"show ▾"));const toggle=()=>{expanded=!expanded;render();};fold.addEventListener("click",toggle);fold.addEventListener("keydown",e=>{if(e.key==="Enter"||e.key===" "){e.preventDefault();toggle();}});canvas.append(fold);}
    for(const key of waitFirstSeen.keys())if(!liveWaits.has(key))waitFirstSeen.delete(key);
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
        nextIDs.add(n.id);
        if(workerIDs&&!workerIDs.has(n.id))newWorkers.add(n.id);
        for(const e of n.events||[]){const key=n.id+"/"+e.occurred_at+"/"+e.kind+"/"+(e.summary||"");nextSeen.add(key);if(seen&&!seen.has(key)){if(!fresh.has(n.id))fresh.set(n.id,[]);fresh.get(n.id).push(e);}}
      }
      seen=nextSeen;workerIDs=nextIDs;data=next;info("");render(fresh,newWorkers);
    } catch(err){info(err.message);}finally{inflight=false;if(pending){pending=false;fetchFlow();}}
  }
  function mount() {
    const next=document.querySelector("[data-flow-map]");if(next===root)return;
    root=next;requestID++;data=null;seen=null;workerIDs=null;cardFirstSeen.clear();waitFirstSeen.clear();if(!root)return;
    expanded=new URL(location.href).searchParams.get("quiet")==="1";selected=null;
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
  document.addEventListener("driga:select-session",event=>{if(!root||!data)return;const id=Number(event.detail.id);const node=[data.lead,...(data.children||[])].find(n=>n?.id===id);if(node){if(!geo.rows.some(r=>r.node.id===id))expanded=true;selected=id;render();rowSelect(id);}});
  document.addEventListener("driga:flow-refresh",fetchFlow);document.addEventListener("htmx:afterSwap",mount);
  let resizeTimer;window.addEventListener("resize",()=>{clearTimeout(resizeTimer);resizeTimer=setTimeout(()=>{if(data)render();},100);});
  const clockTimer=setInterval(()=>{if(!root)return;for(const el of root.querySelectorAll("[data-head-at]"))el.textContent="as of "+age(el.dataset.headAt);for(const el of root.querySelectorAll("[data-wait-since]"))el.textContent=duration(Date.now()-stamp(el.dataset.waitSince));},1000);
  window.addEventListener("pagehide",()=>clearInterval(clockTimer));mount();
})();
