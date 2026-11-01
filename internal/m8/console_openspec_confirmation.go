package m8

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"

	"github.com/zhangzongchu2019/dingwei/internal/specconfirm"
)

func (h *Hub) openSpecConfirmationReady() bool {
	return h.SpecConfirmations != nil && h.SpecConfirmations.Store != nil && h.SpecConfirmations.Authority != nil
}

func confirmationError(w http.ResponseWriter, err error) {
	status := http.StatusServiceUnavailable
	message := "确认依赖未就绪，无法验证委员会资格、仓库权限或版本"
	switch {
	case errors.Is(err, specconfirm.ErrInvalid):
		status = http.StatusBadRequest
		message = "请提供规范仓库、变更目录、完整 commit 与确认含义"
	case errors.Is(err, specconfirm.ErrForbidden):
		status = http.StatusForbidden
		message = "无权执行该操作"
	case errors.Is(err, specconfirm.ErrConflict):
		status = http.StatusConflict
		message = "该操作已绑定另一确认事实，请检查历史记录"
	}
	http.Error(w, message, status)
}
func confirmationCoordinate(q url.Values) (specconfirm.Coordinate, error) {
	for key, values := range q {
		if len(values) != 1 {
			return specconfirm.Coordinate{}, specconfirm.ErrInvalid
		}
		switch key {
		case "git_instance", "repository", "change_dir", "commit_sha":
		default:
			return specconfirm.Coordinate{}, specconfirm.ErrInvalid
		}
	}
	c := specconfirm.Coordinate{Instance: q.Get("git_instance"), Repository: q.Get("repository"), ChangeDir: q.Get("change_dir"), Commit: q.Get("commit_sha")}
	return c, c.Validate()
}
func (h *Hub) HandleOpenSpecConfirmations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor := platformPrincipal(r)
	if actor == "" {
		confirmationError(w, specconfirm.ErrForbidden)
		return
	}
	if h.SpecConfirmations == nil {
		confirmationError(w, specconfirm.ErrUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			confirmationError(w, specconfirm.ErrInvalid)
			return
		}
		current, err := confirmationCoordinate(query)
		if err != nil {
			confirmationError(w, err)
			return
		}
		records, err := h.SpecConfirmations.List(r.Context(), actor, current)
		if err != nil {
			confirmationError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(struct {
			Current specconfirm.Coordinate `json:"current"`
			Records []specconfirm.View     `json:"records"`
		}{current, records})
	case http.MethodPost:
		if r.URL.RawQuery != "" {
			confirmationError(w, specconfirm.ErrInvalid)
			return
		}
		// JSON plus same-origin checks protect the legacy as well as new auth mode.
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			confirmationError(w, specconfirm.ErrInvalid)
			return
		}
		if raw := r.Header.Get("Origin"); raw != "" {
			origin, e := url.Parse(raw)
			if e != nil || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host != r.Host || (h.PublicOrigin != "" && raw != h.PublicOrigin) {
				confirmationError(w, specconfirm.ErrForbidden)
				return
			}
		}
		var req specconfirm.Request
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&req) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			confirmationError(w, specconfirm.ErrInvalid)
			return
		}
		fact, err := h.SpecConfirmations.Confirm(r.Context(), actor, req)
		if err != nil {
			confirmationError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(fact)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
func (h *Hub) HandleOpenSpecConfirmationPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	fmt.Fprint(w, openSpecConfirmationPage)
}

