# TemplateResult 设计文档（lit-html 风格）

## 核心设计

完全照搬 lit-html 的设计思想，使用 **StaticCode + Factory + Dynamic** 三段式结构。

## TemplateResult 结构

```go
type TemplateResult struct {
    StaticCode []string          // 静态代码片段（用于比较模板是否相同）
    Static     []func() Component // 组件工厂函数（闭包捕获动态值）
    hash       string            // StaticCode 的 hash 值（用于快速比较）
}
```

### 字段说明

1. **StaticCode** (`[]string`)
   - 静态的 HTML/组件结构片段
   - 用于生成 hash 值，快速判断模板是否相同
   - 示例：[`<Counter>`, `</Counter>`]

2. **Static** (`[]func() Component`)
   - 组件工厂函数数组
   - 每个函数通过闭包捕获动态值，返回 Component
   - 延迟创建组件，避免不必要的对象分配
   - 示例：`func() Component { return NewButton(ButtonProps{Text: fmt.Sprintf("Count: %d", count)}) }`

3. **hash** (`string`)
   - StaticCode 的哈希值
   - 用于快速比较模板是否相同
   - 自动计算，不需要手动设置

---

## 使用示例

### 1. 基础示例

```go
func Counter(props CounterProps) gui.TemplateResult {
    return gui.TemplateResult{
        StaticCode: []string{
            `<Counter>`,
            `</Counter>`,
        },
        Static: []func() gui.Component{
            func() gui.Component {
                // 闭包捕获 props.Count
                return gui.NewButton(gui.ButtonProps{
                    Text: fmt.Sprintf("Count: %d", props.Count),
                })
            },
        },
    }
}
```

### 2. 多个动态值

```go
func UserProfile(props UserProfileProps) gui.TemplateResult {
    return gui.TemplateResult{
        StaticCode: []string{
            `<div class="user-profile">`,
            `</div>`,
        },
        Static: []func() gui.Component{
            func() gui.Component {
                // 闭包捕获 props.Name 和 props.Bio
                return gui.NewDiv(gui.DivProps{
                    ClassName: "user-profile",
                    Children: []gui.Component{
                        gui.NewHeading(gui.HeadingProps{
                            Text: props.Name,
                        }),
                        gui.NewParagraph(gui.ParagraphProps{
                            ClassName: "bio",
                            Text: props.Bio,
                        }),
                    },
                })
            },
        },
    }
}
```

### 3. 嵌套组件

```go
func App() gui.TemplateResult {
    return gui.TemplateResult{
        StaticCode: []string{
            `<App>`,
            `</App>`,
        },
        Static: []func() gui.Component{
            func() gui.Component {
                return gui.NewDiv(gui.DivProps{
                    ClassName: "app",
                    Children: []gui.Component{
                        gui.WrapTemplateResult(Counter(CounterProps{
                            InitialCount: 0,
                        })),
                    },
                })
            },
        },
    }
}
```

---

## 渲染流程

### 首次渲染

```go
func (f *FxWrapper) Render(screen *ebiten.Image) {
    newTemplate := f.componentFunc()
    
    // 第一次渲染，needRebuild = true
    if needRebuild {
        f.lastTemplate = &newTemplate
        f.components = make([]Component, len(newTemplate.Static))
        
        // 调用工厂函数创建组件（不需要传参数）
        for i, factory := range newTemplate.Static {
            f.components[i] = factory()
        }
        
        // 渲染所有组件
        for _, comp := range f.components {
            comp.Render(screen)
        }
    } else {
        // Hash 相同，但仍然需要重新调用工厂函数
        // 因为闭包捕获的值可能已经变化了
        for i, factory := range newTemplate.Static {
            f.components[i] = factory()
        }
        
        // 渲染所有组件
        for _, comp := range f.components {
            comp.Render(screen)
        }
    }
}
```

---

## Hash 计算

```go
func (t *TemplateResult) computeHash() {
    if len(t.StaticCode) == 0 {
        t.hash = ""
        return
    }
    
    // 简单的 hash：拼接所有字符串后取长度和首尾字符
    totalLen := 0
    for _, code := range t.StaticCode {
        totalLen += len(code)
    }
    
    // 使用第一个和最后一个字符 + 总长度作为简单 hash
    if totalLen > 0 {
        first := t.StaticCode[0]
        last := t.StaticCode[len(t.StaticCode)-1]
        t.hash = fmt.Sprintf("%d_%c_%c", totalLen, first[0], last[len(last)-1])
    }
}
```

### Hash 比较的优势

1. **快速**：只需要比较一个字符串
2. **自动**：不需要手动写 TypeTag
3. **稳定**：相同的 StaticCode 总是生成相同的 hash

---

## 与 lit-html 的对比

| 特性 | lit-html | 我们的实现 |
|------|----------|-----------|
| **静态部分** | `strings: ['<div>', '</div>']` | `StaticCode: []string{...}` |
| **动态部分** | `values: [cls, text]` | **闭包捕获** |
| **创建组件** | 内部 DOM API | `Static: []func() Component` |
| **模板比较** | `strings === oldStrings` | `hash === oldHash` |
| **更新机制** | 更新 Part 的值 | 重新调用工厂函数 |

---

## 优势

1. **完全照搬 lit-html**：核心思想一致
2. **自动化**：不需要手动写 TypeTag，hash 自动计算
3. **高效**：hash 比较非常快
4. **函数式**：组件是纯函数，符合现代前端框架风格
5. **灵活**：工厂函数可以包含任意逻辑

---

## TODO

1. **优化 hash 算法**：可以使用更复杂的 hash（如 FNV-1a 或 SHA256）

2. **智能更新**：虽然需要重新调用工厂函数，但可以优化组件的 Render 方法，只渲染真正变化的部分

3. **Transformer 支持**：自动生成 StaticCode、Static 工厂函数

---

## 总结

这个设计完全遵循 lit-html 的核心思想：
- **StaticCode** 对应 lit-html 的 `strings`
- **Dynamic** 对应 lit-html 的 `values`
- **Hash 比较** 对应 lit-html 的 `strings === oldStrings`
- **工厂函数** 是我们的创新，用于延迟创建组件
