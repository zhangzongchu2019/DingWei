# 开发阶段原始件（不是最终冻结源验收）

本目录运行时 HEAD 仍为 c6851ed9，但工作树有实现／测试改动尚未提交；原始 JSON 的 source/source_head 仅是当时 HEAD，不应解释成 c6851ed9 已含实现或已被本轮验收。最终门以另行冻结的源码提交、SOURCE.json 清单与 final 目录为准。

- private 首轮错误：旧的进程内 ControlFixture 未登记 pin；更新为真实保留核注册，不放宽产品能力门。guard 原19格绿。
- retention-v1：磁盘满已转固定 UpdateError，但新夹具仍期望原始 OSError，1 ERROR；改夹具断言固定拒绝码。combined-v2 101格过，lifecycle-v3 60格过，retention-v4 42格过，均为开发读数。
- withdrawals-v1：26刀中3未到指定红（两刀由稳定guard后置拒绝导致普通ERROR；一刀由第二道inode门兜底）。保留原结果，未声称PASS。补错误码边界、明确删除前后置结果断言；inode两处同时撤修另加最后窗口单独漂移格。
- withdrawals-v2：27刀中1未到指定红，原因是subTest在首格错误删除后继续到后格并产生ERROR；改为首断言失败即停止本例，另分别撤previous/pending门。
- withdrawals-v3：29刀全部指定AssertionError FAIL、恢复绿；检查器明确不接受普通ERROR当指定红。后续仅测试目录模式显式700的小修也会在最终冻结输入重跑。
- native-v1-0163：实际0.163.0独立owner、同ID、PTY正例，属于工作树开发读数；完整冻结门另跑两锁定版本。

没有改真实席位、Hub或生产目录；测试自身的孤儿进程注入只在独立子进程内设subreaper并精确收尸。三轮撤修、失败回执、stderr均原样保存。

第一冻结轮 b75 的全部要求命令已收尾：仅原49撤修中的两旧负夹具未到指定红，其余均rc0（helper915）。完整OUTCOME及原始件在final-b75eb8d，不能记整轮通过。随后源修正owner的-I与包根trampoline（业务cwd的同名包／json不得执行），并校验completed pin回执、所有未回收pin的碰撞拒绝；旧launcher legacy只接受经核全空retain，非空未知仍拒。后继工作树post-shadow-v1 92项绿，原49与新35撤修各绿；最后新增两更新根绑定格smoke3项绿。以上仍是开发工作树读数，最终以后继新冻结锚整轮重跑为准。
