package main

import (
	"fmt"
	"strings"
)

// portAttempts 是起库时【只在端口被抢】这一种失败上换端口重试的次数上限（含首次）。
//
// 🔴 为什么要重试（推翻 pickFreePort 原注释里「不做重试」那个决定，理由如下，不是顺手改）：
//
//	pickFreePort 是「向内核要一个空闲端口 → 关掉监听 → 把号交给 PG」，关监听到 PG bind
//	之间有一个窗口。原注释说「端口被抢时 PG 启动会直接失败并报端口号，比静默重试更容易定位」——
//	这句话在【只有本进程会用这个端口】时成立，实测不成立的场景是：
//	  · 两个 pgtest 消费方并行时本席自己撞上（port 46399，见板件
//	    DELIVERY-G2-FANOUT-DURABILITY-…20261002.md 第七节）；红的是【与端口无关的用例】，
//	    排障的人第一反应是怀疑自己刚改的代码 —— 这比"慢三轮"坏得多。
//	  · 纪律「一台机器同时只跑一个 pgtest 消费方」已立（裁定 b855bb4da7d1），
//	    🔴 但纪律不是判据：它约束不了 CI 上别的 job、别的容器、宿主上别的进程恰好占走那个号。
//	⇒ 原注释担心的「静默」由下面 notify 解决：每次换端口都报一条 PORT_RETRY=<旧>-><新>，
//	  并且失败文案会说清「连续几次、每次都是端口被占、最后用的哪个号」。重试不静默，
//	  于是「更容易定位」这个理由不再与「不要让无关用例红」冲突。
//
// 🔴 上限只有 3 次而不是「一直重试到成功」：无限重试会把【真失败】伪装成慢。
//
//	端口被抢是 transient（换一个号就好）；「二进制解压失败」「initdb 失败」「磁盘满」不是 ——
//	那些必须立刻失败并把原因原样交回来。分辨它们的就是下面的 isPortTaken。
const portAttempts = 3

// portTakenSignatures 是「端口已被别人占用」的两种真实文案，逐字取自
// github.com/fergusstrange/embedded-postgres v1.34.0（本目录 go.mod 钉的版本）：
//
//	① embedded_postgres.go:74 的 Start() 上来先调 ensurePortAvailable，
//	   它在 :244-252 自己 net.Listen 一次，失败即返回 :247 的
//	   "process already listening on port %d"。🔴 抢占窗口里这是【最常命中】的一条 ——
//	   别人已经绑上了，库连 pg_ctl 都不会去调。
//	② 若端口是在①通过之后、PG 真 bind 之前才被抢走，pg_ctl 失败，:224 把 PG 日志原文
//	   包进错误返回，日志里是 "could not bind IPv4 address \"127.0.0.1\": Address already in use"。
//
// 🔴 只认这两条，判据刻意窄：宽一点（例如认所有含 "port" 的错误）就会把「端口号非法」
// 「权限不足」也算成抢占，于是重试三次、每次都失败、报的还是同一个真因 —— 只是慢了三轮，
// 读数里还多三条误导性的 PORT_RETRY。两种签名分别有格钉住（portretry_test.go），
// 少认一条即红。
var portTakenSignatures = []string{
	"address already in use",            // ②（PG 日志原文，大小写按内核给的是首字母大写，故统一小写后比）
	"process already listening on port", // ①（库自己的前置检查）
}

// isPortTaken 判一个起库失败是不是「端口已被别人占用」这一种。
func isPortTaken(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, sig := range portTakenSignatures {
		if strings.Contains(msg, sig) {
			return true
		}
	}
	return false
}

// startWithPortRetry 起库，只在【端口被抢】时换端口重试。
//
// 四个行为参数全部可注入（与 reap_loop.go 同一手法），于是这些格不需要真起 PG：
//
//	start  用给定端口起一次库；返回 nil＝成功
//	pick   再要一个空闲端口（第 2 次及以后才调）
//	notify 每次换端口时报出去（生产是 emit 两条协议行，用例里收集来断言）
//
// 🔴 返回【最终用的那个端口】，调用方必须用它拼 DSN：
//
//	否则重试成功了而 DSN 还指着旧端口 —— 那是「重试了但没用上」，比不重试更坏：
//	不重试至少失败得干脆，这种情况会去连一个【别人的】库（或连不上而报一个看不懂的错），
//	而读数上看不出来。这一条有格钉住（返回值断言新端口，不是原端口）。
//
// 🔵 START_SECONDS 在生产侧覆盖【全部尝试】的耗时，不是最后一次尝试的：那个数是
//
//	「拿到一个能用的库花了多久」，重试真的发生过就该体现出来（PORT_RETRY 同时也在）。
func startWithPortRetry(port uint, attempts int, start func(uint) error, pick func() (uint, error), notify func(from, to uint)) (uint, error) {
	if attempts < 1 {
		return port, fmt.Errorf("起 PG: 尝试次数上限 %d 非法（至少 1）", attempts)
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			next, err := pick()
			if err != nil {
				return port, fmt.Errorf("换端口重试（第 %d 次尝试）时挑端口失败: %w", attempt, err)
			}
			notify(port, next)
			port = next
		}
		lastErr = start(port)
		if lastErr == nil {
			return port, nil
		}
		// 🔴 不是端口被抢 ⇒ 立刻失败、原样交回，不重试（不拿重试掩盖真因）。
		if !isPortTaken(lastErr) {
			return port, lastErr
		}
	}
	return port, fmt.Errorf("连续 %d 次尝试都撞上端口被占（最后一次用的端口 %d）: %w", attempts, port, lastErr)
}
