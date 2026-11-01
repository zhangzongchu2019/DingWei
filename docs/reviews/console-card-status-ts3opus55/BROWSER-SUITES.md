# Full browser runner suite results

总rc2；26套均执行，24套rc0、2套非零。计数采用各套件运行时自报的断言／检查项；Python用例、输出样本、压力前提断言单独标明，不冒称所有嵌套assert()调用次数，也不混合求总数。

| Suite | rc | 总检查单元 | 通过 | 失败 | 已知缺陷 | 口径 |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| renderer_0206_file_render | 1 | 41 | 39 | 2 | 0 | 检查项 |
| console_browser | 0 | 20 | 20 | 0 | 0 | 检查项 |
| renderer_in_console_iframe | 0 | 3 | 3 | 0 | 0 | 检查项 |
| renderer_sse_reconnect | 0 | 26 | 26 | 0 | 0 | 检查项 |
| renderer_failcard | 0 | 5 | 5 | 0 | 0 | 检查项 |
| renderer_t054_errorboard | 0 | 6 | 6 | 0 | 0 | 检查项 |
| renderer_t074_delivery_id | 0 | 5 | 5 | 0 | 0 | 日志PASS行 |
| renderer_card_status | 0 | 18 | 18 | 0 | 0 | 检查项 |
| renderer_provenance_icon | 0 | 5 | 5 | 0 | 0 | 检查项 |
| renderer_storage_denied | 0 | 3 | 3 | 0 | 0 | 检查项 |
| terminal_storage_denied | 0 | 2 | 2 | 0 | 0 | 检查项 |
| renderer_t086_empty_ring_e2e | 0 | 2 | 2 | 0 | 0 | 检查项 |
| renderer_replay_seq | 0 | 13 | 13 | 0 | 0 | 检查项 |
| renderer_upload_ticket_timeout | 0 | 4 | 4 | 0 | 0 | 检查项 |
| renderer_flood_stress | 2 | 1 | 0 | 1 | 0 | 失败前提断言；完成压力场景0 |
| renderer_c2 | 0 | 29 | 25 | 0 | 4 | 检查项，4项已知T-166不计硬失败 |
| console_specs_browser | 0 | 220 | 220 | 0 | 0 | 套件显式断言计数 |
| console_spec_workflow | 0 | 12 | 12 | 0 | 0 | 检查项 |
| console_task_flow_r32 | 0 | 11 | 11 | 0 | 0 | 检查项 |
| pm_spec_flow | 0 | 15 | 15 | 0 | 0 | 检查项 |
| md_render_core_sync | 0 | 12 | 12 | 0 | 0 | Python测试用例 |
| md_render_core_parity | 0 | 99 | 99 | 0 | 0 | 输出比较样本 |
| renderer_markdown_image | 0 | 15 | 15 | 0 | 0 | 检查项 |
| renderer_markdown_injection | 0 | 11 | 11 | 0 | 0 | 检查项 |
| renderer_failure_dock_embedded | 0 | 4 | 4 | 0 | 0 | 检查项 |
| renderer_failure_dock | 0 | 3 | 3 | 0 | 0 | 检查项 |

原始总日志与机器回执：raw/browser-runner-full.log、raw/browser-runner-receipt.txt。首套件2红与洪流前提红的基线定向复跑另列，不改变本轮总门rc2。
