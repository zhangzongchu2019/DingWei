回环 GET 装置说明（S-R62-OLDPLAT-SLIM）
- 实例：本工作树 go build 的 workpulse（二进制 workpulse 同目录），WP_ADDR=127.0.0.1:18777、WP_DB_PATH=/tmp/oldplat-slim-loopback/wp3.db（隔离临时库）、WP_CONSOLE_AUTH_ENABLED=1、WP_CONSOLE_SEED_FILE=seed.json（夹具 principal＋agent，非现网数据）。PID 1364371，GET 时 etime 00:10，server3.log 无 exited 行。
- 未登录 GET 读数（get-time 时刻）：/console/admin 401、/console/change-evidence 401、/console/openspec 401、/api/console/platform 401；响应体均为本实例文案「authentication required」（resp_*.txt）。
- 🔴 假启动披露：先试 127.0.0.1:18099，该端口被本机 nginx/1.24.0 占用（401 页面为 nginx 体），且 workpulse 因种子校验未起 ⇒ 该轮读数作废、不入证据；换 18777 并以响应体＋PID＋日志三重核实例归属后才取数。
- 「仓库提交」项：库内自定义项（组长 00:20:38Z 裁定白名单口径），本单不验回环；路由与数据未动。
- GET 后实例已 kill（PID 1364371），临时库不入库。
