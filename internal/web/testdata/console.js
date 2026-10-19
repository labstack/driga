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
 for (const state of ["working", "waiting", "stalled", "idle", "finished", "dead", "unknown"]) {
  assert(parse("agents --state " + state).url === "/agents?machine=all&state=" + encodeURIComponent(state), "state " + state);
 }
 for (const theme of ["phosphor", "paper", "system"]) assert(parse("theme " + theme).theme === theme, "theme " + theme);
 assert(parse("agents", "peer&x").url === "/agents?machine=peer%26x", "scope is encoded");
 for (const command of ["ack 1", "inbox ack --all", "kill 42", "orphans kill 42", "cleanup apply", "cleanup --apply", "hooks install"]) {
  const result=parse(command); assert(result.error === "read-only in browser · use the CLI" && !result.url && !result.theme, command + " must not run");
 }
 for (const command of ["", "unknown", "agents --state nope", "agents --state", "agents --all --all", "agents --project", "where", "theme dark", "inbox trailing", "cleanup --why apply", 'where "unterminated', "agents; kill 42", "activity --window 6h", "x".repeat(501)]) {
  const result=parse(command); assert(result.error && !result.url && !result.theme, "invalid " + command);
 }
})();
