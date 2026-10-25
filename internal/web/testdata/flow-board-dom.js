const assert=require('node:assert/strict');
const handlers={};let now=1000,timer,requests=[];
class Element {
 constructor(tag,dataset={},attrs={}){this.tagName=tag;this.dataset=dataset;this.attrs=attrs;this.children=[];this.hidden=false;this.scrollTop=0;this.scrollLeft=0;this.value='';this.id=attrs.id||'';this.className=attrs.class||'';this.classList={values:new Set(),toggle:(c,on)=>on?this.classList.values.add(c):this.classList.values.delete(c),contains:c=>this.classList.values.has(c)};}
 append(...nodes){for(const n of nodes){if(n.parentElement)n.parentElement.children=n.parentElement.children.filter(c=>c!==n);n.parentElement=this;this.children.push(n);}}
 matches(s){if(s.includes(','))return s.split(',').some(p=>this.matches(p.trim()));if(s==='input')return this.tagName==='input';if(s.startsWith('.'))return this.className.split(' ').includes(s.slice(1));if(s.startsWith('#'))return this.id===s.slice(1);if(s.startsWith('[')){const m=s.match(/^\[([^=\]]+)(?:="?([^"\]]+)"?)?\]$/);if(!m)return false;const key=m[1].startsWith('data-')?m[1].slice(5).replace(/-([a-z])/g,(_,c)=>c.toUpperCase()):null;const v=key?this.dataset[key]:this.attrs[m[1]];return v!==undefined&&(m[2]===undefined||v===m[2]);}return this.tagName===s;}
 closest(s){return this.matches(s)?this:this.parentElement?.closest(s)||null;}
 contains(n){return n===this||this.children.some(c=>c.contains(n));}
 querySelectorAll(s){const descendants=this.children.flatMap(n=>[n,...n.querySelectorAll('*')]);if(s==='*')return descendants;const split=s.lastIndexOf(' ');if(split>0){const parent=s.slice(0,split),child=s.slice(split+1);return descendants.filter(n=>n.matches(child)&&n.parentElement?.closest(parent));}return descendants.filter(n=>n.matches(s));}
 querySelector(s){return this.querySelectorAll(s)[0]||null;}
 setAttribute(k,v){this.attrs[k]=String(v);}getAttribute(k){return this.attrs[k]??null;}
 focus(){document.activeElement=this;}setSelectionRange(a,b){this.selectionStart=a;this.selectionEnd=b;}
}
const body=new Element('body'),main=new Element('main',{}, {id:'main'}),host=new Element('div',{flowBoard:'',observed:new Date(now).toISOString()});body.append(main);main.append(host);
const search=new Element('input',{boardSearch:''},{id:'flow-find'}),updated=new Element('span',{boardUpdated:''}),status=new Element('p',{boardStatus:''});host.append(search,updated,status);
const surfaces={},rows={},details={};
for(const view of ['map','timeline','sessions']){host.append(new Element('button',{flowView:view}));const surface=new Element('section',{flowSurface:view},{class:'fb-'+(view==='map'?'canvas':view)});surfaces[view]=surface;host.append(surface);rows[view]=[];for(let i=1;i<=3;i++){const row=new Element(view==='map'?'g':'div',{boardKey:'local/'+i,root:'local/1',session:String(i),machine:'local',title:'Session '+i,project:'project',parent:i>1?'local/1':''},{id:view+'-'+i,role:view==='timeline'?'treeitem':'button'});surface.append(row);rows[view].push(row);}}
for(let i=1;i<=3;i++){const detail=new Element('div',{detail:'local/'+i});host.append(detail);details[i]=detail;const family=new Element('div',{familyRow:'local/'+i,familyRoot:'local/1'});family.append(new Element('button'));host.append(family);}
const panel=new Element('aside',{}, {class:'fb-panel'});host.append(panel);
const stop=new Element('button',{stop:'local/2'});host.append(stop);
const prompt=new Element('div',{prompt:'p1'}),allow=new Element('button',{answer:'allow'},{id:'prompt-p1-allow'});prompt.append(allow);host.append(prompt);
global.document={activeElement:body,body,querySelector:s=>body.querySelector(s),getElementById:id=>body.querySelector('#'+id),addEventListener:(name,fn)=>(handlers[name]??=[]).push(fn),dispatchEvent:()=>{}};
global.location={search:'',pathname:'/flow'};global.window={htmx:null};global.CSS={escape:s=>s};global.CustomEvent=class{constructor(type,o){this.type=type;this.detail=o.detail;}};global.setInterval=fn=>(timer=fn,1);Date.now=()=>now;
// The app's native dialog; 404 is a page without a host.
let confirmReply={ok:false,status:404};
global.fetch=async(path,options)=>(requests.push({path,body:JSON.parse(options.body)}),path==='/app/confirm'?confirmReply:{ok:true});
const event=(type,target,key)=>{const e={target,key,preventDefault(){this.defaultPrevented=true;}};return Promise.all((handlers[type]||[]).map(fn=>fn(e)));};
async function runFlowDOMTests(){
 assert(rows.map[0].classList.contains('selected'));assert.equal(document.activeElement,body,'initialization stole focus');assert(allow.disabled,'new permission was armed immediately');
 await event('click',host.querySelector('[data-flow-view="timeline"]'));rows.timeline[0].focus();await event('keydown',rows.timeline[0],'ArrowDown');assert.equal(document.activeElement,rows.timeline[1]);assert.equal(rows.timeline[1].tabIndex,0);assert.equal(rows.timeline[0].tabIndex,-1);
 surfaces.timeline.scrollTop=170;panel.scrollTop=60;await event('htmx:beforeSwap',main);document.activeElement=body;surfaces.timeline.hidden=true;await event('htmx:afterSwap',main);
 assert.equal(document.activeElement,rows.timeline[1],'refresh lost focus');assert(rows.timeline[1].classList.contains('selected'),'refresh lost identity');assert.equal(surfaces.timeline.hidden,false);assert.equal(surfaces.timeline.scrollTop,170);assert.equal(panel.scrollTop,60);
 search.focus();search.value='session 2';search.selectionStart=2;search.selectionEnd=7;await event('input',search);await event('htmx:beforeSwap',main);search.value='';document.activeElement=body;await event('htmx:afterSwap',main);assert.equal(search.value,'session 2');assert.equal(document.activeElement,search);assert.deepEqual([search.selectionStart,search.selectionEnd],[2,7]);assert(rows.timeline[0].hidden);assert(!rows.timeline[1].hidden);
 await event('click',allow);assert.equal(requests.length,0,'unarmed prompt submitted');now+=700;timer();await event('click',allow);assert.equal(requests.length,1);assert.deepEqual(requests[0].body,{id:'p1',allow:true});assert.equal(document.activeElement,search,'answer stole focus');
 const kills=()=>requests.filter(r=>r.path==='/chat/api/flow-action');
 await event('click',stop);assert.equal(kills().length,0,'stopped without an app to confirm');
 confirmReply={ok:true,json:async()=>({confirmed:false})};await event('click',stop);assert.equal(kills().length,0,'stopped after Cancel');
 confirmReply={ok:true,json:async()=>({confirmed:true})};await event('click',stop);assert.deepEqual(kills().map(r=>r.body),[{action:'kill',session_id:2,machine:'local',confirmed:true}]);
 assert(requests.filter(r=>r.path==='/app/confirm').every(r=>r.body.message.includes('Session 2')),'dialog names the session');
 console.log('Flow DOM focus, selection, scroll, filtering and approval regressions passed');
}
