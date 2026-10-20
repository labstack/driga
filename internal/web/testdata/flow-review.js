const assert = require("node:assert/strict");
class Element {
  constructor(tag="div") { this.tag=tag;this.children=[];this.dataset={};this.attrs={};this.textContent="";this.parentElement={clientWidth:1200};this.classList={toggle(){}}; }
  append(...items) { this.children.push(...items); }
  replaceChildren(...items) { this.children=items; }
  setAttribute(k,v) { this.attrs[k]=v; }
  addEventListener() {}
  querySelectorAll() { return []; }
  get childElementCount() { return this.children.length; }
}
const panels=new Map(),root=new Element();
root.querySelector=key=>{if(!panels.has(key))panels.set(key,new Element());return panels.get(key);};
root.querySelector("[data-flow-zoom]").value="60";
globalThis.document={querySelector:key=>key==="[data-flow-map]"?root:null,createElement:tag=>new Element(tag),createElementNS:(_,tag)=>new Element(tag),addEventListener(){}};
globalThis.window={addEventListener(){}};
globalThis.location={href:"http://fixture/flow?id=1",origin:"http://fixture"};
globalThis.matchMedia=()=>({matches:true});
globalThis.setInterval=()=>0;
const now=Date.now(),at=ms=>new Date(now+ms).toISOString();
const lead={id:1,machine_id:"local",adapter:"claude",title:"Long session",started_at:at(-2765*60000),ended_at:at(-60*60000),elapsed_text:"45h5m0s",events:[],waits:[],mixer:{verdict:"finished",observed_at:at(0)}};
const waits=["sample · Fix cache","sample · Fix parser","sample · #ccc333","sample · #ddd444"].map((title,i)=>({title,session_id:i+1,machine_id:"local",reason:"unknown",occurred_at:at(-60000)}));
globalThis.fetch=async()=>({ok:true,json:async()=>({schema:2,data:{lead,children:[],attention:{waits},observed_at:at(0),window_minutes:60,gaps:[]}})});
const allText=el=>[el.textContent,...el.children.map(allText)].join(" ");
setImmediate(()=>{
  assert.equal(root.querySelector("[data-flow-status]").textContent, "", "render failed");
  const dl=root.querySelector("[data-flow-detail]").children.find(el=>el.tag==="dl");
  assert(dl,"missing session details");
  assert.equal(dl.children[1].textContent,"45h5m0s","running duration must use shared server formatter and respect ended_at");
  const cards=root.querySelector("[data-flow-attention]");
  for(const wait of waits)assert(allText(cards).includes(wait.title),"live card lost session hint "+wait.title);
});
