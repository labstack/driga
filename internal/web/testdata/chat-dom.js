const assert = require('node:assert/strict');
class Element {
 constructor(tag, text=''){this.tagName=tag;this.children=[];this._text=text;this.handlers={};this.dataset={};this.value='';this.hidden=true;this.scrollHeight=0;this.scrollTop=0;this.clientHeight=0;this.classList={values:new Set(),add(...v){v.forEach(x=>this.values.add(x));},remove(...v){v.forEach(x=>this.values.delete(x));},contains(v){return this.values.has(v);},toggle(v,on){if(on)this.add(v);else this.remove(v);}};}
 append(...nodes){this.children.push(...nodes);}
 replaceChildren(...nodes){this._text='';this._html='';this.children=nodes;}
 set textContent(text){this._text=String(text);this._html='';this.children=[];}
 get textContent(){return this._text+this.children.map(n=>n.textContent).join('');}
 set innerHTML(html){this._html=html;this.children=[];this._text='';}
 get innerHTML(){return this._html||'';}
 addEventListener(kind,fn){this.handlers[kind]=fn;}
 focus(){document.activeElement=this;}
 blur(){if(document.activeElement===this)document.activeElement=null;}
 closest(){return this.tagName==='textarea'?this:null;}
 setAttribute(key,value){this[key]=value;}
}
const elements=new Map();
const get=id=>{if(!elements.has(id))elements.set(id,new Element('div'));return elements.get(id);};
const documentHandlers={};
global.document={activeElement:null,getElementById:get,createElement:tag=>new Element(tag),createTextNode:text=>new Element('#text',text),querySelector:selector=>selector.startsWith('meta')?{content:'csrf'}:null,addEventListener(kind,fn){documentHandlers[kind]=fn;},dispatchEvent(){},body:new Element('body')};
let reduced=true;
const motion={matches:reduced,addEventListener(kind,fn){this.change=fn;}};
global.window={addEventListener(){},matchMedia(){return motion;}};
let timer=null,now=0;
global.setInterval=fn=>{timer=fn;return 1;};
global.clearInterval=()=>{timer=null;};
Date.now=()=>now;
global.location={pathname:'/agents',search:''};
global.sessionStorage={getItem(){return null;},setItem(){}};
global.CustomEvent=class {constructor(type,options){this.type=type;this.detail=options.detail;}};
const requests=[],payloads=[];
global.fetch=async (path,options)=>(requests.push(path),payloads.push({path,options}),({ok:true,json:async()=>path.endsWith('new')?{id:'conversation'}:[] }));
let source;
global.EventSource=class extends Element {constructor(){super('source');source=this;}close(){}};
const walk=node=>[node,...node.children.flatMap(walk)];
const change=e=>source.handlers.change({data:JSON.stringify(e)});
const reset=(messages,active=messages.some(m=>m.status==='streaming'))=>source.handlers.reset({data:JSON.stringify({cursor:10,messages,active,ephemeral:false})});
async function runChatTests(){
 await window.drigaChat.open();
 const article=()=>get('chat-log').children.find(n=>n.tagName==='article');
 const body=()=>article().children[1];
 const status=s=>source.handlers.status({data:JSON.stringify(s)});
 const escape=target=>{const e={key:'Escape',target,preventDefault(){this.defaultPrevented=true;}};if(target===get('chat-input'))target.handlers.keydown(e);documentHandlers.keydown(e);};
 assert(get('chat').classList.contains('reduced-motion'),'missing reduced motion class');
 motion.change({matches:false});
 assert(!get('chat').classList.contains('reduced-motion'));
 assert.equal(get('chat-stop').hidden,true,'idle Stop visible');
 status({active:true});
 const placeholder=article();
 assert(placeholder.textContent.includes('Driga'),'missing Driga label');
 assert(placeholder.textContent.includes('thinking…'),'missing thinking placeholder');
 assert.equal(get('chat-stop').hidden,false);
 change({seq:1,kind:'message',message:{id:'pending-reply',role:'assistant',status:'streaming',text:''}});
 assert.equal(article(),placeholder,'thinking placeholder lost identity on reply start');
 now=65000;timer();
 assert(get('chat-status').textContent.includes('1m 05s'),'elapsed time');
 reset([{id:'answer',role:'assistant',status:'complete',text:'raw fallback',html:serverHTML.answer}]);
 assert.equal(body().innerHTML,serverHTML.answer,'server HTML not inserted');
 for(const tag of ['<h1>','<strong>','<em>','<code>','<table>','type="checkbox"'])assert(body().innerHTML.includes(tag),'missing server Markdown '+tag);
 assert(body().innerHTML.includes('#42'),'citation lost');
 assert(!body().innerHTML.includes('<script>')&&!body().innerHTML.includes('href="javascript:'),'unsafe server HTML');
 reset([{id:'stream',role:'assistant',status:'streaming',text:'',html:''}]);
 const streamNode=article();
 change({seq:11,kind:'delta',message_id:'stream',text:'**Hel',html:serverHTML.partial});
 assert.equal(body().innerHTML,serverHTML.partial,'partial HTML not inserted');
 assert.equal(article(),streamNode,'placeholder replaced instead of updated');
 assert(get('chat-status').textContent.includes('writing…'));
 assert(walk(article()).some(n=>n.className==='chat-cursor'&&n.textContent==='▍'));
 change({seq:11.5,kind:'message',message:{id:'tool',role:'tool',text:'mcp__driga__list_agents'}});
 assert(get('chat-status').textContent.includes('reading agents…'));
 await get('chat-stop').handlers.click();
 assert(requests.includes('/chat/api/stop'),'Stop did not call API');
 change({seq:12,kind:'delta',message_id:'stream',text:'lo**',html:serverHTML.complete});
 change({seq:12,kind:'delta',message_id:'stream',text:'duplicate',html:'replayed'});
 assert.equal(body().innerHTML,serverHTML.complete,'duplicate replay changed HTML');
 change({seq:13,kind:'message',message:{id:'stream',role:'assistant',status:'complete',text:'**Hello**',html:serverHTML.complete}});
 assert.equal(body().innerHTML,serverHTML.complete);
 status({active:false});
 assert.equal(timer,null,'idle timer');
 assert.equal(get('chat-stop').hidden,true);
 assert(!walk(article()).some(n=>n.className==='chat-cursor'));
 const mark=walk(article()).find(n=>n.tagName==='img');
 assert(mark&&mark.src==='/static/brand/murmur.svg'&&mark.alt==='','decorative mark missing');
 reset([{id:'plain',role:'user',status:'sent',text:'# **plain**\n<script>user text</script>',html:'<strong>must ignore</strong>'}]);
 assert.equal(body().innerHTML,'','user HTML field was trusted');
 assert.equal(body().textContent,'# **plain**\n<script>user text</script>');
 assert.equal(article().children[0].textContent,'You','user label');
 reset([{id:'fallback',role:'assistant',status:'complete',text:'**plain fallback**'}]);
 assert.equal(body().textContent,'**plain fallback**','client still parses Markdown');
 assert(!get('chat-log').textContent.includes('Claude'),'provider used as message label');
 const input=get('chat-input');
 input.value='draft';input.focus();escape(input);
 assert.equal(document.activeElement,null,'Escape did not blur draft');
 assert.equal(get('chat').hidden,false,'draft Escape closed panel');
 assert.equal(input.value,'draft');
 input.value='';input.focus();escape(input);
 assert.equal(get('chat').hidden,true,'empty Escape did not close');
 await window.drigaChat.open();
 const log=get('chat-log');log.scrollHeight=1000;log.clientHeight=100;log.scrollTop=0;log.handlers.scroll();
 status({active:true});
 change({seq:20,kind:'message',message:{id:'next',role:'assistant',status:'streaming',text:'new'}});
 assert.equal(log.scrollTop,0,'stream stole scroll position');
 log.scrollTop=900;log.handlers.scroll();
 change({seq:21,kind:'delta',message_id:'next',text:' text'});
 assert.equal(log.scrollTop,1000,'stream did not follow bottom');
 status({active:false});
 for(const state of ['stopped','error','interrupted']){
  reset([{id:'ending',role:'assistant',status:'streaming',text:''}]);
  change({seq:11,kind:'message',message:{id:'ending',role:'assistant',status:state,text:state}});
  assert.equal(timer,null,state+' left timer active');
  assert.equal(get('chat-stop').hidden,true,state+' left Stop visible');
 }
 reset([{id:'connection',role:'assistant',status:'streaming',text:'partial'}]);
 source.onerror();
 assert.equal(timer,null,'connection error left timer active');
 assert(!walk(article()).some(n=>n.className==='chat-cursor'));
 assert.equal(get('chat-status').textContent,'Reconnecting…');
 const action={id:'action',tool:'Bash',title:'Bash · touch demo',input:{command:'touch demo'},directory:'/project',status:'pending'};
 source.handlers.reset({data:JSON.stringify({cursor:30,messages:[{id:'action',role:'action',status:'pending',text:'Bash'}],actions:[action],mode:'full auto',active:true})});
 assert.equal(get('chat-mode-full')['aria-pressed'],'true');
 assert(get('chat-mode-full').classList.contains('danger'),'Full auto must be red');
 assert(get('chat-status').textContent.includes('action waiting'),'pending status missing');
 const buttons=walk(get('chat-log')).filter(n=>n.tagName==='button');
 assert.deepEqual(buttons.map(n=>n.textContent),['Allow','Deny']);
 assert(buttons.every(n=>n.type==='button'&&n['aria-label']),'action buttons lack native keyboard semantics');
 await buttons[0].handlers.click();
 const decision=payloads.find(p=>p.path==='/chat/api/decide');
 assert(decision&&JSON.parse(decision.options.body).allow===true,'Allow did not decide');
 assert.equal(decision.options.headers['X-CSRF-Token'],'csrf');
 change({seq:31,kind:'action',action:{...action,status:'denied',decided_by:'you'}});
 assert(!walk(get('chat-log')).some(n=>n.tagName==='button'),'decided card kept buttons');
 await get('chat-mode-auto').handlers.click();
 assert(payloads.some(p=>p.path==='/chat/api/mode'&&JSON.parse(p.options.body).mode==='auto'));
 change({seq:32,kind:'mode',mode:'auto'});
 assert.equal(get('chat-mode-auto')['aria-pressed'],'true');
 assert(!get('chat-mode-full').classList.contains('danger'));
 const fullPosts=()=>payloads.filter(p=>p.path==='/chat/api/mode'&&JSON.parse(p.options.body).mode==='full auto').length;
 global.confirm=()=>false;await get('chat-mode-full').handlers.click();
 assert.equal(fullPosts(),0,'Full auto sent without confirmation');
 let asked='';global.confirm=text=>{asked=text;return true;};await get('chat-mode-full').handlers.click();
 assert.equal(fullPosts(),1);assert(asked.includes('30 minutes'),'confirmation must state the limit');
 change({seq:33,kind:'mode',mode:'full auto',until:new Date(now+30*60000).toISOString()});
 assert.equal(get('chat-mode-left').hidden,false);assert.equal(get('chat-mode-left').textContent,'30m left');
 change({seq:34,kind:'mode',mode:'auto'});
 assert.equal(get('chat-mode-left').hidden,true);
 location.pathname='/chat';input.value='';input.focus();escape(input);
 assert.equal(get('chat').hidden,true,'dedicated page Escape did not close');
}
