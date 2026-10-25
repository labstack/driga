const assert = require('node:assert/strict');
class Element {
 constructor(tag, text=''){this.tagName=tag;this.children=[];this._text=text;this.handlers={};this.dataset={};this.value='';this.hidden=true;this.scrollHeight=0;this.scrollTop=0;this.clientHeight=0;this.classList={values:new Set(),add(...v){v.forEach(x=>this.values.add(x));},remove(...v){v.forEach(x=>this.values.delete(x));},contains(v){return this.values.has(v);},toggle(v,on){if(on)this.add(v);else this.remove(v);}};}
 append(...nodes){for(const node of nodes){if(node.parentElement)node.parentElement.removeChild(node);node.parentElement=this;this.children.push(node);}}
 insertBefore(node,before){if(node.parentElement)node.parentElement.removeChild(node);const i=before?this.children.indexOf(before):this.children.length;this.children.splice(i,0,node);node.parentElement=this;}
 removeChild(node){if(walk(node).includes(document.activeElement))document.activeElement=null;this.children.splice(this.children.indexOf(node),1);node.parentElement=null;return node;}
 get lastChild(){return this.children.at(-1);}

 replaceChildren(...nodes){this._text='';this._html='';for(const node of [...this.children])this.removeChild(node);this.append(...nodes);}
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
global.document={activeElement:null,getElementById:get,createElement:tag=>new Element(tag),createTextNode:text=>new Element('#text',text),querySelector:()=>null,addEventListener(kind,fn){documentHandlers[kind]=fn;},dispatchEvent(event){this.lastEvent=event;},body:new Element('body')};
let reduced=true;
const motion={matches:reduced,addEventListener(kind,fn){this.change=fn;}};
global.window={addEventListener(){},matchMedia(){return motion;}};
let timer=null,now=0;
global.setInterval=fn=>{timer=fn;return 1;};
let retry=null;
global.setTimeout=fn=>{retry=fn;return 1;};
global.clearInterval=()=>{timer=null;};
Date.now=()=>now;
global.location={pathname:'/agents',search:''};
global.sessionStorage={getItem(){return null;},setItem(){}};
global.CustomEvent=class {constructor(type,options){this.type=type;this.detail=options.detail;}};
const requests=[],payloads=[];
// The Chat long poll waits until a test delivers its answer.
let waiting=null;
global.fetch=(path,options)=>{requests.push(path);payloads.push({path,options});
 if(path.startsWith('/chat/api/wait'))return new Promise((resolve,reject)=>{waiting={resolve,reject};});
 return Promise.resolve({ok:true,json:async()=>path.endsWith('new')?{id:'conversation'}:[]});};
const settle=()=>new Promise(resolve=>setImmediate(resolve));
async function deliver(state){await settle();const poll=waiting;waiting=null;assert(poll,'no Chat poll in flight');poll.resolve({ok:true,json:async()=>JSON.parse(JSON.stringify(state))});await settle();}
async function fail(){await settle();const poll=waiting;waiting=null;poll.reject(new Error('offline'));await settle();retry();await settle();}
const walk=node=>[node,...node.children.flatMap(walk)];
const change=e=>deliver({reset:false,cursor:e.seq,events:[e]});
const status=s=>deliver({reset:false,events:[],...s});
const resetState=s=>deliver({reset:true,...s});
const reset=(messages,active=messages.some(m=>m.status==='streaming'))=>resetState({cursor:10,messages,active,ephemeral:false});
async function runChatTests(){
 await window.drigaChat.open();
 const article=()=>get('chat-log').children.find(n=>n.tagName==='article');
 const body=()=>article().children[1];
 const escape=target=>{const e={key:'Escape',target,preventDefault(){this.defaultPrevented=true;}};if(target===get('chat-input'))target.handlers.keydown(e);documentHandlers.keydown(e);};
 assert(get('chat').classList.contains('reduced-motion'),'missing reduced motion class');
 motion.change({matches:false});
 assert(!get('chat').classList.contains('reduced-motion'));
 assert.equal(get('chat-stop').hidden,true,'idle Stop visible');
 await status({active:true});
 const placeholder=article();
 assert(placeholder.textContent.includes('Driga'),'missing Driga label');
 assert(placeholder.textContent.includes('thinking…'),'missing thinking placeholder');
 assert.equal(get('chat-stop').hidden,false);
 await change({seq:1,kind:'message',message:{id:'pending-reply',role:'assistant',status:'streaming',text:''}});
 assert.equal(article(),placeholder,'thinking placeholder lost identity on reply start');
 now=65000;timer();
 assert(placeholder.textContent.includes('1m 05s'),'elapsed time');
 assert.equal(get('chat-status').textContent,'','activity duplicated in status');
 await reset([{id:'answer',role:'assistant',status:'complete',text:'raw fallback',html:serverHTML.answer}]);
 assert.equal(body().innerHTML,serverHTML.answer,'server HTML not inserted');
 for(const tag of ['<h1>','<strong>','<em>','<code>','<table>','type="checkbox"'])assert(body().innerHTML.includes(tag),'missing server Markdown '+tag);
 assert(body().innerHTML.includes('data-chat-session="42"'),'citation button lost');
 get('chat-log').handlers.click({target:{closest(){return {dataset:{chatSession:'42'}};}}});
 assert.equal(document.lastEvent.type,'driga:select-session');
 assert.equal(document.lastEvent.detail.id,'42');
 assert(get('chat-context').textContent.includes('session: 42'),'citation did not select context');
 assert(!body().innerHTML.includes('<script>')&&!body().innerHTML.includes('href="javascript:'),'unsafe server HTML');
 await reset([{id:'stream',role:'assistant',status:'streaming',text:'',html:''}]);
 await reset([{id:'unsafe-stream',role:'assistant',status:'streaming',text:'<script>stream</script>',html:'<script>must ignore</script>'}]);
 assert.equal(body().innerHTML,'','stream reset trusted HTML');
 assert.equal(body().textContent,'<script>stream</script>');
 await reset([{id:'stream',role:'assistant',status:'streaming',text:'',html:''}]);
 const streamNode=article();
 await change({seq:11,kind:'delta',message_id:'stream',text:'**Hel',html:serverHTML.partial});
 assert.equal(body().innerHTML,'','streaming parsed HTML');
 assert.equal(body().textContent,'**Hel','streaming text missing');
 assert.equal(article(),streamNode,'placeholder replaced instead of updated');
 assert(get('chat-status').textContent.includes('writing…'));
 assert(walk(article()).some(n=>n.className==='chat-cursor'&&n.textContent==='▍'));
 await change({seq:11.5,kind:'message',message:{id:'tool',role:'tool',text:'mcp__driga__list_agents'}});
 assert(get('chat-status').textContent.includes('reading agents…'));
 await get('chat-stop').handlers.click();
 assert(requests.includes('/chat/api/stop'),'Stop did not call API');
 await change({seq:12,kind:'delta',message_id:'stream',text:'lo**',html:serverHTML.complete});
 await change({seq:12,kind:'delta',message_id:'stream',text:'duplicate',html:'replayed'});
 assert.equal(body().innerHTML,'','streaming parsed HTML after Stop');
 assert.equal(body().textContent,'**Hello**','duplicate replay changed text');
 await change({seq:13,kind:'message',message:{id:'stream',role:'assistant',status:'complete',text:'**Hello**',html:serverHTML.complete}});
 assert.equal(body().innerHTML,serverHTML.complete);
 await status({active:false});
 assert.equal(timer,null,'idle timer');
 assert.equal(get('chat-stop').hidden,true);
 assert(!walk(article()).some(n=>n.className==='chat-cursor'));
 const mark=walk(article()).find(n=>n.tagName==='img');
 assert(mark&&mark.src==='/static/brand/murmur.svg'&&mark.alt==='','decorative mark missing');
 await reset([{id:'plain',role:'user',status:'sent',text:'# **plain**\n<script>user text</script>',html:'<strong>must ignore</strong>'}]);
 assert.equal(body().innerHTML,'','user HTML field was trusted');
 assert.equal(body().textContent,'# **plain**\n<script>user text</script>');
 assert.equal(article().children[0].textContent,'You','user label');
 await reset([{id:'fallback',role:'assistant',status:'complete',text:'**plain fallback**'}]);
 assert.equal(body().textContent,'**plain fallback**','client still parses Markdown');
 assert(!get('chat-log').textContent.includes('Claude'),'provider used as message label');
 const input=get('chat-input');
 // Enter sends; Shift+Enter and an IME composition do not.
 {let submits=0;const form=get('chat-form');const before=form.requestSubmit;form.requestSubmit=()=>{submits++;};
  const press=extra=>{const e={key:'Enter',target:input,preventDefault(){this.defaultPrevented=true;},...extra};input.handlers.keydown(e);return e;};
  assert.equal(press({}).defaultPrevented,true);assert.equal(submits,1,'Enter did not send');
  assert(!press({shiftKey:true}).defaultPrevented);assert.equal(submits,1,'Shift+Enter sent');
  press({isComposing:true});assert.equal(submits,1,'IME Enter sent');
  form.requestSubmit=before;}
 input.value='draft';input.focus();escape(input);
 assert.equal(document.activeElement,null,'Escape did not blur draft');
 assert.equal(get('chat').hidden,false,'draft Escape closed panel');
 assert.equal(input.value,'draft');
 input.value='';input.focus();escape(input);
 assert.equal(get('chat').hidden,true,'empty Escape did not close');
 await window.drigaChat.open();
 const log=get('chat-log');log.scrollHeight=1000;log.clientHeight=100;log.scrollTop=0;log.handlers.scroll();
 await status({active:true});
 await change({seq:20,kind:'message',message:{id:'next',role:'assistant',status:'streaming',text:'new'}});
 assert.equal(log.scrollTop,0,'stream stole scroll position');
 log.scrollTop=900;log.handlers.scroll();
 await change({seq:21,kind:'delta',message_id:'next',text:' text'});
 assert.equal(log.scrollTop,1000,'stream did not follow bottom');
 await status({active:false});
 for(const state of ['stopped','error','interrupted']){
  await reset([{id:'ending',role:'assistant',status:'streaming',text:''}]);
  await change({seq:11,kind:'message',message:{id:'ending',role:'assistant',status:state,text:state}});
  assert.equal(timer,null,state+' left timer active');
  assert.equal(get('chat-stop').hidden,true,state+' left Stop visible');
 }
 await reset([{id:'connection',role:'assistant',status:'streaming',text:'partial'}]);
 await fail();
 assert.equal(timer,null,'connection error left timer active');
 assert(!walk(article()).some(n=>n.className==='chat-cursor'));
 assert.equal(get('chat-status').textContent,'Reconnecting…');
 const action={id:'action',tool:'Bash',title:'Bash · touch demo',input:{command:'touch demo'},directory:'/project',status:'pending'};
 await resetState({cursor:30,messages:[{id:'action',role:'action',status:'pending',text:'Bash'}],actions:[action],mode:'full auto',active:true});
 assert.equal(get('chat-mode-full')['aria-pressed'],'true');
 assert(get('chat-mode-full').classList.contains('danger'),'Full auto must be red');
 assert(get('chat-status').textContent.includes('waiting for your approval · Bash · 0s'),'pending status missing');
 const buttons=walk(get('chat-approval')).filter(n=>n.tagName==='button');
 assert.deepEqual(buttons.map(n=>n.textContent),['Allow','Deny']);
 assert(buttons.every(n=>n.type==='button'&&n['aria-label']),'action buttons lack native keyboard semantics');
 buttons[1].focus();
 await status({active:true,queued:0});
 assert.equal(document.activeElement,buttons[1],'heartbeat removed approval focus');
 await change({seq:30.2,kind:'message',message:{id:'tool-a',role:'tool',text:'read'}});
 assert.equal(document.activeElement,buttons[1],'unrelated update removed approval focus');
 const disclosure=walk(get('chat-log')).find(n=>n.tagName==='details');disclosure.open=true;
 await status({active:true,queued:0});
 assert.equal(disclosure.open,true,'heartbeat collapsed tools');
 await change({seq:30.3,kind:'message',message:{id:'tool-b',role:'tool',text:'read more'}});
 assert.equal(walk(get('chat-log')).find(n=>n.tagName==='details'),disclosure,'stream replaced disclosure');
 assert.equal(disclosure.open,true,'stream collapsed tools');
 await change({seq:30.4,kind:'message',message:{id:'card-reply',role:'assistant',status:'streaming',text:''}});
 await change({seq:30.5,kind:'delta',message_id:'card-reply',text:'token',html:'token'});
 assert.equal(document.activeElement,buttons[1],'text delta removed approval focus');
 assert.equal(walk(get('chat-approval')).filter(n=>n.tagName==='button').length,2,'text delta lost unchanged action');
 // A card must not take focus from typing, and an immediate Allow is ignored.
 {const input=get('chat-input');input.focus();await resetState({cursor:30.6,messages:[{id:'late',role:'action',status:'pending',text:'Bash'}],actions:[{...action,id:'late'}],mode:'confirm',active:true});
  assert.equal(document.activeElement,input,'new card stole typing focus');
  const late=walk(get('chat-approval')).filter(n=>n.tagName==='button');const sent=payloads.length;
  await late[0].handlers.click();assert.equal(payloads.filter(p=>p.path==='/chat/api/decide').length,payloads.slice(0,sent).filter(p=>p.path==='/chat/api/decide').length,'Allow accepted before it was armed');
  await resetState({cursor:30.7,messages:[{id:'action',role:'action',status:'pending',text:'Bash'}],actions:[action],mode:'full auto',active:true});}
 const armed=walk(get('chat-approval')).filter(n=>n.tagName==='button');
 now+=600;await armed[0].handlers.click();
 const decision=payloads.find(p=>p.path==='/chat/api/decide');
 assert(decision&&JSON.parse(decision.options.body).allow===true,'Allow did not decide');
 assert.deepEqual(decision.options.headers,{'Content-Type':'application/json'});
 await change({seq:31,kind:'action',action:{...action,status:'denied',decided_by:'you'}});
 assert(!walk(get('chat-approval')).some(n=>n.tagName==='button'),'decided card kept buttons');
 await get('chat-mode-auto').handlers.click();
 assert(payloads.some(p=>p.path==='/chat/api/mode'&&JSON.parse(p.options.body).mode==='auto'));
 await change({seq:32,kind:'mode',mode:'auto'});
 assert.equal(get('chat-mode-auto')['aria-pressed'],'true');
 assert(!get('chat-mode-full').classList.contains('danger'));
 const fullPosts=()=>payloads.filter(p=>p.path==='/chat/api/mode'&&JSON.parse(p.options.body).mode==='full auto').length;
 global.confirm=()=>false;await get('chat-mode-full').handlers.click();
 assert.equal(fullPosts(),0,'Full auto sent without confirmation');
 let asked='';global.confirm=text=>{asked=text;return true;};await get('chat-mode-full').handlers.click();
 assert.equal(fullPosts(),1);assert(asked.includes('30 minutes'),'confirmation must state the limit');
 await change({seq:33,kind:'mode',mode:'full auto',until:new Date(now+30*60000).toISOString()});
 assert.equal(get('chat-mode-left').hidden,false);assert.equal(get('chat-mode-left').textContent,'30m left');
 await change({seq:34,kind:'mode',mode:'auto'});
 assert.equal(get('chat-mode-left').hidden,true);
 await testPendingApprovalUX({change,reset,status,input,log,escape});
 location.pathname='/chat';input.value='';input.focus();escape(input);
 assert.equal(get('chat').hidden,true,'dedicated page Escape did not close');
}

async function testPendingApprovalUX({change,reset,status,input,log,escape}) {
 const approval=get('chat-approval');
 const card={id:'first',tool:'Bash',title:'Bash · check',input:{command:'echo > result',description:'Check output'},directory:'/project',status:'pending'};
 input.value='';
 await resetState({cursor:100,active:true,messages:[{id:'empty',role:'assistant',status:'streaming',text:''},{id:'first',role:'action',status:'pending'},{id:'q',role:'user',status:'queued',text:'later'}],actions:[card,{...card,id:'second'},{...card,id:'third'}]});
 // testApprovalPinnedDecodedAndFocused
 assert.equal(approval.hidden,false);
 assert(approval.textContent.includes('Approve? · Bash'));
 assert(approval.textContent.includes('echo > result'));
 assert(approval.textContent.includes('Check output'));
 assert(approval.textContent.includes('in /project'));
 assert(approval.textContent.includes('+2 more waiting'));
 assert(!log.textContent.includes('Approve?'),'pending card still in transcript');
 const buttons=walk(approval).filter(n=>n.tagName==='button');
 assert.equal(buttons.length,2,'only oldest approval must have controls');
 assert(!buttons.includes(document.activeElement),'a new card must not take focus');
 const details=walk(approval).find(n=>n.tagName==='details');
 assert(!details.open,'raw JSON starts expanded');
 assert(details.textContent.includes('"command"'),'raw JSON missing');
 // testApprovalElapsedNoSpinnerQueuedAndNoEmptyBubble
 now+=65000;timer();
 assert.equal(get('chat-status').textContent,'waiting for your approval · Bash · 1m 05s');
 assert(!walk(get('chat-status')).some(n=>n.className==='chat-spinner'),'approval spun');
 assert(log.textContent.includes('queued · sends after you answer the card'));
 assert(!walk(log).some(n=>n.className==='chat-who'&&n.textContent==='Driga'),'empty Driga bubble');
 details.open=true;await status({active:true});
 assert.equal(walk(approval).find(n=>n.tagName==='details'),details,'heartbeat replaced card');
 assert.equal(details.open,true);
 // testFinalAnswerFollowsDecisionAndPreservesReadingPosition
 log.scrollHeight=1200;log.clientHeight=100;log.scrollTop=1100;log.handlers.scroll();
 await change({seq:101,kind:'action',action:{...card,status:'denied',decided_by:'you',result:'Denied'}});
 await change({seq:102,kind:'delta',message_id:'empty',text:'AFTER-ACTION-ANSWER'});
 await change({seq:103,kind:'message',message:{id:'empty',role:'assistant',status:'complete',text:'AFTER-ACTION-ANSWER'}});
 assert.equal(log.scrollTop,1200,'final answer did not follow bottom');
 assert(log.textContent.indexOf('denied · you · Denied')<log.textContent.indexOf('AFTER-ACTION-ANSWER'),'reply before decision');
 assert.equal(approval.hidden,false,'next approval disappeared');
 assert(approval.textContent.includes('+1 more waiting'));
 log.scrollTop=0;log.handlers.scroll();
 await change({seq:104,kind:'message',message:{id:'empty',role:'assistant',status:'complete',text:'AFTER-ACTION-ANSWER more'}});
 assert.equal(log.scrollTop,0,'completion stole reading position');
 // testHistoricalDecisionsStayCompact
 await resetState({cursor:109,active:false,messages:[{id:'saved',role:'action',status:'done',text:'Bash · done · you · success'}]});
 assert.equal(log.textContent,'Bash · done · you · success');
 assert(!walk(log).some(n=>n.className==='chat-who'),'saved action became a Driga bubble');
 // testEditWriteAndDrigaReadableFields
 for(const [tool,input,want]of [['Edit',{file_path:'/a',old_string:'é',new_string:'new'},'/a · 2 → 3 bytes'],['Write',{file_path:'/b',content:'界'},'/b · 3 bytes'],['mcp__driga__kill',{},'Kill session #12']]){
  await resetState({cursor:110,active:true,messages:[],actions:[{...card,id:tool,tool,input,title:'Kill session #12'}]});
  assert(approval.textContent.includes(want),'readable fields missing for '+tool);
 }
 await resetState({cursor:120,active:false,messages:[{id:'q',role:'user',status:'queued',text:'later'}],actions:[]});
 assert.equal(approval.hidden,true);
 assert(log.textContent.includes('queued')&&!log.textContent.includes('sends after'),'idle queued label');
 // testSinglePlaceholderForEmptyStreamingMessages
 await resetState({cursor:125,active:true,actions:[],messages:[{id:'old-empty',role:'assistant',status:'streaming',text:''},{id:'current-empty',role:'assistant',status:'streaming',text:''}]});
 assert.equal(log.children.filter(n=>n.className==='chat-placeholder').length,1,'duplicate placeholder rows');
 now+=1000;timer();
 assert.equal(get('chat-status').textContent,'','placeholder also showed status activity');
 // testEscapeYieldsToFocusedFilterAndHelp
 const filter=new Element('input');filter.closest=selector=>selector==='.filter'?filter:filter;
 escape(filter);assert.equal(get('chat').hidden,false,'filter Escape closed Chat');
 get('help').open=true;escape(input);assert.equal(get('chat').hidden,false,'help Escape closed Chat');get('help').open=false;
 // testNoContextLabelWithoutChips
 for(const chip of [...get('chat-context').children])if(chip.tagName==='button')chip.handlers.click();
 assert.equal(get('chat-context').textContent,'');
 assert.equal(get('chat-context').hidden,true);
 console.log('Pending approval DOM regressions passed');
}

// Also runnable directly, without Go or a server. The Go test supplies actual
// goldmark fixtures; these minimal fixtures exercise the same DOM transitions.
if(require.main===module){
 global.serverHTML={answer:'<h1>Heading</h1><strong>bold</strong><em>italic</em><code>code</code><table></table><input type="checkbox"><button data-chat-session="42">session</button>',partial:'<p>**Hel</p>',complete:'<p><strong>Hello</strong></p>'};
 require('node:vm').runInThisContext(require('node:fs').readFileSync(require('node:path').join(__dirname,'../static/chat.js'),'utf8'));
 runChatTests().then(()=>console.log('Chat DOM tests passed')).catch(e=>{console.error(e);process.exitCode=1;});
}
