# 最终源41刀索引

固定源5bc0280d7d6d8da8402ed6459766ce9ae1e0e0f4。全部build_rc=0，mutant_test_rc=1，restored_baseline_rc=0；原始编辑串、出现序号、命令与UTC见evidence/mutations各JSON，原日志可按RAW-MANIFEST重建。

| 刀 | 修改文件 | 业务断言（首条；完整见JSON/日志） |
| --- | --- | --- |
| K01 | internal/mcpgw/oauth.go | dcr_http_test.go:142: DCR authorization did not enforce S256 |
| K02 | internal/mcpgw/dcr_policy.go | dcr_test.go:37: unsafe redirect accepted/wrong category "http://evil.example/callback": <nil> |
| K03 | internal/mcpgw/dcr_policy.go | dcr_http_test.go:285: unsafe registration 201: {"client_id":"dw-dcr-Lx2Pes7--lOkuCxpBEwA0Qk02d4lHrCs_HEvbrWo3yQ","client_id_expires_at":1795167202,"client_id_issued_at":1795080802,"client_name":"Local editor","grant_types":["authorization_code"],"redirect_uris":["http://127.0.0.1:43123/callback"],"response_types":["code"],"scope":"list_online send_message wait_for_messages ack","token_endpoint_auth_method":"none"} |
| K04 | internal/mcpgw/dcr.go | dcr_http_test.go:312: bad metadata bypassed persistent rate limit |
| K05 | internal/mcpgw/dcr_store.go | dcr_pg_test.go:158: unbound exact/past expiry accepted |
| K06 | internal/mcpgw/dcr_store.go | dcr_pg_test.go:175: successful token exchange did not renew bound TTL |
| K07 | internal/mcpgw/dcr_store.go | dcr_pg_test.go:187: principal bound client limit not enforced |
| K08 | internal/mcpgw/dcr_policy.go | dcr_pg_test.go:93: unbound TTL=72h0m0s |
| K09 | internal/mcpgw/dcr_store.go | dcr_pg_test.go:122: source rate lost across store restart <nil> |
| K10 | internal/mcpgw/dcr_store.go | dcr_pg_test.go:130: global rate not enforced <nil> |
| K11 | internal/mcpgw/dcr_store.go | dcr_pg_test.go:278: global live quota missing <nil> |
| K12 | internal/mcpgw/dcr_store.go | dcr_pg_test.go:261: per-source live quota missing <nil> |
| K13 | internal/mcpgw/dcr_store.go | dcr_pg_test.go:352: GC deleted a live unbound registration mcpgw: missing, expired or consumed record |
| K14 | internal/mcpgw/dcr_store.go | dcr_pg_test.go:329: client GC cascaded an unbounded child deletion |
| K15 | internal/mcpgw/dcr.go | dcr_test.go:102: source "" want "192.0.2.9": mcpgw: missing, expired or consumed record |
| K16 | internal/mcpgw/oauth.go | dcr_boundaries_test.go:58: unsafe actual redirect accepted http://127.0.0.1:/callback |
| K17 | internal/mcpgw/oauth.go | dcr_boundaries_test.go:58: unsafe actual redirect accepted http://127.0.0.1:/callback |
| K18 | internal/mcpgw/dcr_store.go | dcr_boundaries_test.go:150: DCR DB work was not bounded: elapsed=3.205596431s err=<nil> |
| K19 | internal/mcpgw/dcr.go | dcr_boundaries_test.go:212: slow registration body not bounded: elapsed=5.001755125s response="" err=read tcp 127.0.0.1:53842->127.0.0.1:42797: i/o timeout |
| K20 | internal/mcpgw/dcr.go | dcr_boundaries_test.go:197: early rejection blocked draining an unread body: response="" err=read tcp 127.0.0.1:57054->127.0.0.1:38949: i/o timeout |
| K21 | internal/mcpgw/dcr_policy.go | dcr_test.go:65: unsafe bound TTL accepted 2160h0m0.000000001s |
| K22 | internal/mcpgw/oauth.go | dcr_http_test.go:227: successful token exchange did not persist bound client |
| K23 | internal/mcpgw/dcr_policy.go | dcr_test.go:74: scheme log not bounded: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaprivate" |
| K24 | internal/mcpgw/dcr_store.go | dcr_pg_test.go:285: cleanup was not bounded to 256 |
| K25 | internal/mcpgw/dcr_store.go | dcr_pg_test.go:208: configured shorter bound TTL retained an obsolete long deadline |
| K26 | internal/mcpgw/dcr_store.go | dcr_pg_test.go:235: eviction retained a deadline beyond the remaining subject binding |
| K27 | internal/mcpgw/dcr_store.go | dcr_pg_test.go:361: GC deleted a live subject binding mcpgw: missing, expired or consumed record |
| K28 | internal/mcpgw/dcr_store.go | dcr_pg_test.go:165: valid in-flight grant lost on registration expiry mcpgw: missing, expired or consumed record |
| K29 | internal/mcpgw/oauth.go | dcr_http_test.go:213: failed PKCE exchange bound/renewed a client |
| K30 | internal/mcpgw/dcr.go | dcr_test.go:102: source "1.2.3.4" want "192.0.2.8": <nil> |
| K31 | internal/mcpgw/gateway.go | r61_auth_first_baseline_test.go:52: REAL_HTTP_AUTH_FIRST: status=400 challenge="" body={"error":"invalid_request"} |
| K32 | internal/mcpgw/dcr.go | dcr_boundaries_test.go:82: DCR shutdown allowed new authorization |
| K33 | internal/mcpgw/dcr_policy.go | dcr_test.go:37: unsafe redirect accepted/wrong category "https://127.0.0.1/cb": <nil> |
| K34 | internal/mcpgw/dcr_policy.go | dcr_test.go:37: unsafe redirect accepted/wrong category "codebuddy://oauth/callback?private=value": <nil> |
| K35 | internal/mcpgw/dcr.go | dcr_boundaries_test.go:176: DCR exceeded bounded processing slots |
| K36 | internal/mcpgw/dcr_store.go | dcr_pg_test.go:309: unbounded source bucket allocation <nil> |
| K37 | internal/mcpgw/store.go | dcr_pg_test.go:379: full DCR registry consumed the existing OAuth grant pool mcpgw: pending OAuth limit |
| K38 | internal/mcpgw/dcr_policy.go | dcr_http_test.go:300: redirect diagnostic scheme exceeded 32 bytes |
| K39 | internal/mcpgw/dcr_store.go | dcr_pg_test.go:238: untrusted/absent principal bound a client |
| K40 | internal/mcpgw/dcr.go | dcr_http_test.go:332: disabled DCR still registered clients |
| K41 | internal/mcpgw/dcr_policy.go | dcr_test.go:52: secret authentication method accepted |
