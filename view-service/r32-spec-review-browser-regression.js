// Production Specs HTML in Chromium, stateful HTTP fixture. Backend security is
// separately tested by real Hub/WS/PG/SQLite tests; this suite checks UI behavior.
const assert=require('node:assert/strict'),fs=require('fs'),http=require('http'),path=require('path');
const {chromium}=require('playwright');const root=path.resolve(__dirname,'..');
const src=fs.readFileSync(path.join(root,'internal/m8/console_specs_page.go'),'utf8'),begin='const consoleSpecsPageHTML = `',end='`\n\nfunc (h *Hub) HandleConsoleSpecsPage';
let html=src.slice(src.indexOf(begin)+begin.length,src.indexOf(end,src.indexOf(begin))).replace('<script id=dw-md-core></script>',()=>'<script>'+fs.readFileSync(path.join(__dirname,'md-render-core.js'),'utf8')+'</script>');
let spec={id:'s',title:'R8 spec',state:'editing',current_revision:1,can_edit:true,can_delete:true,can_split:false,current_editor_short_name:'甲'};
let revision={content_markdown:'# Requirement\nowner quote',content_sha256:'a'.repeat(64),acceptance_json:JSON.stringify({criteria:[{id:'A1',text:'works',owner_ref:'owner quote',source:'owner',cases:['case1']}]}),input_anchors_json:JSON.stringify([{kind:'owner_message',ref:'owner quote'}])};
let requests=[],gate=null,number=0;const calls=[];let oldRevision='';
const json=(res,v,status=200)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(v));};
const server=http.createServer(async(req,res)=>{
 const u=new URL(req.url,'http://fixture');let text='';for await(const c of req)text+=c;const body=text?JSON.parse(text):{};calls.push({path:u.pathname,method:req.method,body});
 if(u.pathname==='/console/specs'){res.writeHead(200,{'Content-Type':'text/html'});return res.end(html)}
 if(u.pathname.startsWith('/console/spec-assets/')){const file=path.basename(u.pathname);if(!['easymde.min.js','easymde.min.css'].includes(file))return json(res,{},404);return res.end(fs.readFileSync(path.join(root,'internal/m8/static/easymde',file)))}
 if(u.pathname==='/favicon.ico'){res.writeHead(204);return res.end()}
 if(u.pathname==='/api/console/specs')return json(res,{role:'owner',principal:'owner-a',specs:[spec],page:1,total_pages:1,agents:[]});
 if(u.pathname==='/api/console/spec-editors')return json(res,{editors:[{value:'乙',display_name:'乙'}]});
 if(u.pathname==='/api/console/specs/s'&&req.method==='GET')return json(res,{spec,revision,gate_review:gate,tasks:[],dependency_candidates:[]});
 if(u.pathname==='/api/console/specs/s/agent-review'&&req.method==='GET')return json(res,{requests,agents:[{id:'own-agent',session_name:'Agent.甲'}]});
 if((u.pathname==='/api/console/specs/s/agent-review'||u.pathname==='/api/console/specs/s/agent-criteria')&&req.method==='POST'){
  const q={request_id:'specreview_'+(++number),spec_id:'s',kind:u.pathname.endsWith('agent-review')?'review':'criteria',status:'pending',revision:spec.current_revision,content_sha256:revision.content_sha256};requests.unshift(q);if(q.kind==='review')spec.state='review';return json(res,q,202)
 }
 if(u.pathname==='/fixture/reply'){
  const q=requests[0];if(q.status==='cancelled')return json(res,{error:'cancelled'},409);
  if(q.kind==='review'){q.status='completed';gate={review_agent_id:'own-agent',issues_json:JSON.stringify([{id:'I1',text:'Check owner quote',response:'',outcome:'pending'}])}}
  else{q.status='awaiting_confirmation';q.result={criteria:[{id:'A1',text:'new criterion',owner_ref:'owner quote',cases:['new case']}]}}
  return json(res,{})
 }
 if(u.pathname.endsWith('/confirm')){const q=requests[0];assert.equal(q.status,'awaiting_confirmation');oldRevision=revision.acceptance_json;revision.acceptance_json=JSON.stringify(q.result);spec.current_revision++;spec.state='editing';q.status='completed';gate=null;return json(res,{revision:spec.current_revision})}
 if(u.pathname.endsWith('/cancel')){requests[0].status='cancelled';res.writeHead(204);return res.end()}
 if(u.pathname==='/api/console/specs/s/workflow/review'){gate={review_agent_id:body.review_agent_id,issues_json:JSON.stringify(body.issues)};res.writeHead(204);return res.end()}
 if(u.pathname==='/api/console/specs/s/workflow/transfer'){assert.equal(body.cancel_request_id,requests[0].request_id);assert.equal(body.expected_revision,spec.current_revision);requests[0].status='cancelled';spec.can_edit=false;spec.current_editor_short_name='乙';spec.state=body.state;res.writeHead(204);return res.end()}
 if(u.pathname==='/api/console/specs/s/workflow/finalize'){const issues=JSON.parse(gate.issues_json);assert.ok(issues.every(i=>i.outcome!=='pending'&&i.response));spec.state='final';spec.can_edit=false;res.writeHead(204);return res.end()}
 return json(res,{error:'unhandled '+u.pathname},404);
});
async function main(){
 await new Promise(ok=>server.listen(0,'127.0.0.1',ok));const base='http://127.0.0.1:'+server.address().port;
 const browser=await chromium.launch({channel:process.env.PW_CHANNEL||'chrome',headless:true});const page=await browser.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));page.setDefaultTimeout(5000);
 let checks=0;const check=(v,msg)=>{assert.ok(v,msg);checks++};
 try{
 await page.goto(base+'/console/specs');await page.locator('.spec-title').click();await page.locator('#spec-review-assist').waitFor({state:'visible'});
 // Visibility precedes the asynchronous authorized-Agent response; wait for data, then keep the exact count assertion.
 await page.waitForFunction(()=>document.querySelector('#spec-review-agent').options.length>0);
 check(await page.locator('#spec-review-agent option').count()===1,'R8_AGENT_PICKER_WRONG');
 await page.click('#request-spec-review');await page.waitForFunction(()=>!!specAgentActive);check(await page.locator('#save-spec').isDisabled(),'R8_UI_EDIT_NOT_LOCKED');check(spec.current_revision===1,'R8_REVIEW_CHANGED_REVISION');
 await page.request.post(base+'/fixture/reply');await page.evaluate(()=>loadSpecAgentAssist());await page.waitForFunction(()=>document.querySelectorAll('.review-issue').length===1);
 check(await page.locator('#review-agent-id').inputValue()==='own-agent','R8_ACTUAL_REVIEWER_NOT_SHOWN');check(await page.locator('#review-agent-id').evaluate(e=>e.readOnly),'R8_UI_PROVENANCE_EDITABLE');
 await page.selectOption('.review-issue-outcome','false_positive');await page.fill('.review-issue-response','checked by human');await page.click('#save-gate-review');await page.waitForFunction(()=>document.getElementById('workflow-status').textContent==='已完成');
 check(JSON.parse(gate.issues_json)[0].outcome==='false_positive','R8_ANNOTATION_NOT_SAVED');
 await page.click('#request-spec-criteria');await page.waitForFunction(()=>specAgentActive&&specAgentActive.kind==='criteria');await page.request.post(base+'/fixture/reply');await page.evaluate(()=>loadSpecAgentAssist());await page.locator('#confirm-spec-criteria').waitFor({state:'visible'});
 check(spec.current_revision===1,'R8_UNCONFIRMED_CRITERIA_OVERWRITE');check(await page.locator('#spec-agent-candidate').innerText().then(t=>t.includes('new case')),'R8_CANDIDATE_NOT_VISIBLE');
 await page.click('#confirm-spec-criteria');await page.waitForFunction(()=>currentSpec.current_revision===2);
 check(spec.state==='editing'&&gate===null,'R8_NEW_REVISION_REUSED_GATE');check(oldRevision!==revision.acceptance_json,'R8_CONFIRM_DID_NOT_CREATE_REVISION');
 await page.click('#request-spec-review');await page.waitForFunction(()=>specAgentActive&&specAgentActive.status==='pending');await page.locator('.transfer-spec').click();await page.selectOption('#list-transfer-owner','乙');page.once('dialog',d=>d.accept());await page.click('#confirm-list-transfer');await page.waitForFunction(()=>document.getElementById('transfer-screen').hidden);
 check(requests[0].status==='cancelled'&&spec.current_editor_short_name==='乙','R8_CANCEL_TRANSFER_UI_FAILED');
 const late=await page.request.post(base+'/fixture/reply');check(late.status()===409,'R8_LATE_REPLY_NOT_REJECTED');check(!errors.length,'R8_UI_PAGE_ERRORS '+errors.join(';'));
 console.log(JSON.stringify({suite:'r32-spec-review-browser',passed:checks,failed:0,scope:'production HTML with fixture HTTP; not backend security acceptance'}));
 }finally{await browser.close();server.close()}
}
main().catch(e=>{console.error(e);server.close();process.exitCode=1});
