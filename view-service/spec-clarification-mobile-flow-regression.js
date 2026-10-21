// Three real production pages; loopback synthetic data only. This does not
// claim a real Feishu in-app browser or real provider-message acceptance.
const assert=require('node:assert/strict'),fs=require('fs'),path=require('path'),http=require('http'),{chromium}=require('playwright');
const root=path.resolve(__dirname,'..');
function page(file,name){const source=fs.readFileSync(path.join(root,'internal/m8',file),'utf8'),marker='const '+name+' = `',start=source.indexOf(marker);assert(start>=0);const from=start+marker.length;return source.slice(from,source.indexOf('`',from))}
const thread=page('spec_clarification_page.go','specClarificationHTML'),template=page('pilot_template_page.go','pilotTemplateHTML'),questions=page('pilot_question_page.go','pilotQuestionsHTML');
const login=page('console_login_newplat.go','newPlatformLoginPageHTML');
const specs=page('console_specs_page.go','consoleSpecsPageHTML').replace('<script id=dw-md-core></script>',()=>'<script>'+fs.readFileSync(path.join(__dirname,'md-render-core.js'),'utf8')+'</script>');
let role='pm',actor='user:owner',imageUploads=[],posts=[],saved=[],failedImage=false;let checks=0;
const fixtureSpec={id:'spec',title:'不可改写的定稿',state:'final',current_revision:3,principal_id:'owner',archived:false,can_edit:false,can_split:false};
const question={id:'q',spec_id:'spec',revision:3,task_id:'task',version:1,generation:1,kind:'requirement_unclear',title:'对应条目问题',observed_fact:'PRIVATE_D3_NOT_PUBLIC',question:'保持范围解释A1',expected_answer:'边界',state:'awaiting_answer',clarification_required:true,can_answer:false,notifications:[]};
function json(res,status,data){res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(data))}
const server=http.createServer(async(req,res)=>{const u=new URL(req.url,'http://fixture');let raw=Buffer.alloc(0);for await(const c of req)raw=Buffer.concat([raw,c]);
 if(u.pathname==='/console/login'){res.writeHead(200,{'Content-Type':'text/html'});return res.end(login)}
 if(u.pathname==='/console/login/password')return json(res,200,{user_id:'owner',must_change_password:false});
 if(u.pathname==='/console/clarification-return'){res.writeHead(302,{Location:'/console/spec-clarifications?question_id=q'});return res.end()}
 if(u.pathname==='/console'){res.writeHead(200,{'Content-Type':'text/html'});return res.end('<main>Console fixture</main>')}
 if(u.pathname==='/console/spec-clarifications'){res.writeHead(200,{'Content-Type':'text/html'});return res.end(thread)}
 if(u.pathname==='/console/spec-templates'){res.writeHead(200,{'Content-Type':'text/html'});return res.end(template)}
 if(u.pathname==='/console/questions'){res.writeHead(200,{'Content-Type':'text/html'});return res.end(questions)}
 if(u.pathname==='/console/specs/spec'){res.writeHead(200,{'Content-Type':'text/html'});return res.end(specs)}
 if(u.pathname.startsWith('/console/spec-assets/')){const n=path.basename(u.pathname);if(!['easymde.min.js','easymde.min.css'].includes(n))return json(res,404,{});return res.end(fs.readFileSync(path.join(root,'internal/m8/static/easymde',n)))}
 if(u.pathname==='/api/console/spec-templates/options')return json(res,200,{actor_principal:actor,template_version:'pilot-spec-light/1',users:[],agents:[],sample:{entries:[]}});
 if(u.pathname==='/api/console/spec-templates/parents')return json(res,200,{parents:[]});
 if(u.pathname==='/api/console/spec-templates'&&req.method==='GET')return json(res,200,{drafts:[]});
 if(u.pathname==='/api/console/specs/spec'||u.pathname==='/api/specs/shared/spec')return json(res,200,{spec:fixtureSpec,revision:{content_markdown:'# 原定稿\n条目 A1：只读正文，不能以解释更改。',content_sha256:'a'.repeat(64),acceptance_json:'[{"id":"A1","text":"不改范围","cases":[]}]',input_anchors_json:'[]'},attribution:{},tasks:[],dependency_candidates:[]});
 if(u.pathname.endsWith('/agent-review'))return json(res,200,{requests:[],agents:[]});
 if(u.pathname==='/api/console/specs/spec/clarifications')return json(res,200,{posts:[{floor:7,role:'pm',actor:'owner',kind:'explanation',question_id:'q',entry_refs:['A1'],body:'公开补充，只作原范围的解释'}],next_before:0,data_only:true,execution_authorized:false});
 if(u.pathname==='/api/console/specs')return json(res,200,{specs:[fixtureSpec],page:1,total_pages:1,agents:[]});
 if(u.pathname==='/api/console/spec-editors')return json(res,200,{editors:[]});
 if(u.pathname==='/api/console/specs/spec/question-context')return json(res,200,{spec_id:'spec',task_id:'task',expected_version:1,entry_refs:['A1'],can_configure_policy:false,contacts:[]});
 if(u.pathname==='/api/console/questions')return json(res,200,{questions:[question],execution_authorized:false});
 if(u.pathname==='/api/console/questions/q')return json(res,200,{question,answers:[],changes:[]});
 if(u.pathname==='/api/console/questions/q/clarification-images'){
  imageUploads.push(raw);if(failedImage)return json(res,503,{error:'private_images_pending_config'});return json(res,200,{image:{file_id:'f'.repeat(64),media_type:'image/png',sha256:'a'.repeat(64),size:raw.length},execution_authorized:false})
 }
 if(u.pathname==='/api/console/questions/q/clarifications'){
  if(req.method==='GET')return json(res,200,{context:{actor,spec_id:'spec',revision:3,content_sha256:'a'.repeat(64),question_id:'q',title:'问题',question:'解释A1',expected_answer:'边界',role,entry_refs:['A1'],archived:false,policy_version:1},posts:posts.slice().reverse(),next_before:0,enabled:true});
  const b=JSON.parse(raw);const post={...b,question_id:'q',floor:posts.length+1,role,actor,images:(b.file_ids||[]).map(id=>({file_id:id,media_type:'image/png',size:100,sha256:'a'.repeat(64)}))};posts.push(post);return json(res,200,{post,replayed:false,execution_authorized:false});
 }
 if(u.pathname==='/api/console/specs/spec/clarification-policy')return json(res,200,{policy:{spec_id:'spec',pm_user:'owner',deputy_user:'',version:1},enabled:true,can_select_pm:true,can_select_deputy:true});
 if(u.pathname==='/api/console/spec-template-sources'){const b=JSON.parse(raw);return json(res,200,{source:{id:'source',source_kind:b.mode,source_url:b.source_url,state:'ready',content_sha256:'c'.repeat(64)}})}
 if(u.pathname==='/api/console/spec-templates'&&req.method==='POST'){
  const b=JSON.parse(raw);saved.push(b);return json(res,200,{draft:{draft_id:'successor-draft',owner_principal:'owner',version:1,state:'pending',document:b.document,source:{id:'source',source_kind:'manual_export',source_url:'https://fixture.feishu.cn/docx/id',state:'ready',content_sha256:'c'.repeat(64)},missing:[{path:'roles.R1.reviewer',reason:'not_accepted'}],roles:[],can_confirm:false,plan_sha256:'b'.repeat(64),content_sha256:'d'.repeat(64),content_markdown:'待确认的新范围',execution_authorized:false},artifacts:{},execution_authorized:false})
 }
 return json(res,404,{error:'not_found'});
});
(async()=>{await new Promise(r=>server.listen(0,'127.0.0.1',r));const origin='http://127.0.0.1:'+server.address().port,browser=await chromium.launch({headless:true,channel:process.env.PW_CHANNEL||'chrome'}),errors=[];
try{
 for(const width of [390,820]){
  posts=[];role='pm';actor='user:owner';const p=await browser.newPage({viewport:{width,height:1000},isMobile:true,hasTouch:true});p.setDefaultTimeout(6000);p.on('pageerror',e=>errors.push(e.message));
  await p.goto(origin+'/console/login?clarification_state='+'a'.repeat(64));for(const id of ['pwUser','pwGo']){const b=await p.locator('#'+id).boundingBox();assert(b.height>=44,'MOBILE_LOGIN_TOUCH_SIZE')}await p.fill('#pwUser','owner');await p.fill('#pwPass','SYNTHETIC-LOCAL-ONLY');await p.click('#pwGo');await p.waitForURL('**/console/spec-clarifications?question_id=q');assert.equal(new URL(p.url()).origin,origin);checks++;
  await p.goto(origin+'/console/login?clarification_state='+encodeURIComponent('https://outside.invalid/'));await p.fill('#pwUser','owner');await p.fill('#pwPass','SYNTHETIC-LOCAL-ONLY');await p.click('#pwGo');await p.waitForURL(origin+'/console');assert.equal(new URL(p.url()).origin,origin,'MOBILE_LOGIN_OPEN_REDIRECT');checks++;
  await p.goto(origin+'/console/questions?spec_id=spec&task_id=task&question_id=q');await p.waitForSelector('#detail:not([hidden])');assert(await p.locator('#answer-form').isHidden(),'MOBILE_OLD_ANSWER_FORM_ENABLED');assert.equal(await p.locator('#create-refs').inputValue(),'A1');assert(await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'MOBILE_QUESTION_HORIZONTAL_OVERFLOW');checks++;
  await p.locator('#clarification-link').click();await p.waitForSelector('#compose:not([hidden])');assert((await p.locator('body').innerText()).includes('外部浏览器'));checks++;
  const png=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+lm8sAAAAASUVORK5CYII=','base64');
  await p.setInputFiles('#images',{name:'screen.png',mimeType:'image/png',buffer:png});await p.waitForFunction(()=>document.querySelector('#image-status').textContent.includes('已完成 1'));
  await p.fill('#body','手机附图解释');await p.click('#submit');await p.waitForFunction(()=>document.querySelector('#status').textContent.includes('已入第'));assert.deepEqual(posts.at(-1).file_ids,['f'.repeat(64)]);assert.equal(await p.locator('#posts a').filter({hasText:'查看私有附图'}).count(),1);checks++;
  const count=imageUploads.length;await p.setInputFiles('#images',{name:'bad.svg',mimeType:'image/svg+xml',buffer:Buffer.from('<svg/>')});await p.waitForFunction(()=>document.querySelector('#status').textContent.includes('附图未上传'));assert.equal(imageUploads.length,count);checks++;
  const limitBefore=imageUploads.length;await p.setInputFiles('#images',{name:'large.png',mimeType:'image/png',buffer:Buffer.alloc(5*1024*1024+1)});await p.waitForFunction(()=>document.querySelector('#status').textContent.includes('附图未上传'));assert.equal(imageUploads.length,limitBefore,'MOBILE_IMAGE_SIZE_NOT_BOUNDED');checks++;
  await p.setInputFiles('#images',Array.from({length:5},(_,i)=>({name:'screen'+i+'.png',mimeType:'image/png',buffer:png})));await p.waitForFunction(()=>document.querySelector('#status').textContent.includes('附图未上传'));assert.equal(imageUploads.length,limitBefore,'MOBILE_IMAGE_COUNT_NOT_BOUNDED');checks++;
  failedImage=true;await p.setInputFiles('#images',{name:'screen.png',mimeType:'image/png',buffer:png});await p.waitForFunction(()=>document.querySelector('#image-status').textContent.includes('附图未完成'));const n=posts.length;await p.click('#submit');await p.waitForFunction(()=>!document.querySelector('#submit').disabled);assert.equal(posts.length,n,'MOBILE_INCOMPLETE_UPLOAD_POSTED');assert((await p.locator('#status').innerText()).includes('尚未全部上传成功'));failedImage=false;checks++;
  const href=await p.locator('#spec-link').getAttribute('href');await p.goto(origin+href);await p.waitForSelector('#spec-rendered:not([hidden])');assert((await p.locator('#spec-rendered').innerText()).includes('只读正文'));assert(await p.locator('#save-spec').isHidden());await p.locator('#clarification-summary summary').click();await p.waitForFunction(()=>document.querySelector('#clarification-summary').textContent.includes('公开补充'));assert(!(await p.locator('#clarification-summary').innerText()).includes('PRIVATE_D3'));assert(await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'MOBILE_SPEC_READONLY_HORIZONTAL_OVERFLOW');checks++;
  await p.goto(origin+'/console/spec-templates?predecessor_spec_id=spec');await p.waitForFunction(()=>document.querySelector('#target').value==='spec');assert.equal(await p.locator('#target-revision').inputValue(),'3');assert(await p.locator('#predecessor').isChecked());assert(await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'MOBILE_SUCCESSOR_HORIZONTAL_OVERFLOW');checks++;
  await p.fill('#change-reason','PM说明需追加，待所有者确认');await p.fill('#source-url','https://fixture.feishu.cn/docx/id');await p.selectOption('#source-mode','manual_export');await p.fill('#source-revision','revision-1');await p.fill('#source-content','本人有权导出的范围原文');await p.click('#save-source');await p.waitForFunction(()=>document.querySelector('#source-summary').textContent.includes('ready'));await p.fill('#goal','待确认新目标');await p.fill('#scope','严格限定的后继范围');await p.fill('#entry-0-title','新条目');await p.fill('#entry-0-owner_ref','本人有权导出的范围原文');await p.click('#save-draft');await p.waitForFunction(()=>document.querySelector('#status').textContent.includes('已保存待完善'));assert.equal(saved.at(-1).document.predecessor_spec_id,'spec');assert.equal(saved.at(-1).document.expected_revision,3);assert.equal(saved.at(-1).document.change_reason,'PM说明需追加，待所有者确认');assert(await p.locator('#confirm').isDisabled());assert.equal(fixtureSpec.current_revision,3);checks++;
  if(process.env.SCREENSHOT_DIR){fs.mkdirSync(process.env.SCREENSHOT_DIR,{recursive:true});await p.screenshot({path:path.join(process.env.SCREENSHOT_DIR,`successor-mobile-${width}.png`),fullPage:true})}
  actor='user:pm-other';await p.goto(origin+'/console/spec-templates?predecessor_spec_id=spec');await p.waitForFunction(()=>document.querySelector('#status').textContent.includes('仅原 spec 所有者'));assert.equal(await p.locator('#target').inputValue(),'');checks++;await p.close();
 }
 assert.deepEqual(errors,[]);checks++;console.log(JSON.stringify({suite:'spec-clarification-mobile-flow',passed:checks,failed:0,widths:[390,820],live_access:false,real_feishu_inapp:false}));
}finally{await browser.close();await new Promise(r=>server.close(r))}})().catch(e=>{console.error(e.stack);server.close();process.exitCode=1});
