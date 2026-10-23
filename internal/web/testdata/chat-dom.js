const assert = require('node:assert/strict');
class Element {
 constructor(tag, text=''){this.tagName=tag;this.children=[];this._text=text;this.handlers={};this.dataset={};this.value='';this.hidden=true;this.scrollHeight=0;this.scrollTop=0;this.clientHeight=0;this.classList={add(){},remove(){}};}
 append(...nodes){this.children.push(...nodes);}
 replaceChildren(...nodes){this._text='';this.children=nodes;}
 set textContent(text){this._text=String(text);this.children=[];}
 get textContent(){return this._text+this.children.map(n=>n.textContent).join('');}
 set innerHTML(html){this._html=html;this.children=[];this._text='';}
 get innerHTML(){return this._html||'';}
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
 const body=()=>get('chat-log').children[0].children[1];
 reset([{id:'answer',role:'assistant',status:'complete',text:'raw fallback',html:serverHTML.answer}]);
 assert.equal(body().innerHTML,serverHTML.answer,'server HTML not inserted');
 for(const tag of ['<h1>','<strong>','<em>','<code>','<table>','type="checkbox"'])assert(body().innerHTML.includes(tag),'missing server Markdown '+tag);
 assert(body().innerHTML.includes('#42'),'citation lost');
 assert(!body().innerHTML.includes('<script>')&&!body().innerHTML.includes('href="javascript:'),'unsafe server HTML');
 reset([{id:'stream',role:'assistant',status:'streaming',text:'',html:''}]);
 change({seq:11,kind:'delta',message_id:'stream',text:'**Hel',html:serverHTML.partial});
 assert.equal(body().innerHTML,serverHTML.partial,'partial HTML not inserted');
 change({seq:12,kind:'delta',message_id:'stream',text:'lo**',html:serverHTML.complete});
 change({seq:12,kind:'delta',message_id:'stream',text:'duplicate',html:'replayed'});
 assert.equal(body().innerHTML,serverHTML.complete,'duplicate replay changed HTML');
 change({seq:13,kind:'message',message:{id:'stream',role:'assistant',status:'complete',text:'**Hello**',html:serverHTML.complete}});
 assert.equal(body().innerHTML,serverHTML.complete);
 reset([{id:'plain',role:'user',status:'sent',text:'# **plain**\n<script>user text</script>',html:'<strong>must ignore</strong>'}]);
 assert.equal(body().innerHTML,'','user HTML field was trusted');
 assert.equal(body().textContent,'# **plain**\n<script>user text</script>');
 reset([{id:'fallback',role:'assistant',status:'complete',text:'**plain fallback**'}]);
 assert.equal(body().textContent,'**plain fallback**','client still parses Markdown');
}
