const assert = require("node:assert/strict");
class Element {
  constructor(tag="div") { this.listeners={};this.style={};this.tag=tag;this.children=[];this.dataset={};this.attrs={};this.textContent="";this.parentElement={clientWidth:1200};this.classList={toggle(){}}; }
  append(...items) { this.children.push(...items); }
  replaceChildren(...items) { this.children=items; }
  setAttribute(k,v) { this.attrs[k]=v; }
  addEventListener(key,fn) { this.listeners[key]=fn; }
  querySelectorAll() { return []; }
  get childElementCount() { return this.children.length; }
}
const panels=new Map(),root=new Element();
root.querySelector=key=>{if(!panels.has(key))panels.set(key,new Element());return panels.get(key);};
root.querySelector("[data-flow-zoom]").value="60";
globalThis.document={querySelector:key=>key==="[data-flow-map]"?root:null,createElement:tag=>new Element(tag),createElementNS:(_,tag)=>new Element(tag),addEventListener(){}};
globalThis.window={addEventListener(){}};
globalThis.location={href:"http://fixture/flow?id=1",origin:"http://fixture"};
let reduceMotion=true;globalThis.matchMedia=()=>({matches:reduceMotion});
globalThis.setInterval=()=>0;
const now=Date.now();let wall=now;Date.now=()=>wall;const at=ms=>new Date(now+ms).toISOString();
const lead={id:1,machine_id:"local",adapter:"claude",title:"Long session",started_at:at(-2765*60000),ended_at:at(-60*60000),elapsed_text:"45h5m0s",jump:{kind:"resume",cwd:"/fixture/it's a project",command:"claude --resume 'fixture'"},events:[],waits:[],mixer:{verdict:"finished",observed_at:at(0),agreement:1,rows:[{name:"registry",reading:"idle",strength:1,supports:true},{name:"hooks",reading:"Stop",strength:1,supports:false},{name:"telemetry",reading:"unavailable",strength:0}]}};
const waits=["sample · Fix cache","sample · Fix parser","sample · #ccc333","sample · #ddd444"].map((title,i)=>({title,session_id:i+1,machine_id:"local",reason:"unknown",occurred_at:at(-60000)}));
globalThis.fetch=async()=>({ok:true,json:async()=>({schema:2,data:{lead,children:[child,{id:3,started_at:at(-600000),events:[],waits:[]}],attention:{waits},observed_at:at(0),window_minutes:60,gaps:[]}})});
const child={id:2,parent_id:1,adapter:"codex",title:"Worker",started_at:at(-10*60000),retained_from_at:at(-2*60000),events:[],waits:[{id:"open",started_at:at(-60000)}],mixer:{verdict:"waiting",observed_at:at(0)}};
const allText=el=>[el.textContent,...el.children.map(allText)].join(" ");
let copied="";
Object.defineProperty(globalThis,"navigator",{value:{clipboard:{writeText:async text=>{copied=text;}}},configurable:true});
setImmediate(async()=>{
  assert.equal(root.querySelector("[data-flow-status]").textContent, "", "render failed");
  const dl=root.querySelector("[data-flow-detail]").children.find(el=>el.tag==="dl");
  assert(dl,"missing session details");
  assert.equal(dl.children[1].textContent,"45h5m0s","running duration must use shared server formatter and respect ended_at");
  const cards=root.querySelector("[data-flow-attention]");
  for(const wait of waits)assert(allText(cards).includes(wait.title),"live card lost session hint "+wait.title);
  const detail=root.querySelector("[data-flow-detail]");
  for(const copy of ["verdict as of","telemetry unavailable","Why finished","1 of 4 signals agree","✓","✗","–"])assert(allText(detail).includes(copy),"missing detail copy "+copy);
  assert(allText(cards).includes("what it asked wasn't captured"));
  const walk=el=>[el,...el.children.flatMap(walk)];
  const svg=walk(root.querySelector("[data-flow-svg]"));
  assert(svg.some(el=>el.tag==="circle"&&Number(el.attrs.r)===7),"14px verdict disc missing");
  assert(!svg.some(el=>(el.attrs.class||"").includes("metro-head-pattern")),"legacy dashed stub");
  assert(svg.some(el=>el.textContent.startsWith("now · ")),"now clock missing");
  const actions=detail.children.findIndex(el=>el.className==="metro-actions");
  assert(actions>=0&&actions<detail.children.indexOf(dl),"actions precede facts");
  const buttons=detail.children[actions].children;
  await buttons[1].listeners.click();
  assert.equal(copied,`cd -- '/fixture/it'"'"'s a project' && claude --resume 'fixture'`);
  assert.equal(root.querySelector("[data-flow-status]").textContent,"Resume command copied.");
  assert.equal(root.querySelector("[data-flow-count]").textContent,"4");
  assert(!cards.children.some(el=>el.className.includes("fresh")),"reduced motion disables fresh-card pulse");
  assert(svg.some(el=>el.attrs.class==="metro-hatch"&&Number(el.attrs.height)===22),"22px no-data band");
  assert(svg.some(el=>el.attrs.class==="metro-quiet"),"quiet fold must live inside SVG");
  assert(svg.some(el=>el.textContent==="show ▾"));
  reduceMotion=false;
  await root.querySelector("[data-flow-zoom]").listeners.change();
  assert(cards.children.some(el=>el.className.includes("fresh")),"new card pulse missing");
  wall+=8000;
  await root.querySelector("[data-flow-zoom]").listeners.change();
  assert(!cards.children.some(el=>el.className.includes("fresh")),"refresh restarted a finished card pulse");
  assert(!walk(root.querySelector("[data-flow-svg]")).some(el=>(el.attrs.class||"")==="metro-wait open"),"refresh restarted a finished wait pulse");
});
