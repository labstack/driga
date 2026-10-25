(() => {
 const parse = globalThis.DrigaConsole.parseCommand;
 const assert = (condition, message) => { if (!condition) throw new Error(message); };
 const cases = [
  ["home", "/?machine=all"], ["agents", "/agents?machine=all"],
  ["agents --all", "/agents?all=1&machine=all"],
  ["agents --project driga", "/agents?machine=all&project=driga"],
  ['agents --project "Fixture project" --all --state waiting', "/agents?all=1&machine=all&project=Fixture+project&state=waiting"],
  ["projects show Fixture project", "/projects?machine=all&project=Fixture+project"],
  ['projects show "a&b?<>"', "/projects?machine=all&project=a%26b%3F%3C%3E"],
  ["projects", "/projects?machine=all"], ["projects list", "/projects?machine=all"], ["agents show 12", "/agents/12?machine=all"], ["inbox", "/inbox?machine=all"],
  ["orphans", "/orphans?machine=all"], ["machines", "/machines?machine=all"],
  ["cleanup", "/cleanup?machine=all"], ["cleanup --why", "/cleanup?machine=all&why=1"],
  ["activity", "/activity?machine=all"]
 ];
 for (const [command,url] of cases) assert(parse(command).url === url, command + " => " + JSON.stringify(parse(command)));
 for (const state of ["working", "waiting", "stalled", "idle", "finished", "ended", "dead", "unknown"]) {
  assert(parse("agents --state " + state).url === "/agents?machine=all&state=" + encodeURIComponent(state), "state " + state);
 }
 for (const theme of ["light", "dark", "system"]) assert(parse("theme " + theme).error, "theme belongs to the app: " + theme);
 assert(parse("agents", "peer&x").url === "/agents?machine=peer%26x", "scope is encoded");
 for (const command of ["inbox ack --all", "agents jump 12", "agents kill 12", "inbox jump item", "projects summarize app", "orphans kill 42", "cleanup remove", "hooks install"]) {
  const result=parse(command); assert(result.error === "read-only in browser · use the CLI" && !result.url, command + " must not run");
 }
 for (const command of ["", "unknown", "agents --state nope", "agents --state", "agents --all --all", "agents --project", "projects show", "theme mocha", "inbox trailing", "cleanup --why apply", 'projects show "unterminated', "agents; kill 42", "activity --window 6h", "x".repeat(501)]) {
  const result=parse(command); assert(result.error && !result.url, "invalid " + command);
 }
})();

if (DrigaConsole.parseCommand("flow 42").url !== "/flow?id=42&machine=all") throw Error("flow command");
if (DrigaConsole.parseCommand("flow").url !== "/flow?machine=all") throw Error("default flow command");

(() => {
 const text=globalThis.DrigaConsole.connectionText;
 if(text(false)!=="○ reconnecting…")throw new Error("retry status");
 if(text(true)!=="● live")throw new Error("live status");
})();
