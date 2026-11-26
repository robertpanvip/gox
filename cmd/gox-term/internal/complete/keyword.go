package complete

import "strings"

// KeywordProvider 内置高频 shell 命令、子命令与 GX 关键字/片段，开箱即用，
// 不依赖任何外部工具——即使在一台干净的机器上也有候选可弹。
type KeywordProvider struct {
	items []Candidate
}

type kw struct {
	text, label, detail string // text 即采纳时插入的文本
}

var builtinKeywords = []kw{
	// shell 常用
	{"cd", "cd", "进入目录"}, {"cd ~", "cd ~", "回家目录"},
	{"ls", "ls", "列出文件"}, {"ls -la", "ls -la", "列出全部（含隐藏）"},
	{"pwd", "pwd", "当前路径"}, {"clear", "clear", "清屏"},
	{"echo", "echo", "输出文本"}, {"cat", "cat", "查看文件"},
	{"grep", "grep", "文本搜索"}, {"find", "find", "查找文件"},
	{"mkdir", "mkdir", "建目录"}, {"rm", "rm", "删除"},
	{"cp", "cp", "复制"}, {"mv", "mv", "移动"},
	{"touch", "touch", "建空文件"}, {"head", "head", "头部"}, {"tail", "tail", "尾部"},
	// git
	{"git status", "git status", "查看状态"}, {"git add .", "git add .", "暂存全部"},
	{"git commit -m ", "git commit -m", "提交"}, {"git push", "git push", "推送"},
	{"git pull --rebase", "git pull --rebase", "变基拉取"}, {"git log --oneline", "git log --oneline", "简洁日志"},
	{"git diff", "git diff", "差异"}, {"git branch", "git branch", "分支"},
	// go
	{"go build ./...", "go build ./...", "编译全部"}, {"go test ./...", "go test ./...", "测试全部"},
	{"go run .", "go run .", "运行"}, {"go vet ./...", "go vet ./...", "静态检查"},
	{"go mod tidy", "go mod tidy", "整理依赖"},
	// GX 语言关键字/片段
	{"sig ", "sig", "信号声明"}, {"func ", "func", "函数"},
	{"return ", "return", "返回"}, {"for ", "for", "循环"},
	{"if ", "if", "条件"}, {"view ", "view", "视图"},
}

func NewKeywordProvider() *KeywordProvider {
	p := &KeywordProvider{items: make([]Candidate, 0, len(builtinKeywords))}
	for _, k := range builtinKeywords {
		p.items = append(p.items, Candidate{
			Text: k.text, Label: k.label, Detail: k.detail, Kind: KindKeyword,
		})
	}
	return p
}

func (p *KeywordProvider) Name() string { return "keyword" }

// 常见命令的子命令表：Prev 命中时，优先补子命令而不是无关的系统命令。
var subcommands = map[string][]kw{
	"git": {
		{"status", "status", "查看工作区状态"}, {"add .", "add .", "暂存全部"},
		{"commit -m ", "commit -m", "提交"}, {"push", "push", "推送"},
		{"pull --rebase", "pull --rebase", "变基拉取"}, {"log --oneline", "log --oneline", "简洁日志"},
		{"checkout ", "checkout", "切换分支"}, {"branch", "branch", "分支列表"},
		{"diff", "diff", "查看差异"}, {"merge ", "merge", "合并分支"},
		{"reset --hard", "reset --hard", "重置"}, {"stash", "stash", "暂存改动"},
	},
	"go": {
		{"build ./...", "build ./...", "编译全部"}, {"test ./...", "test ./...", "测试全部"},
		{"run .", "run .", "运行"}, {"vet ./...", "vet ./...", "静态检查"},
		{"mod tidy", "mod tidy", "整理依赖"}, {"fmt ./...", "fmt ./...", "格式化"},
		{"get ", "get", "拉取依赖"}, {"env", "env", "环境信息"},
	},
	"docker": {
		{"ps", "ps", "容器列表"}, {"run ", "run", "运行容器"}, {"build ", "build", "构建镜像"},
		{"exec ", "exec", "进入容器"}, {"logs ", "logs", "查看日志"}, {"images", "images", "镜像列表"},
		{"compose up", "compose up", "编排启动"}, {"compose down", "compose down", "编排停止"},
	},
	"npm": {
		{"install", "install", "安装依赖"}, {"run ", "run", "运行脚本"}, {"test", "test", "测试"},
		{"start", "start", "启动"}, {"init", "init", "初始化项目"},
	},
	"kubectl": {
		{"get pods", "get pods", "查看 Pod"}, {"get svc", "get svc", "查看服务"},
		{"apply -f ", "apply -f", "应用配置"}, {"describe ", "describe", "查看详情"},
		{"logs ", "logs", "查看日志"}, {"exec ", "exec", "执行命令"},
	},
	"cargo": {
		{"build", "build", "编译"}, {"test", "test", "测试"}, {"run", "run", "运行"},
		{"check", "check", "检查"}, {"add ", "add", "添加依赖"},
	},
}

func (p *KeywordProvider) Provide(q Query) []Candidate {
	if q.Token == "" {
		return nil // 空查询交给 PATH/文件来源，避免一堆关键字糊屏
	}
	tl := strings.ToLower(q.Token)

	// 上下文子命令优先：git st → status / stash。
	if subs, ok := subcommands[strings.ToLower(q.Prev)]; ok {
		out := make([]Candidate, 0, len(subs))
		for i, k := range subs {
			if strings.HasPrefix(strings.ToLower(k.label), tl) || subseqMatch(strings.ToLower(k.label), tl) {
				out = append(out, Candidate{
					Text: k.text + " ", Label: k.label, Detail: q.Prev + " · " + k.detail, Kind: KindKeyword,
					// 上下文子命令优先于系统命令；表内越靠前（越常用）加权越高。
					Score: 300 + len(subs) - i,
				})
			}
		}
		if len(out) > 0 {
			return out // 有子命令命中就不再掺入全局表，避免噪声
		}
	}

	out := make([]Candidate, 0, 16)
	for _, c := range p.items {
		if strings.Contains(strings.ToLower(c.Label), tl) || subseqMatch(strings.ToLower(c.Label), tl) {
			out = append(out, c)
		}
	}
	return out
}
