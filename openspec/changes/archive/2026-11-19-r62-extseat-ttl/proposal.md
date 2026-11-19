# R62 外部 Agent 七天存活期切片

## Why
业主已明确外部Agent相关存活期“都按7天来处理”，组长于2026-11-18T16:17:02Z单独授权第1片实现。当前临时AG仅三天、消息仅二十四小时，与新的使用周期不一致；本片仅统一这两个存活期，保留既有授权、轮询与逐条消息绝对到期规则，避免延长令牌或悄悄重写存量消息。

## 来源
批准设计：`togpt60lunazzc/r62-extseat-design@124d24ee3818762f977920d5a015e4dfdca80663` 的design.md Lifetime policy、Verification第一条与docs/design/R62-EXTSEAT-SLICES.md第1片；派单S-R62-EXTSEAT-TTL（组长2026-11-18T16:17:02Z），实施基点main206d41b26dab646c0e50a111eed5a45ccf7c72f6。

## Scope
仅将GroupTTL与MessageTTL改为7×24小时，补真实PG生产服务边界/续期/存量回归和两条旧值变异。TokenTTL仍14天，无迁移，不改现有持久expiry，不改版本、不部署；正常受理发送及首次received ACK按现有事件正常续组，不属于存量回填。Unicode、普通组同权、抄送摘要、500条保留及上传均不在本片。

## 待确认假设
无新增产品假设。PG边界使用一微秒前后，匹配timestamptz精度；七天是UTC duration 168小时，不是本地日历或重置日期解析。

## 已裁定边界
R62TTL-PM01（2026-11-18T18:05:27Z组长裁定）：本TTL片不改已投影接管语义、不加发送前AG围栏；通用“失去授权后在途件”与内部普通编组成员移出行为一并归r62切片三设计。两源红探针作为既有边界证据保留，不新增过期后仍投递的正向回归。安全面常驻格已钉未投影dead/group_unavailable及Check等拒绝不返正文。裁定不等于该边界被修复，切片三实施仍需另派。
