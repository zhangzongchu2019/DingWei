package m8

import "net/http"

// 新平台（G3/N3，第一期）的 console 登录页。
//
// 两种登录，接口都在 G3 设计件 3.3 / opus50tc2k 的 N3 分支（httpapi/n3.go）：
//
//	密码   POST /console/login/password {user_id,password} → {user_id,must_change_password}
//	       must_change_password 为真 ⇒ 本页只给改密表单；POST /api/console/account/password {old_password,new_password}
//	飞书   POST /console/login/code {user_id} → {code,expires_at}；用户在飞书里把「#console 码」发给机器人，
//	       本页每 2 秒 GET /console/login/status?code= ，{state:"confirmed"} 即已登录（服务端同时设会话 cookie）
//
// 页面不存任何秘密：密码只在输入框与一次请求里，不进 localStorage、不进日志；错误只认机器码、页面自己写中文。
// 「必须改密」的会话在服务端只能调改密接口，其它接口一律 403 must_change_password（本页不靠前端拦）。
//
// 路由登记（G3 的 newplat.go，非本文件）：mux.HandleFunc("GET /console/login", hub.HandleNewPlatformLoginPage)。
// 本处理器公开可访问：它不授予任何权限，只是一张表单。

// HandleNewPlatformLoginPage 返回新平台登录页。
func (h *Hub) HandleNewPlatformLoginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	_, _ = w.Write([]byte(newPlatformLoginPageHTML))
}

