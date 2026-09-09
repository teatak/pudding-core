import assert from "node:assert/strict";
import {test} from "node:test";
import {inputPanelRemaining,inputPanelCountdown,inputWaitCountdown} from "../src/components/transcript/inputFlowTiming.ts";

test("model wait 10s does not shorten the independent 60s panel idle timer",()=>{
 const start=Date.now(),deadline=new Date(start+10_000).toISOString();
 assert.equal(inputWaitCountdown(deadline,10,start),null);
 assert.equal(inputWaitCountdown(deadline,10,start+5_000),5);
 assert.equal(inputWaitCountdown(deadline,10,start+10_000),null);
 assert.equal(inputPanelRemaining(start,start+10_000),50);
 assert.equal(inputPanelCountdown(start,start+10_000),null);
 assert.equal(inputPanelCountdown(start,start+49_000),null);
 assert.equal(inputPanelCountdown(start,start+50_000),10);
 assert.equal(inputPanelRemaining(start,start+60_000),0);
});
test("interaction renews panel and hides countdown; model async has none",()=>{
 const start=Date.now(),touched=start+55_000;
 assert.equal(inputPanelCountdown(start,touched),5);
 assert.equal(inputPanelCountdown(touched,touched),null);
 assert.equal(inputPanelRemaining(touched,touched),60);
 assert.equal(inputWaitCountdown(undefined,0,touched),null);
 assert.equal(inputWaitCountdown(new Date(touched+60_000).toISOString(),60,touched),null);
});
