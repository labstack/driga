// Rendered DOM regressions. Run with DRIGA_PLAYWRIGHT_MODULE pointing at an
// installed Playwright module. All fixtures, profiles and screenshots stay local.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {chromium}=require(process.env.DRIGA_PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(__dirname,'../../..'),scratch=path.join(root,'.chat-browser-test');
fs.mkdirSync(scratch,{recursive:true});
process.env.TMPDIR=scratch;
const html=fs.readFileSync(path.join(root,'internal/web/templates/chat.html'),'utf8').replace(/{{.*?}}/g,'');
let css=fs.readFileSync(path.join(root,'internal/web/static/app.css'),'utf8')+'\n'+fs.readFileSync(path.join(root,'internal/web/static/chat.css'),'utf8');
css=css.replace(/url\("\/static\/([^"\)]+)"\)/g,(_,file)=>`url("data:font/woff2;base64,${fs.readFileSync(path.join(root,'internal/web/static',file)).toString('base64')}")`);
const tokens=[...fs.readFileSync(path.join(root,'internal/view/theme.go'),'utf8').matchAll(/\{"([^" ]+)", "(#[A-Fa-f0-9]+)", "(#[A-Fa-f0-9]+)"\}/g)];
const palette=theme=>':root{'+tokens.map(([,name,light,dark])=>`--${name}:${theme==='latte'?light:dark};`).join('')+'}';
const chatJS=fs.readFileSync(path.join(root,'internal/web/static/chat.js'),'utf8');
(async()=>{
 let browser;
 try{
  browser=await chromium.launch({headless:true,executablePath:process.env.DRIGA_CHROMIUM_PATH||undefined,env:{...process.env,HOME:scratch,XDG_CONFIG_HOME:scratch,TMPDIR:scratch},args:['--disable-background-networking','--disable-component-update','--disable-sync','--no-first-run']});
  const page=await browser.newPage();await page.route('**/*',route=>route.abort());
  const errors=[];page.on('pageerror',error=>errors.push(error.message));
  for(const theme of ['latte','mocha'])for(const [width,height]of [[375,600],[375,667],[768,700],[1280,900]]){
   await page.setViewportSize({width,height});
   await page.setContent(`<style>${palette(theme)}${css}</style><div class="c chat-page chat-open">${html}</div>`);
   await page.evaluate(()=>{
    document.getElementById('chat').hidden=false;
    document.getElementById('chat-history').innerHTML='<option>Driga · 10/23/2026, 5:00:00 AM</option>';
    const chip=document.createElement('button');chip.className='chat-chip';chip.textContent='project: /synthetic/'+ 'long-directory-'.repeat(16);document.getElementById('chat-context').append(chip);
    document.getElementById('chat-log').innerHTML='<article class="chat-message assistant"><div class="chat-body"><pre><code>'+ 'LONG-CODE'.repeat(80)+'</code></pre></div></article>';
   });
   await page.evaluate(()=>document.fonts.ready);
   const result=await page.evaluate(()=>{
    const box=id=>document.getElementById(id).getBoundingClientRect();
    const pre=document.querySelector('pre'),hint=document.querySelector('#chat-form>.chat-note').getBoundingClientRect(),chip=document.querySelector('.chat-chip').getBoundingClientRect();
    const input=document.getElementById('chat-input'),style=getComputedStyle(input,'::placeholder');
    const lum=color=>{const rgb=color.match(/\d+/g).slice(0,3).map(Number).map(n=>{n/=255;return n<=.04045?n/12.92:((n+.055)/1.055)**2.4;});return rgb[0]*.2126+rgb[1]*.7152+rgb[2]*.0722;};
    const a=lum(style.color),b=lum(getComputedStyle(input).backgroundColor);
    return {panel:box('chat').bottom,form:box('chat-form').bottom,send:box('chat-send').bottom,hint:hint.bottom,chipWidth:chip.width,chipHeight:chip.height,panelWidth:box('chat').width,contrast:(Math.max(a,b)+.05)/(Math.min(a,b)+.05),codeScroll:pre.scrollWidth>pre.clientWidth,pageWidth:document.documentElement.scrollWidth,viewport:innerWidth};
   });
   assert(result.form<=result.panel+1&&result.send<=result.panel+1&&result.hint<=result.panel+1,'testLongContextComposerVisible '+JSON.stringify({theme,width,height,...result}));
   assert(result.chipWidth<result.panelWidth&&result.chipHeight>20,'testContextChipWraps '+JSON.stringify(result));
   assert(result.contrast>=4.5,'testPlaceholderAAContrast '+JSON.stringify(result));
   assert(result.codeScroll,'testCodeBlockHorizontalScroll');
   assert(result.pageWidth<=width,'testCodeBlockNoPageScroll '+JSON.stringify(result));
  }
  // Real DOM event propagation must leave Escape available to the app handler.
  await page.evaluate(()=>{
   window.fetch=async()=>({ok:true,json:async()=>[]});window.EventSource=class{constructor(){window.chatSource=this;this.handlers={};}addEventListener(k,f){this.handlers[k]=f;}close(){}};
   const meta=document.createElement('meta');meta.name='chat-csrf';meta.content='fixture';document.head.append(meta);
   const help=document.createElement('dialog');help.id='help';document.body.append(help);
   const filter=document.createElement('label');filter.className='filter';filter.innerHTML='<input id="filter-test">';document.body.append(filter);
  });
  await page.addScriptTag({content:chatJS});await page.evaluate(()=>window.drigaChat.open());
  await page.locator('#filter-test').focus();await page.keyboard.press('Escape');
  assert.equal(await page.locator('#chat').evaluate(e=>e.hidden),false,'testEscapeFocusedFilter');
  await page.evaluate(()=>document.getElementById('help').showModal());await page.keyboard.press('Escape');
  assert.equal(await page.locator('#chat').evaluate(e=>e.hidden),false,'testEscapeHelp');
  for(const [width,height]of [[375,600],[1280,900]]){
   await page.setViewportSize({width,height});
   await page.evaluate(()=>{
    const card={id:'card',tool:'Bash',title:'Bash · check',input:{command:'echo > result',description:'Check output'},directory:'/project',status:'pending'};
    window.chatSource.handlers.reset({data:JSON.stringify({cursor:10,active:true,actions:[card],messages:[{id:'answer',role:'assistant',text:'',status:'streaming'},{id:'card',role:'action',status:'pending'}]})});
   });
   assert.equal(await page.locator('#chat-approval button').first().evaluate(e=>document.activeElement===e),true,'testAllowFocused');
   assert.equal(await page.locator('#chat-log .assistant').count(),0,'testNoEmptyAssistant');
   assert.equal(await page.locator('#chat-status .chat-spinner').count(),0,'testApprovalNoSpinner');
   const layout=await page.evaluate(()=>{
    const rect=id=>document.getElementById(id).getBoundingClientRect();
    return {panel:rect('chat').bottom,approval:rect('chat-approval').bottom,form:rect('chat-form').top,send:rect('chat-send').bottom};
   });
   assert(layout.approval<=layout.form+1,'testApprovalAboveComposer '+JSON.stringify(layout));
   assert(layout.send<=layout.panel+1,'testApprovalComposerVisible '+JSON.stringify(layout));
  }
  if(process.env.DRIGA_CHAT_SCREENSHOT)await page.screenshot({path:process.env.DRIGA_CHAT_SCREENSHOT});
  assert.deepEqual(errors,[]);
  console.log('testLongContextComposerVisible, testContextChipWraps, testPlaceholderAAContrast, testCodeBlockHorizontalScroll, testCodeBlockNoPageScroll, testEscapeFocusedFilter, testEscapeHelp passed (8 viewport/theme cases)');
 }finally{if(browser)await browser.close();fs.rmSync(scratch,{recursive:true,force:true});}
})().catch(error=>{console.error(error);process.exitCode=1;});