const openSpecConfirmationPage = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>OpenSpec 委员会确认</title>
<style>body{font:16px system-ui;max-width:960px;margin:2rem auto;padding:0 1rem}label{display:block;margin:1rem 0}input,textarea{display:block;width:95%;padding:.6rem}code{overflow-wrap:anywhere}article{border:1px solid #ccc;padding:1rem;margin:1rem 0}button{padding:.6rem;margin-right:1rem}.status{font-weight:bold}pre{white-space:pre-wrap}</style>
<a href="/console">返回 Console</a><h1>研发管理委员会确认</h1><p>普通变更以 MR 审阅为准。此处确认仅用于研发管理委员会，与 MR 审阅分别记录；确认不表示验收或发布。</p>
<section><h2>MR 审阅</h2><p>MR 审阅结果以仓库对应 commit 的 MR 记录为准；此页不生成或代替 MR 通过。</p></section>
<form id="coordinates"><label>Git 实例<input name="git_instance" required placeholder="git.code.tencent.com"></label><label>仓库标识<input name="repository" required placeholder="wego-ai/dingwei"></label><label>变更目录<input name="change_dir" required placeholder="openspec/changes/change-id"></label><label>当前浏览的完整 commit SHA<input name="commit_sha" required pattern="[0-9a-f]{40}" minlength="40" maxlength="40"></label><button type="submit">查看确认记录</button></form>
<section><h2>委员会确认</h2><p>仅可确认你实际审看的完整 commit。尚未启用实质修改判定时，不同版本显示“待核对”；原确认不会随分支移动。</p><label>确认含义与范围<textarea id="meaning" maxlength="4096" required></textarea></label><button id="confirm" disabled>确认上述不可变版本</button></section>
<p id="message" role="status"></p><div id="records"></div>
<script>
'use strict';
const form=document.getElementById('coordinates'),button=document.getElementById('confirm'),meaning=document.getElementById('meaning'),message=document.getElementById('message'),records=document.getElementById('records');
let generation=0,loaded=null,operation=null,busy=false;
function coordinate(){return Object.fromEntries(new FormData(form).entries());}
function clear(){generation++;loaded=null;operation=null;button.disabled=true;records.replaceChildren();message.textContent='';}
form.addEventListener('input',clear);meaning.addEventListener('input',()=>{operation=null;});
function line(parent,label,value,tag='p'){const node=document.createElement(tag);node.textContent=label+value;parent.appendChild(node);}
async function request(url,options){const response=await fetch(url,{credentials:'same-origin',cache:'no-store',...options});if(!response.ok)throw new Error(await response.text());return response.json();}
function draw(data){records.replaceChildren();line(records,'当前浏览 commit：',data.current.commit_sha,'code');for(const fact of data.records){const card=document.createElement('article');line(card,'类型：','委员会确认');line(card,'所确认 commit：',fact.commit_sha,'code');line(card,'仓库：',fact.git_instance+'/'+fact.repository);line(card,'变更：',fact.change_dir);line(card,'确认人：',fact.confirmed_by);line(card,'UTC 时刻：',fact.confirmed_at);line(card,'确认含义：',fact.meaning,'pre');line(card,'版本关系：',fact.assessment.reason);line(card,'判定策略：',fact.assessment.policy_version||'未启用');records.appendChild(card);}}
async function load(current){const mine=++generation;loaded=null;button.disabled=true;records.replaceChildren();try{const data=await request('/api/console/openspec-confirmations?'+new URLSearchParams(current));if(mine!==generation)return;draw(data);loaded={...data.current};button.disabled=busy;message.textContent='记录已读取；提交时仍须核实委员会资格与仓库权限。';}catch(e){if(mine===generation)message.textContent=e.message;}}
form.addEventListener('submit',event=>{event.preventDefault();operation=null;load(coordinate());});
button.addEventListener('click',async()=>{if(!loaded||busy||!meaning.value.trim())return;const target={...loaded};if(JSON.stringify(target)!==JSON.stringify(coordinate())){clear();return;}if(!operation)operation=crypto.randomUUID();const body={...target,operation_id:operation,meaning:meaning.value};const mine=generation;busy=true;button.disabled=true;try{const fact=await request('/api/console/openspec-confirmations',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});if(mine!==generation)return;await load(target);if(loaded)message.textContent='委员会确认已保存，仅针对 commit '+fact.commit_sha;}catch(e){if(mine===generation)message.textContent=e.message+'（重试将保留同一操作编号）';}finally{busy=false;button.disabled=!loaded;}});
</script></html>`
