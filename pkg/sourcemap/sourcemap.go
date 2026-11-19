// Package sourcemap 提供 GX 源码与生成 Go 源码之间的双向位置映射。
// 本包无业务依赖，只处理「行/列」位置对。
package sourcemap

import (
	"encoding/json"
	"sort"
)

// Position 是 1 基的行列位置。
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Mapping 是一对生成位置 ↔ 原始位置。
type Mapping struct {
	Generated Position `json:"go"`
	Original  Position `json:"gx"`
}

// RawMapping 是 compiler emit 阶段产出的原始映射项。
type RawMapping struct {
	OutLine int
	OutCol  int
	InLine  int
	InCol   int
}

// SourceMap 持有按生成位置与原始位置分别排序的映射表，支持双向最近邻查询。
type SourceMap struct {
	File string
	gen  []Mapping // 按 Generated 升序
	orig []Mapping // 按 Original 升序
}

// Build 由原始映射项构造 SourceMap。
func Build(file string, raw []RawMapping) *SourceMap {
	gen := make([]Mapping, 0, len(raw))
	for _, r := range raw {
		gen = append(gen, Mapping{
			Generated: Position{Line: r.OutLine, Column: r.OutCol},
			Original:  Position{Line: r.InLine, Column: r.InCol},
		})
	}
	sort.SliceStable(gen, func(i, j int) bool {
		if gen[i].Generated.Line != gen[j].Generated.Line {
			return gen[i].Generated.Line < gen[j].Generated.Line
		}
		return gen[i].Generated.Column < gen[j].Generated.Column
	})
	orig := append([]Mapping{}, gen...)
	sort.SliceStable(orig, func(i, j int) bool {
		if orig[i].Original.Line != orig[j].Original.Line {
			return orig[i].Original.Line < orig[j].Original.Line
		}
		return orig[i].Original.Column < orig[j].Original.Column
	})
	return &SourceMap{File: file, gen: gen, orig: orig}
}

func leq(p Position, line, col int) bool {
	return p.Line < line || (p.Line == line && p.Column <= col)
}

func findLast(m []Mapping, byGenerated bool, line, col int) int {
	lo, hi := 0, len(m)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		p := m[mid].Generated
		if !byGenerated {
			p = m[mid].Original
		}
		if leq(p, line, col) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo - 1
}

// GoToGx 把生成 Go 的位置映射回 GX 位置。ok=false 表示映射为空。
// 采用「最近的前一个映射」；同一行时按列偏移做线性修正。
func (m *SourceMap) GoToGx(line, col int) (Position, bool) {
	if m == nil || len(m.gen) == 0 {
		return Position{}, false
	}
	i := findLast(m.gen, true, line, col)
	if i < 0 {
		return m.gen[0].Original, true
	}
	res := m.gen[i].Original
	if m.gen[i].Generated.Line == line {
		d := col - m.gen[i].Generated.Column
		if d > 0 && res.Column+d >= 1 {
			res.Column += d
		}
	}
	return res, true
}

// GxToGo 把 GX 位置映射到生成 Go 的位置。
func (m *SourceMap) GxToGo(line, col int) (Position, bool) {
	if m == nil || len(m.orig) == 0 {
		return Position{}, false
	}
	i := findLast(m.orig, false, line, col)
	if i < 0 {
		return m.orig[0].Generated, true
	}
	res := m.orig[i].Generated
	if m.orig[i].Original.Line == line {
		d := col - m.orig[i].Original.Column
		if d > 0 && res.Column+d >= 1 {
			res.Column += d
		}
	}
	return res, true
}

// Mappings 返回按生成位置排序的全部映射（只读副本）。
func (m *SourceMap) Mappings() []Mapping {
	if m == nil {
		return nil
	}
	return append([]Mapping{}, m.gen...)
}

// Len 返回映射条目数。
func (m *SourceMap) Len() int {
	if m == nil {
		return 0
	}
	return len(m.gen)
}

type jsonDoc struct {
	Version  int       `json:"version"`
	File     string    `json:"file"`
	Sources  []string  `json:"sources"`
	Mappings []Mapping `json:"mappings"`
}

// MarshalJSON 输出可读的 JSON 文档。
func (m *SourceMap) MarshalJSON() ([]byte, error) {
	doc := jsonDoc{Version: 1, File: m.File, Sources: []string{m.File}, Mappings: m.Mappings()}
	return json.MarshalIndent(doc, "", "  ")
}

// UnmarshalJSON 从 JSON 文档恢复 SourceMap。
func (m *SourceMap) UnmarshalJSON(data []byte) error {
	var doc jsonDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	m.File = doc.File
	raw := make([]RawMapping, 0, len(doc.Mappings))
	for _, mp := range doc.Mappings {
		raw = append(raw, RawMapping{
			OutLine: mp.Generated.Line, OutCol: mp.Generated.Column,
			InLine: mp.Original.Line, InCol: mp.Original.Column,
		})
	}
	built := Build(doc.File, raw)
	m.gen, m.orig = built.gen, built.orig
	return nil
}
