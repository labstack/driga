(async () => {
  const assert = (ok, message) => { if (!ok) throw Error(message); };
  let rows, selected = null, copied = null, calls = 0, scrollCalls = 0;
  const older = {hidden:false, textContent:""};
  const pane = {scrollLeft:3, scrollTop:250, querySelector:()=>null};
  const doc = {
    querySelectorAll: selector => selector === "[data-inbox-key]" ? rows : selector === "[data-inbox-recent]" ? rows.filter(r => r.recent) : selector === "[data-pane]" ? [pane] : [],
    querySelector: selector => selector === "[data-inbox-older]" ? older : null,
    createRange: () => ({selectNodeContents: el => { selected = el; }}),
    getSelection: () => ({removeAllRanges() {}, addRange() {}}),
    activeElement: null
  };
  const viewport = {scrollX:0, scrollY:640, scrollTo(x,y) { this.scrollX=x; this.scrollY=y; scrollCalls++; }};
  const controller = DrigaConsole.createInboxController(doc, viewport);
  const makeRows = () => Array.from({length:177}, (_,i) => ({dataset:{inboxKey:JSON.stringify([i%2 ? "peer" : "local", i===4 || i===5 ? "shared-id" : String(i)])}, recent:i>=70, hidden:i>=120, open:false, scrollTop:0, hasAttribute(name) { return name==="data-inbox-recent" && this.recent; }, closest() { return pane; }}));
  rows=makeRows(); controller.restore();
  assert(rows.filter(r=>!r.hidden).length===120, "all 70 waits + first 50 recent");
  rows[4].open=true; rows[125].open=true;
  controller.showOlder();
  assert(rows.filter(r=>!r.hidden).length===170 && !older.hidden, "next 50 recent");
  controller.capture(); rows=makeRows(); viewport.scrollY=0; pane.scrollTop=0; controller.restore();
  assert(rows[4].open && rows[125].open && !rows[5].open, "expanded identities survive refresh");
  assert(rows.filter(r=>!r.hidden).length===170, "pagination survives refresh");
  assert(viewport.scrollY===640 && scrollCalls===1 && pane.scrollTop===250, "page and pane scroll restored after swap");
  assert(!rows[5].open, "same inbox ID on another machine stays collapsed");
  rows[4].open=false; controller.capture(); rows=makeRows(); controller.restore();
  assert(!rows[4].open && rows[125].open, "closed row stays closed");
  controller.showOlder(); assert(rows.every(r=>!r.hidden) && older.hidden, "final partial batch hides show older");
  const command={textContent:"agent --resume 'full command'", focus() { this.focused=true; }};
  const status={textContent:""};
  const button={closest:()=>({querySelector:s=>s==="[data-inbox-command]" ? command : status})};
  const pending = controller.copy(button, {writeText(text) { calls++; copied=text; return Promise.resolve(); }});
  assert(calls===1, "clipboard invoked synchronously in user activation");
  await pending;
  assert(calls===1 && copied===command.textContent && !selected && status.textContent==="Copied", "clipboard success");
  await controller.copy(button, {writeText() { return Promise.reject(Error("denied")); }});
  assert(selected===command && command.focused && status.textContent.includes("select"), "clipboard rejection selects full command");
  selected=null; await controller.copy(button, undefined);
  assert(selected===command, "unavailable clipboard selects command");
  selected=null; await controller.copy(button, {writeText() { throw Error("blocked"); }});
  assert(selected===command, "synchronous clipboard failure selects command");
  rows=makeRows();
  const arriving = DrigaConsole.createInboxController(doc, viewport);
  arriving.restore(); rows[119].open=true; arriving.capture();
  rows=makeRows();
  const newRow=makeRows()[70]; newRow.dataset.inboxKey=JSON.stringify(["local","just-arrived"]);
  rows.splice(70,0,newRow); arriving.restore();
  assert(rows[120].open && !rows[120].hidden, "new completion must not hide an expanded row at the cap boundary");
})().catch(error => { console.error(error); process.exitCode=1; });
