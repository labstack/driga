const assert=require("node:assert/strict");
const {duration,nextIndex}=globalThis.DrigaBoard;
assert.equal(duration(-1000),"0s");assert.equal(duration(3660000),"1h 1m");
assert.equal(nextIndex(2,"ArrowUp",4),1);assert.equal(nextIndex(0,"ArrowUp",4),0);assert.equal(nextIndex(0,"End",4),3);assert.equal(nextIndex(3,"ArrowDown",4),3);
console.log("Flow keyboard invariants passed");
