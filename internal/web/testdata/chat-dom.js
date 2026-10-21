const assert = require('node:assert/strict');
class Element {
 constructor(tag, text=''){this.tagName=tag;this.children=[];this._text=text;this.handlers={};this.dataset={};this.value='';this.hidden=true;this.scrollHeight=0;this.scrollTop=0;this.clientHeight=0;this.classList={add(){},remove(){}};}
 append(...nodes){this.children.push(...nodes);}
 replaceChildren(...nodes){this._text='';this.children=nodes;}
 set textContent(text){this._text=String(text);this.children=[];}
 get textContent(){return this._text+this.children.map(n=>n.textContent).join('');}
 set innerHTML(_){throw new Error('HTML parsing is forbidden');}
 addEventListener(kind,fn){this.handlers[kind]=fn;}
 focus(){}
}
const elements=new Map();
const get=id=>{if(!elements.has(id))elements.set(id,new Element('div'));return elements.get(id);};
global.document={getElementById:get,createElement:tag=>new Element(tag),createTextNode:text=>new Element('#text',text),querySelector:selector=>selector.startsWith('meta')?{content:'csrf'}:null,addEventListener(){},dispatchEvent(){},body:new Element('body')};
global.window={addEventListener(){}};
global.location={pathname:'/agents',search:''};
global.sessionStorage={getItem(){return null;},setItem(){}};
global.CustomEvent=class {constructor(type,options){this.type=type;this.detail=options.detail;}};
global.fetch=async path=>({ok:true,json:async()=>path.endsWith('new')?{id:'conversation'}:[]});
let source;
global.EventSource=class extends Element {constructor(){super('source');source=this;}close(){}};
const walk=node=>[node,...node.children.flatMap(walk)];
const change=e=>source.handlers.change({data:JSON.stringify(e)});
const reset=messages=>source.handlers.reset({data:JSON.stringify({cursor:10,messages,ephemeral:false})});
async function runChatTests(){
 await window.drigaChat.open();
 const hostile='<script>alert(1)</script> <img src=x onerror="alert(2)">';
 const text='A **bold** and *italic* with `inline` and [[session:42]].\n\n'+
 '```js\n<script>code only</script>\n```\n\n- first\n- second\n\n1. one\n2. two\n\n'+
 '[safe](https://example.com) [bad](javascript:alert(1)) [inject](https://x/" onclick="alert(1))\n\n'+hostile;
 reset([{id:'answer',role:'assistant',status:'complete',text}]);
 const nodes=walk(get('chat-log'));
 for(const tag of ['p','strong','em','code','pre','ul','ol','li'])assert(nodes.some(n=>n.tagName===tag),'missing Markdown '+tag);
 assert(nodes.some(n=>n.tagName==='strong'&&n.textContent==='bold'));
 assert(nodes.some(n=>n.tagName==='code'&&n.textContent==='inline'));
 assert(get('chat-log').textContent.includes(hostile),'raw HTML should remain literal text');
 for(const n of nodes){assert(!['script','img','iframe'].includes(n.tagName));assert(!n.href||/^https?:/.test(n.href));assert(!n.onclick&&!n.onerror);}
 const chip=nodes.find(n=>n.tagName==='button'&&n.textContent==='session #42');
 assert(chip,'session citation lost');chip.handlers.click();
 // Real event handlers must append each ordered delta once, including replay.
 reset([{id:'stream',role:'assistant',status:'streaming',text:''}]);
 change({seq:11,kind:'delta',message_id:'stream',text:'**Hel'});
 assert(get('chat-log').textContent.includes('Hel'),'first partial not rendered');
 change({seq:12,kind:'delta',message_id:'stream',text:'lo**'});
 change({seq:12,kind:'delta',message_id:'stream',text:'lo**'});
 assert.equal(walk(get('chat-log')).find(n=>n.tagName==='strong').textContent,'Hello');
 change({seq:13,kind:'message',message:{id:'stream',role:'assistant',status:'complete',text:'**Hello**'}});
 assert(!get('chat-log').textContent.includes('Hellolo'));
}
