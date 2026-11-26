package complete

import (
	"sync"

	"github.com/robertpanvip/gox/pkg/gxcomplete"
)

// MemberProvider 用 go/types 做点号成员补全（obj. / pkg. 弹成员）。
// 数据源是主仓库的 gxcomplete 公共包（gox-tui 是独立 module，无法 import internal/*）。
// 会话惰性构建、异步预热，Provide 不会因首次类型检查而长时间卡 UI。
type MemberProvider struct {
	once sync.Once
	sess *gxcomplete.Session
}

func NewMemberProvider() *MemberProvider { return &MemberProvider{} }

func (p *MemberProvider) Name() string { return "member" }

func (p *MemberProvider) get() *gxcomplete.Session {
	p.once.Do(func() { p.sess = gxcomplete.New() })
	return p.sess
}

// Warm 异步预热类型图，可在构建引擎时调用。
func (p *MemberProvider) Warm() { p.get().Warm() }

func (p *MemberProvider) Provide(q Query) []Candidate {
	if !q.Member {
		return nil
	}
	cands := p.get().MembersAt(q.BufferText, q.LineNo, q.Col)
	if len(cands) == 0 {
		return nil
	}
	out := make([]Candidate, 0, len(cands))
	for _, c := range cands {
		out = append(out, Candidate{
			Text:   c.Name,
			Label:  c.Name,
			Detail: c.Kind + " · " + c.Signature,
			Kind:   KindMember,
		})
	}
	return out
}
