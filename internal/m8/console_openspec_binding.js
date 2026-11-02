'use strict';
(() => {
  const $ = id => document.getElementById(id);
  const endpoint = '/api/console/openspec-bindings';
  let generation = 0, pending = null, busy = false;
  const bodyFromForm = () => ({principal: $('principal').value, provider: 'github', instance: 'github.com', login: $('login').value, account_id: $('accountID').value});
  const mappingText = value => value === null ? '无映射' : 'Console principal：' + value.principal + '\nGitHub 登录名：' + value.login + '\nGitHub 数字 ID：' + value.account_id;
  const closeConfirmation = () => { pending = null; $('confirmation').hidden = true; };
  const setBusy = value => {
    busy = value;
    document.querySelectorAll('input,button').forEach(el => { el.disabled = value; });
  };
  async function request(method, body) {
    const path = method === 'GET' ? endpoint + '?' + new URLSearchParams({principal: body.principal, provider: 'github', instance: 'github.com'}) : endpoint;
    const options = {method, credentials: 'same-origin', cache: 'no-store'};
    if (method !== 'GET') { options.headers = {'Content-Type': 'application/json'}; options.body = JSON.stringify(body); }
    const response = await fetch(path, options);
    const data = response.status === 204 ? null : await response.json().catch(() => null);
    if (!response.ok) throw new Error(data && typeof data.error === 'string' ? data.error : '请求失败（HTTP ' + response.status + '）');
    if (method !== 'GET' || data === null) return null;
    if (!data || typeof data.principal !== 'string' || typeof data.login !== 'string' || typeof data.account_id !== 'string') throw new Error('绑定响应格式无效');
    return {principal: data.principal, provider: 'github', instance: 'github.com', login: data.login, account_id: data.account_id};
  }
  async function query() {
    if (busy) return;
    const body = bodyFromForm(), turn = ++generation;
    closeConfirmation(); setBusy(true); $('notice').textContent = '';
    try {
      const current = await request('GET', body);
      if (turn === generation) $('current').textContent = mappingText(current);
    } catch (error) { if (turn === generation) $('notice').textContent = error.message; }
    finally { setBusy(false); }
  }
  async function prepare(method) {
    if (busy) return;
    const body = bodyFromForm(), turn = ++generation;
    closeConfirmation(); setBusy(true); $('notice').textContent = '';
    try {
      const current = await request('GET', body);
      if (turn !== generation) return;
      $('current').textContent = mappingText(current);
      if (method === 'DELETE' && current === null) { $('notice').textContent = '当前无映射，无需删除。'; return; }
      pending = {method, body: method === 'DELETE' ? current : body, turn};
      $('confirmTitle').textContent = method === 'PUT' ? '确认保存绑定' : '确认删除绑定';
      $('confirmTarget').textContent = '目标 Console principal：' + body.principal;
      $('confirmOld').textContent = mappingText(current);
      $('confirmNew').textContent = method === 'PUT' ? mappingText(body) : '删除上述当前映射';
      $('confirmation').hidden = false;
    } catch (error) { if (turn === generation) $('notice').textContent = error.message; }
    finally { setBusy(false); }
  }
  async function commit() {
    if (busy || !pending) return;
    const operation = pending;
    closeConfirmation(); setBusy(true); $('notice').textContent = '';
    try {
      await request(operation.method, operation.body);
      const current = await request('GET', operation.body);
      if (operation.turn === generation) {
        $('current').textContent = mappingText(current);
        $('notice').textContent = operation.method === 'PUT' ? '保存请求已完成，请核对当前映射。' : '删除请求已完成，请核对当前映射。';
      }
    } catch (error) { if (operation.turn === generation) $('notice').textContent = error.message; }
    finally { setBusy(false); }
  }
  $('query').onclick = query;
  $('bindingForm').onsubmit = event => { event.preventDefault(); prepare('PUT'); };
  $('remove').onclick = () => prepare('DELETE');
  $('confirm').onclick = commit;
  $('cancel').onclick = closeConfirmation;
  ['principal', 'login', 'accountID'].forEach(id => { $(id).oninput = () => { ++generation; closeConfirmation(); $('current').textContent = '表单已更改，请重新查询。'; $('notice').textContent = ''; }; });
})();
