(() => {
 const parse = globalThis.DrigaConsole.parseCommand;
 const assert = (condition, message) => { if (!condition) throw new Error(message); };
 const cases = [
  ["home", "/?machine=all"], ["agents", "/agents?machine=all"],
  ["agents --all", "/agents?all=1&machine=all"],
  ["agents --project driga", "/agents?machine=all&project=driga"],
  ['agents --project "Fixture project" --all --state waiting', "/agents?all=1&machine=all&project=Fixture+project&state=waiting"],
  ["where Fixture project", "/projects?machine=all&project=Fixture+project"],
  ['where "a&b?<>"', "/projects?machine=all&project=a%26b%3F%3C%3E"],
  ["projects", "/projects?machine=all"], ["inbox", "/inbox?machine=all"],
  ["orphans", "/orphans?machine=all"], ["machines", "/machines?machine=all"],
  ["cleanup", "/cleanup?machine=all"], ["cleanup --why", "/cleanup?machine=all&why=1"],
  ["activity", "/activity?machine=all"]
 ];
 for (const [command,url] of cases) assert(parse(command).url === url, command + " => " + JSON.stringify(parse(command)));
 for (const state of ["working", "waiting", "stalled", "idle", "finished", "ended", "dead", "unknown"]) {
  assert(parse("agents --state " + state).url === "/agents?machine=all&state=" + encodeURIComponent(state), "state " + state);
 }
 for (const theme of ["mocha", "latte", "system"]) assert(parse("theme " + theme).theme === theme, "theme " + theme);
 assert(parse("agents", "peer&x").url === "/agents?machine=peer%26x", "scope is encoded");
 for (const command of ["ack 1", "inbox ack --all", "kill 42", "orphans kill 42", "cleanup apply", "cleanup --apply", "hooks install"]) {
  const result=parse(command); assert(result.error === "read-only in browser · use the CLI" && !result.url && !result.theme, command + " must not run");
 }
 for (const command of ["", "unknown", "agents --state nope", "agents --state", "agents --all --all", "agents --project", "where", "theme dark", "inbox trailing", "cleanup --why apply", 'where "unterminated', "agents; kill 42", "activity --window 6h", "x".repeat(501)]) {
  const result=parse(command); assert(result.error && !result.url && !result.theme, "invalid " + command);
 }
})();

if (DrigaConsole.parseCommand("flow 42").url !== "/flow?id=42&machine=all") throw Error("flow command");
if (DrigaConsole.parseCommand("flow").url !== "/flow?machine=all") throw Error("default flow command");

var flowEvent={machine_id:"local",adapter:"claude",external_id:"lead",key:"event-1"};
if (!DrigaConsole.familyEvent(flowEvent,"local",["claude:lead"],new Set())) throw Error("family pulse missing");
if (DrigaConsole.familyEvent(flowEvent,"peer",["claude:lead"],new Set()) || DrigaConsole.familyEvent(flowEvent,"local",["claude:other"],new Set()) || DrigaConsole.familyEvent(flowEvent,"local",["claude:lead"],new Set(["event-1"]))) throw Error("spurious family pulse");

var geometry=DrigaConsole.flowWireGeometry({right:100,y:30},{left:140,right:240,y:30},[{key:"one",left:300,y:20},{key:"two",left:300,y:80}]);
if (geometry.trunk !== "M 240 30 H 270 M 270 20 V 80") throw Error("shared trunk geometry: "+geometry.trunk);
if (geometry.branches[0].d !== "M 270 20 H 300" || geometry.branches[1].d !== "M 270 80 H 300") throw Error("short horizontal branches");
if (geometry.routes["two"] !== "M 240 30 H 270 V 80 H 300") throw Error("pulse must travel trunk then matching branch");
