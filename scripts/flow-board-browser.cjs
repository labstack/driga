// Browser QA uses only synthetic fixtures and a loopback server.
// Run fixture export first: FLOW_SHOTS_DIR=... scripts/isolated.sh go test
// ./internal/web -run TestBoardFixturePages. Set PLAYWRIGHT_MODULE when needed.
const fs=require("node:fs"),path=require("node:path"),http=require("node:http"),assert=require("node:assert/strict");
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||"playwright");
const repo=path.resolve(__dirname,".."),out=process.env.FLOW_SHOTS_DIR;
if(!out)throw Error("FLOW_SHOTS_DIR must point to synthetic fixture exports");
let kind="busy",revision=0;const streams=new Set(),requests=[];
const server=http.createServer((req,res)=>{
 const u=new URL(req.url,"http://localhost");
 if(req.method==="POST"){let body="";req.on("data",d=>body+=d);req.on("end",()=>{requests.push({path:u.pathname,body:JSON.parse(body)});res.setHeader("Content-Type","application/json");res.end("{\"ok\":true}");});return;}
 if(u.pathname==="/events"){res.writeHead(200,{"Content-Type":"text/event-stream"});res.write(": fixture\n\n");streams.add(res);req.on("close",()=>streams.delete(res));return;}
 const staticPath=u.pathname==="/static/theme.css"?path.join(out,"fixtures/theme.css"):u.pathname.startsWith("/static/")?path.join(repo,"internal/web",u.pathname):null;
 if(staticPath){if(!fs.existsSync(staticPath)){res.writeHead(404);res.end();return;}res.setHeader("Content-Type",({".css":"text/css",".js":"application/javascript",".woff2":"font/woff2",".svg":"image/svg+xml",".png":"image/png"})[path.extname(staticPath)]||"application/octet-stream");res.end(fs.readFileSync(staticPath));return;}
 let html=fs.readFileSync(path.join(out,"fixtures",kind+".html"),"utf8").replace("data-flow-board",`data-revision="${revision}" data-flow-board`);
 if(revision)html=html.replaceAll("Add board view","Add board view refreshed");
 if(req.headers["hx-request"])html=html.match(/<main[^>]*>([\s\S]*)<\/main>/)[1];
 res.setHeader("Content-Type","text/html");res.setHeader("Content-Security-Policy","default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'");res.end(html);
});
(async()=>{let browser;try{
 await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve));
 browser=await chromium.launch({headless:true,...(process.env.CHROMIUM_PATH?{executablePath:process.env.CHROMIUM_PATH}:{}),args:["--disable-background-networking","--disable-component-update","--disable-sync","--no-first-run"]});
 const page=await browser.newPage({viewport:{width:1600,height:1000},serviceWorkers:"block"});const errors=[];page.on("pageerror",e=>errors.push(e.message));const base="http://127.0.0.1:"+server.address().port;
 await page.goto(base+"/flow");await page.locator(".fb-node.selected").waitFor();
 await page.locator('[data-flow-view="timeline"]').click();const rows=page.locator('[data-flow-surface="timeline"] [role="treeitem"]');await rows.nth(7).focus();await page.keyboard.press("Enter");await page.keyboard.press("ArrowDown");
 const key=await page.evaluate(()=>document.activeElement.dataset.boardKey);assert(key);
 await page.evaluate(()=>{window.focusedBefore=document.activeElement;document.querySelector(".fb-timeline").scrollTop=160;});
 revision++;for(const res of streams)res.write("event: change\ndata: refresh\n\n");await page.waitForFunction(()=>document.querySelector("[data-flow-board]").dataset.revision==="1");
 assert.equal(await page.locator("[data-flow-board]").count(),1,"refresh duplicated Flow");assert.equal(await page.evaluate(()=>document.activeElement.dataset.boardKey),key,"refresh lost keyboard focus");assert(await page.evaluate(()=>document.activeElement===window.focusedBefore),"morph replaced focused row");assert.equal(await page.locator('[data-flow-surface="timeline"] .selected').getAttribute("data-board-key"),key);assert.equal(await page.locator(".fb-timeline").evaluate(n=>n.scrollTop),160);
 await page.keyboard.press("/");await page.keyboard.type("retry");revision++;for(const res of streams)res.write("event: change\ndata: refresh\n\n");await page.waitForFunction(()=>document.querySelector("[data-flow-board]").dataset.revision==="2");assert.equal(await page.locator("[data-board-search]").inputValue(),"retry");assert(await page.locator("[data-board-search]").evaluate(n=>n===document.activeElement));
 await page.locator("[data-board-search]").fill("");await page.locator('[data-flow-view="map"]').click();
 await page.locator("[data-answer]").first().waitFor();await page.waitForTimeout(1100);await page.locator('[data-answer="deny"]').first().click();assert(requests.some(r=>r.path==="/chat/api/prompt-answer"&&!r.body.allow));await page.waitForTimeout(200);assert.equal(await page.locator("[data-flow-board]").count(),1,"prompt refresh duplicated Flow");
 for(const width of [375,768,1280,1600])for(const theme of ["mocha","latte"]){await page.setViewportSize({width,height:1000});await page.evaluate(t=>{localStorage.setItem("driga-theme",t);document.documentElement.dataset.theme=t;},theme);assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),"horizontal page overflow");assert.equal(await page.locator("[data-flow-board]").count(),1,"duplicate board before screenshot");assert.equal(await page.locator("nav.wins").count(),1,"duplicate navigation before screenshot");await page.screenshot({path:path.join(out,`map-${width}-${theme}.png`),fullPage:false});}
 await page.setViewportSize({width:1600,height:1000});await page.locator('[data-flow-view="timeline"]').click();await page.screenshot({path:path.join(out,"timeline-1600.png"),fullPage:false});
 for(const state of ["quiet","empty"]){kind=state;await page.goto(base+"/flow");await page.screenshot({path:path.join(out,state+"-1600.png"),fullPage:false});}
 await page.emulateMedia({reducedMotion:"reduce"});assert.equal(await page.locator(".flow-board").evaluate(n=>getComputedStyle(n).animationName),"none");assert.deepEqual(errors,[]);fs.writeFileSync(path.join(out,"browser-results.json"),JSON.stringify({focus:true,selection:true,scroll:true,search:true,prompt:true,errors},null,2));console.log("Flow browser QA passed");
}finally{if(browser)await browser.close();for(const res of streams)res.end();await new Promise(resolve=>server.close(resolve));}})().catch(e=>{console.error(e);process.exitCode=1;});
