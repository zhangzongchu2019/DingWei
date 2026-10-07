package nodeagent

import (
	"os"
	"regexp"
	"testing"
)

// R-V2-IMAGE-R4-IMPL：节点刷新凭据时删的标记名，与镜像 r4 start.py 读写的标记名必须是同一个文件名——
// 两侧漂移不会编译错，只会让「刷新后下次启动必重写」那条静默失效（start.py 看不见标记被删）。
func TestAppliedMarkerNameMatchesHostedImageStart(t *testing.T) {
	src, err := os.ReadFile("../../deploy/ops/hosted-image/start.py")
	if err != nil {
		t.Fatalf("读不到镜像 start.py：%v —— 查不了不等于通过", err)
	}
	m := regexp.MustCompile(`(?m)^MARKER_NAME = '([^']+)'$`).FindSubmatch(src)
	if m == nil {
		t.Fatal("start.py 里找不到 MARKER_NAME")
	}
	if got := string(m[1]); got != appliedMarkerName {
		t.Fatalf("start.py MARKER_NAME=%q 与节点 appliedMarkerName=%q 不一致", got, appliedMarkerName)
	}
}
