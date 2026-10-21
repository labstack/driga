const assert = require("node:assert/strict");
const {geometry,eventMark}=globalThis.DrigaFlow;
const now=Date.parse("2026-10-20T12:00:00Z"),at=m=>new Date(now+m*60000).toISOString();
const node=(id,start)=>({id,adapter:"claude",started_at:at(start),retained_from_at:at(-59),events:[],waits:[],handoffs:[],mixer:{verdict:"working"}});
const lead=node(1,-480),a=node(3,-100),b=node(2,-100);
for(let i=0;i<100;i++)lead.events.push({kind:"otlp:tool_result",occurred_at:at(-2+i/100)});
lead.events.push({kind:"Notification",occurred_at:at(-4)});
lead.waits=[{started_at:at(-5),ended_at:at(-4.99),reason:"permission"}];
const quiet=Array.from({length:12},(_,i)=>node(i+10,-400+i));
const data={lead,children:[a,b,...quiet],window_minutes:240,observed_at:at(0),gaps:[{started_at:at(-20),ended_at:at(-18)}]};
let g=geometry(data,1400,true);
assert.deepEqual(g.rows.slice(1).map(r=>r.node.id),[...quiet.map(n=>n.id),2,3]);
assert.equal(g.rows[0].tools.reduce((s,b)=>s+b.count,0),100);
assert.equal(g.rows[0].waits.length,1,"Notification cannot create wait spans");
assert(g.rows[0].waits[0].to>g.rows[0].waits[0].from,"short waits remain visible");
assert(g.rows[0].hatches[0].to/g.plotWidth>.7,"snapshot fixture mostly hatches");
assert.equal(g.rows[0].hatches.length,2);
assert.equal(geometry(data,1400,false).quiet,14);
a.mixer.verdict="waiting";a.last_activity_at=at(0);data.children.reverse();
assert.deepEqual(geometry(data,1400,true).rows.map(r=>r.node.id),g.rows.map(r=>r.node.id));
assert.equal(eventMark("codex","otlp:tool_result"),"");
assert.equal(eventMark("gemini","Stop"),"");
lead.waits=[];assert.equal(geometry(data,1400,true).rows[0].waits.length,0);
lead.observed={from_at:at(-2),log_bytes_per_min:[100,0],cpu_ms_per_min:[0,300]};
g=geometry(data,500,true);assert(g.minutes<=g.plotWidth/4);assert(g.earlierMinutes>0);assert.equal(g.rows[0].activity.length,2);
console.log("flow map geometry passed");

// The rail is existence only; verdict patterns belong exclusively to its head.
const f=globalThis.DrigaFlow;
lead.ended_at=at(-1);lead.retained_from_at=at(-59);
g=f.geometry(data,1400,true);
assert.equal(g.rows[0].rail.from,g.rows[0].start);
assert(g.rows[0].rail.to<g.plotWidth);
assert.equal(g.rows[0].head.x,g.plotWidth,"verdict stays at now, even on ended sessions");
assert.equal(g.rows[0].head.radius,7);
for(const [s,pattern] of Object.entries({working:"filled",waiting:"filled",idle:"ring",stalled:"dashed",unknown:"question",finished:"filled"})) {
  lead.mixer.verdict=s;assert.equal(f.geometry(data,1400,true).rows[0].head.pattern,pattern);
}
lead.waits=[{id:"closed",started_at:at(-5),ended_at:at(-2)},{id:"open",started_at:at(-1)}];
g=f.geometry(data,1400,true);
assert.equal(g.rows[0].waits[0].label,"needs you · 3m · answered");
assert.equal(g.rows[0].waits[1].label,"needs you · 1m · open");
assert(g.rows[0].hatches.every(h=>h.from>=g.rows[0].rail.from&&h.to<=g.rows[0].rail.to));
assert.deepEqual(f.sortAttention([{id:"new",occurred_at:at(-1)},{id:"old",waiting_since_at:at(-30),occurred_at:at(-2)}]).map(w=>w.id),["old","new"]);
assert.equal(f.waitReason({reason:"unknown",detail:"must not be invented"}),"◆ Asked for input · what it asked wasn't captured");
assert.equal(f.waitReason({reason:"permission",tool:"Bash",detail:"fixture command"}),"◆ Permission · Bash: fixture command");
assert.equal(f.waitReason({reason:"permission",tool:"Bash"}),"◆ Permission · Bash");
assert.equal(f.signalGlyph({strength:0,supports:false}),"–");
assert.equal(f.signalGlyph({strength:1,supports:true}),"✓");
assert.equal(f.signalGlyph({strength:1,supports:false}),"✗");
const runs=f.recentEvents(Array.from({length:7},(_,i)=>({kind:"Stop",occurred_at:at(-10+i)})));
assert.equal(runs.length,4);assert.equal(runs[3].more,4);
assert.equal(runs[3].kind,"Stop");assert.equal(runs[3].from,at(-10));assert.equal(runs[3].to,at(-7));

assert.equal(f.sessionTitle({title:"Task",adapter:"claude",started_from:"Editor"}),"Task · from Editor");
assert.equal(f.sessionTitle({title:"Task",adapter:"claude"}),"Task");
