# 外部名称长度与兼容核对

指定基线main fce2ede8。外部短名20＋下划线1＋AG前缀2＋序号最多18＝Agent名41；加点1与用户简称最多16＝全名58。不是把普通Agent名改成41或用户简称改成20。

| 入口/持久/消费 | 处理 |
|---|---|
| extshared.shortNameRE / AgentName / SplitAgentName | 只对外部短名从8放宽到20，AG仍1～18位无前导零；旧名继续解析 |
| feishulogin.Start、connect.go | 原手填两项保持8，确保关闭功能不再产生新长名；受控自动意图在可信profile后生成最长20，不接受表单覆盖 |
| extseat.IssueToken | 原共享校验自动继承20；身份四元组、AG唯一性、事务内创建与目标锁不变 |
| PG np_extseat.short_name | 新0108迁移改CHECK至1～20；旧0105正文与digest不改；旧行原样读取；旧约束回退受长名行阻挡 |
| PG np_agent.name/name_lc、np_user.short_name、tempgroup与投递姓名字段 | text，无新增长度截断。只更改外部短名CHECK；用户简称原校验2～16，普通Agent2～32不动 |
| identity.ResolveFullName | 既有外部专用SplitAgentName分支继承20；仍现核origin=external；不能使普通注册创建带_AG名称 |
| m8.console_auth.go约312、console_mux.go、health_auth.go | console自备Agent/用户简称/普通目标规则保持原限；外部不是自备helper，禁止为了长外部名批量放宽这些规则 |
| m8.extseat.go路由、目录与presence | 外部专用分支按ID授权，继承共享校验；新长名路由和真实WS在线清单用例验证无截短、无伪造helper presence |
| m8.amr.go提及解析 | amrMentionToken/amrTokenName允许字母数字下划线和点，未设置8/32位外部上限；路由仍现核身份与AG。不改普通Agent提及权限 |
| helper routing.py / send_dingwei.py /目录展示 | 提及字符集合含下划线；legacy route表达式无8位限制；desktop_dispatch目标上限256大于58，不需要放宽 |
| Console临时组与View显示 | 外部全名原样使用文本渲染、按ID授权，样式可折行；未新增helper View身份或订阅权限，未改自备Agent表单上限 |

实际PG测试把AG序号置于合法18位范围，8路同秒签发得到最长58位全名，验证Authenticate和ResolveFullName往返。非法21位与普通名称上限都有反例。文本静态核对不是桌面实测或所有浏览器视觉验收；桌面仍未测。
