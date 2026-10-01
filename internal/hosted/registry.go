package hosted

// 装配② · 静态登记表：CombosSource 与 ImageCatalog 的文件驱动实现。
//
// 🔴 fail-closed 纪律：
//   · 验通组合表（B12 逐家验通制）：文件缺失/为空 ⇒ 空表＝一切组合 combo_incompatible——
//     验通结果回板前新建界面无一组合可选，比"默认可选"安全；
//   · 镜像版本登记（G1 挂接）：无登记 ⇒ Latest 返回 ok=false ⇒ 创建即拒（不编造镜像）；
//   · 文件存在但畸形 ⇒ 启动期报错（Load 上抛），不带着半截表跑。
//
// 文件格式（JSON）：
//   combos: [{"cli":"codex","llm":"gpt","cred_kind":"token_file","verified":true}, ...]
//   images: {"codex":"hub.local/dingwei/base-codex@sha256:<64hex>", ...}

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// StaticCombos 文件驱动的验通组合表。
type StaticCombos struct{ combos []Combo }

// LoadCombos 读组合表；path 空 ⇒ 空表（合法，全不兼容）；文件在但畸形 ⇒ 报错。
func LoadCombos(path string) (*StaticCombos, error) {
	if path == "" {
		return &StaticCombos{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("combos 表读取: %v", err)
	}
	var rows []Combo
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("combos 表畸形: %v", err)
	}
	for _, c := range rows {
		if c.CLI == "" || c.LLM == "" || (c.CredKind != "api_key" && c.CredKind != "token_file") {
			return nil, fmt.Errorf("combos 表行畸形: %+v", c)
		}
	}
	return &StaticCombos{combos: rows}, nil
}

func (s *StaticCombos) Lookup(cli, llm, credKind string) (bool, bool) {
	for _, c := range s.combos {
		if c.CLI == cli && c.LLM == llm && c.CredKind == credKind {
			return c.Verified, true
		}
	}
	return false, false
}

func (s *StaticCombos) List() []Combo { return append([]Combo(nil), s.combos...) }

// StaticImages 文件驱动的镜像版本登记（cli → pinned ref；ref 必须是 仓库@sha256:64hex 形）。
type StaticImages struct{ m map[string]string }

func LoadImages(path string) (*StaticImages, error) {
	if path == "" {
		return &StaticImages{m: map[string]string{}}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("镜像登记读取: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("镜像登记畸形: %v", err)
	}
	for cli, ref := range m {
		// 形状闸：必须带 sha256 digest（与节点侧 validateImageRef 同一条纪律的 Hub 半段）
		parts := strings.Split(ref, "@")
		if len(parts) != 2 || !strings.HasPrefix(parts[1], "sha256:") || len(parts[1]) != 7+64 {
			return nil, fmt.Errorf("镜像登记 %q 的 ref 必须是 仓库@sha256:64hex: %q", cli, ref)
		}
	}
	return &StaticImages{m: m}, nil
}

func (s *StaticImages) Latest(cli string) (string, bool) {
	ref, ok := s.m[cli]
	return ref, ok
}
