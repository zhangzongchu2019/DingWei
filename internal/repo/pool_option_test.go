package repo

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// 甲域 sol CR #6 的反测,两半各钉一条 —— 缺任何一半都会让「已收口」成假:
//
//	① 收口成立:Option 拿到的具体类型不是裸句柄(否则回调可把它搬出包外);
//	② 语义未丢:MaxOpenConns(1) 仍真的落到池上(§4.5——收编不得抹掉用事故换来的并发语义)。
//
// 🔴 只做 ① 会把「换了个类型名」当成收口;只做 ② 会把「功能还在」当成安全。
func TestOptionNeverHandsOutRawHandle(t *testing.T) {
	var captured PoolTuner
	dsn := "file:" + filepath.Join(t.TempDir(), "opt.db")

	p, err := OpenSQLite(context.Background(), dsn,
		MaxOpenConns(1),
		func(pt PoolTuner) { captured = pt },
	)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer func() { _ = p.Close() }()

	if captured == nil {
		t.Fatal("Option 未被调用,本反测什么也没测到")
	}

	// ① 包外回调拿不到裸句柄:断言必须失败。
	if db, ok := captured.(*sql.DB); ok {
		t.Fatalf("Option 收到裸 *sql.DB(%p),句柄已出仓储包", db)
	}
	// 注:*sql.Conn / *sql.Tx 连 PoolTuner 都不满足,编译期即不可能传出,无需运行期反测。
	// 唯一【编译得过】因而必须运行期钉住的就是 *sql.DB —— 它恰好满足 PoolTuner,
	// 所以后人一句 o(db) 就能把这条收口悄悄改回去,而签名一字不变。

	// ② 池参数确实落地了,不是只换了个签名。
	if got := p.db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConns(1) 未落到池上:MaxOpenConnections=%d", got)
	}
}
