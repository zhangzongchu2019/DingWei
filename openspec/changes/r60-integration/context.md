# R60集成上下文

授权：组长2026-11-16T16:05:24Z；冲突口径20:57:01Z；Z1/联合格及Z3以2026-11-17T01:37:18Z最新令。正式基点110e2c1f，四输入见docs/reviews/r60-integration/CONFLICT-RESOLUTION.md。

代码：app.py IPC current装配；deploy/test_build_entrypoints.py inventory；internal/m8/r60_outbound_joint_test.go与hosted_body_send_test.go、viewer_output_queue_test.go；tools/sessionhelper/tests/test_session_heartbeat.py和hosted_body_hub_client.py；版本VERSION/internal/version/helper __init__。

环境：锁定隔离Python解释器、Go1.22.2、Node/OpenSpec1.16.0，PGstrict临时库，无现网、模型、在线席位；真实CLI组合格必须显式解释器以免SKIP。原始证据后续落docs/reviews/r60-integration/evidence。未推main/部署。

②最终证据tip依组长02:50:33Z令补至7590285b31d1276c8907f15c3780bbdcbee7747f，纯文档merge fd2c99e8d；最终四输入见CONFLICT-RESOLUTION。受测a311产品九项Git对象不变，门不重跑。

组长03:19:05Z要求0077回退只读实测，结论/代码坐标见docs/reviews/r60-integration/ROLLBACK-0077.md；旧库兼容启动但新协议未终结队列不可直接给旧Hub，依helper-send-lost design:59，不新增产品/迁移。

组长03:21:07Z澄清：本机构建只作门/SHA参考，不上传或交prodops。三对象SHA、go version -m和同SOURCE_COMMIT比对前提见docs/reviews/r60-integration/LOCAL-BUILD-REFERENCE.md；正式dw123 dwbuild/go1.22.2构建待终审与另令。

组长2026-11-17T04:01:15Z通过cecde/a311集成复核，待togpt60独立复核。正式dw123构建须显式设置DW_DESKTOP_INTEGRATION_PYTHON，并按INTEGRATION-GATES.md逐名核联合格与同ID双CLI格均PASS、0SKIP；构建脚本总绿不替代。
