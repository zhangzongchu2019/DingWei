// 测试桩共用:GET /api/console/agents 的一条记录,按设计件 3.2 的新形状
// {agent_id, name, full_name, origin, group_code, online}(组长裁定 2026-10-01:页签与成员一律按 full_name)。
// 桩里的"会话串"直接当 full_name 用——页签 key、iframe 的 /view/<…> 都跟着它走,各回归其余断言不变;
// name 取短名(与旧页面 dwShortName 同规则:三段式取中间段,否则原样)。
function shortName(s) { const p = String(s).split('-'); return p.length === 3 ? p[1] : String(s); }
function agentRecord(fullName, online = true, extra = {}) {
  return { agent_id: `id-${fullName}`, name: shortName(fullName), full_name: fullName, origin: 'selfhost', group_code: '', online, ...extra };
}
module.exports = { agentRecord, shortName };
