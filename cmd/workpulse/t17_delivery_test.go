package main

// T17 §K1/C3(Hub 加性改动③):webhook 条件严格分支——含 v=严格,无 v=legacy 原样。

import "testing"

func TestVerifyVersionedDelivery(t *testing.T) {
	cases := []struct {
		name string
		data string
		ok   bool
	}{
		{"无v=legacy放行", `{"schema":"2.0","header":{"token":"x"},"whatever":1}`, true},
		{"v1合法", `{"v":1,"token":"k","msg_id":"om_1","chat_type":"group","chat_id":"oc","open_id":"","sender_open_id":"ou","text":"hi","raw":{"text":"hi"},"ts":1690000000}`, true},
		{"v1挑战探针", `{"v":1,"token":"k","challenge":"t17-prearm"}`, true},
		{"v2拒", `{"v":2,"token":"k","msg_id":"om_1"}`, false},
		{"v1未知字段拒", `{"v":1,"token":"k","msg_id":"om_1","hax":true}`, false},
	}
	for _, c := range cases {
		err := verifyVersionedDelivery([]byte(c.data))
		if (err == nil) != c.ok {
			t.Errorf("%s: err=%v want ok=%v", c.name, err, c.ok)
		}
	}
}