const newPlatformLoginPageHTML = `<!doctype html><meta charset=utf-8><meta name=viewport content="width=device-width,initial-scale=1"><title>登录 DingWei</title>
<style>
body{font:14px/1.5 -apple-system,"Segoe UI",sans-serif;margin:0;background:#f5f6f8;color:#222}
main{max-width:420px;margin:8vh auto;background:#fff;border-radius:10px;padding:24px 28px;box-shadow:0 2px 12px rgba(0,0,0,.08)}
h1{font-size:20px;margin:0 0 4px}h2{font-size:15px;margin:18px 0 8px}p.sub{margin:0 0 16px;color:#666}
label{display:block;margin:10px 0}input{display:block;width:100%;box-sizing:border-box;padding:7px 9px;margin-top:4px;font-size:14px}
button{padding:7px 14px;font-size:14px;cursor:pointer}button[disabled]{cursor:default;opacity:.6}
section{border-top:1px solid #e3e5e8;margin-top:16px;padding-top:4px}section[hidden]{display:none}
.err{min-height:1.3em;color:#9d1b1b;margin:6px 0}.hint{color:#666;font-size:13px}
code{display:inline-block;background:#f0f1f4;padding:3px 8px;border-radius:5px;font-size:15px;user-select:all}
</style>
<main>
<h1>登录 DingWei</h1><p class=sub>用户 ID 加密码，或用飞书确认。</p>

<section id=pwSection>
<h2>用户 ID 和密码</h2>
<label>用户 ID <input id=pwUser autocomplete=username spellcheck=false></label>
<label>密码 <input id=pwPass type=password autocomplete=current-password></label>
<button id=pwGo type=button>登录</button><div id=pwErr class=err role=alert></div>
</section>

<section id=changeSection hidden>
<h2>请先修改初始密码</h2><p class=hint>这是你的第一次登录，需要先设置一个新密码，之后才能进入 console。新密码至少 12 位，且不能和旧密码相同。</p>
<label>旧密码 <input id=chOld type=password autocomplete=current-password></label>
<label>新密码 <input id=chNew type=password autocomplete=new-password></label>
<label>再输入一次新密码 <input id=chNew2 type=password autocomplete=new-password></label>
<button id=chGo type=button>修改密码并进入</button><div id=chErr class=err role=alert></div>
</section>

<section id=fsSection>
<h2>用飞书确认</h2>
<label>用户 ID <input id=fsUser autocomplete=username spellcheck=false></label>
<button id=fsGo type=button>获取登录码</button><div id=fsErr class=err role=alert></div>
<div id=fsBox hidden>
<p>请在飞书里向机器人发送：</p><p><code id=fsCmd></code></p>
<p class=hint id=fsStatus role=status></p>
<button id=fsRetry type=button hidden>重新获取登录码</button>
</div>
</section>
</main>
<script>
var LOGIN_POLL_MS=2000;
var LOGIN_ERRORS={
  bad_credentials:'用户 ID 或密码不对',
  bad_request:'用户 ID 格式不对（2～32 位大小写字母和数字）',
  origin_mismatch:'请求来源不符，请刷新页面后重试',
  unauthenticated:'登录已失效，请重新登录',
  must_change_password:'请先修改初始密码',
  internal:'服务端出错了，请稍后再试'
};
var fsTimer=null,fsGeneration=0,lastPw='';
function $(id){return document.getElementById(id);}
function say(id,text){var e=$(id);if(e)e.textContent=text||'';}
function errText(status,data){
  var code=data&&typeof data.error==='string'?data.error:'';
  if(code==='rate_limited'){var w=data&&Number.isFinite(data.retry_after_s)?data.retry_after_s:null;return '尝试次数过多，'+(w===null?'请稍后再试':'请 '+w+' 秒后再试');}
  if(code==='weak_password'){var n=data&&Number.isFinite(data.min_length)?data.min_length:12;return '新密码至少 '+n+' 位，且不能和旧密码相同';}
  return LOGIN_ERRORS[code]||('操作失败（HTTP '+status+(code?'：'+code:'')+'）');
}
async function post(path,body){
  try{
    var r=await fetch(path,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body||{})});
    var data=null;try{data=await r.json();}catch(_){}
    return {ok:r.ok,status:r.status,data:data||{},text:r.ok?'':errText(r.status,data)};
  }catch(e){return {ok:false,status:0,data:{},text:'请求失败，请检查网络后重试'};}
}
function enter(){location.assign('/console');}
function showChange(prefillOld){
  $('pwSection').hidden=true;$('fsSection').hidden=true;$('changeSection').hidden=false;
  $('chOld').value=prefillOld||'';
  (prefillOld?$('chNew'):$('chOld')).focus();
}
async function passwordLogin(){
  var user=$('pwUser').value.trim(),pass=$('pwPass').value;
  if(!user||!pass){say('pwErr','请输入用户 ID 和密码');return;}
  var btn=$('pwGo');btn.disabled=true;
  var res=await post('/console/login/password',{user_id:user,password:pass});
  btn.disabled=false;
  if(!res.ok){say('pwErr',res.text);$('pwPass').value='';return;}   // 失败后清掉密码,保留用户 ID
  say('pwErr','');
  if(res.data.must_change_password){lastPw=pass;$('pwPass').value='';showChange(lastPw);lastPw='';return;}   // 必须改密:不进 console
  $('pwPass').value='';enter();
}
async function changePassword(){
  var oldPw=$('chOld').value,n1=$('chNew').value,n2=$('chNew2').value;
  if(!oldPw||!n1){say('chErr','请填写旧密码和新密码');return;}
  if(n1!==n2){say('chErr','两次输入的新密码不一致');return;}
  var btn=$('chGo');btn.disabled=true;
  var res=await post('/api/console/account/password',{old_password:oldPw,new_password:n1});
  btn.disabled=false;
  if(!res.ok){say('chErr',res.text);if(res.status===401)$('chOld').value='';return;}   // 只有"旧密码不对"才清旧密码框;弱密码等其它失败保留它,新密码也保留
  say('chErr','');['chOld','chNew','chNew2'].forEach(function(id){$(id).value='';});enter();
}
function fsStop(){clearTimeout(fsTimer);fsTimer=null;fsGeneration++;}
async function fsStart(){
  var user=$('fsUser').value.trim();
  if(!user){say('fsErr','请输入用户 ID');return;}
  fsStop();var mine=fsGeneration;
  var btn=$('fsGo');btn.disabled=true;
  var res=await post('/console/login/code',{user_id:user});
  btn.disabled=false;
  if(!res.ok){say('fsErr',res.text);$('fsBox').hidden=true;return;}
  say('fsErr','');
  var code=String(res.data.code||''),expires=Date.parse(res.data.expires_at||'');
  $('fsCmd').textContent='#console '+code;$('fsBox').hidden=false;$('fsRetry').hidden=true;
  say('fsStatus','等待你在飞书里确认…');
  async function poll(){
    if(mine!==fsGeneration)return;
    if(Number.isFinite(expires)&&Date.now()>=expires){
      $('fsCmd').textContent='';say('fsStatus','登录码已过期，请重新获取。');$('fsRetry').hidden=false;return;   // 过期:不再轮询,不再显示旧码
    }
    try{
      var r=await fetch('/console/login/status?code='+encodeURIComponent(code),{cache:'no-store'});
      var d=null;try{d=await r.json();}catch(_){}
      if(mine!==fsGeneration)return;
      if(r.ok&&d&&d.state==='confirmed'){say('fsStatus','已确认，正在进入…');enter();return;}
    }catch(_){}   // 网络抖动:继续轮询
    fsTimer=setTimeout(poll,LOGIN_POLL_MS);
  }
  poll();
}
$('pwGo').onclick=passwordLogin;$('chGo').onclick=changePassword;$('fsGo').onclick=fsStart;$('fsRetry').onclick=fsStart;
// 从 console 被踢回来(会话必须改密)时带 #change:只给改密表单,旧密码要重新输入
if(location.hash==='#change')showChange('');
</script>
`
