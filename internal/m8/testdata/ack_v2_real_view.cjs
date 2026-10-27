// Observe actual renderer + live Hub event ring. Never synthesize delivery events.
const {chromium}=require('playwright');
const fs=require('fs');
const path=require('path');
const readline=require('readline');
(async()=>{
 const lines=readline.createInterface({input:process.stdin})[Symbol.asyncIterator]();
 const boot=JSON.parse((await lines.next()).value);
 const browser=await chromium.launch({headless:true,channel:'chrome'});
 try {
  const page=await browser.newPage();const errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/*',r=>new URL(r.request().url()).origin===new URL(boot.url).origin?r.continue():r.abort());
  await page.goto(boot.url,{waitUntil:'domcontentloaded'});
  await page.locator('#humanInput').waitFor({state:'visible'});
  fs.writeFileSync(path.join(boot.root,'view-ready.json'),'{}');
  while(true){
   const next=await lines.next();if(next.done)break;
   const command=JSON.parse(next.value);
   const data=await page.evaluate(id=>{
    const waiting=window.__c2.waitingDeliveries.get(id);
    const card=c1InputCards.get(c1DeliveryCards.get(deliveryMergeKey(id)));
    return {waiting:!!waiting,waitingText:waiting?.node.textContent||'',stage:card?.stageEl.textContent||'',cardKey:c1DeliveryCards.get(deliveryMergeKey(id))||'',timeline:document.querySelector('#timeline').textContent};
   },command.delivery_id);
   data.errors=errors;
   const file=path.join(boot.root,'view-'+command.id+'.json');fs.writeFileSync(file+'.tmp',JSON.stringify(data));fs.renameSync(file+'.tmp',file);
  }
 }finally{await browser.close()}
})().catch(e=>{console.error(e.stack);process.exitCode=1});
