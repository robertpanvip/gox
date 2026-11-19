// Package list 是 GX 集合链式调用 map/filter/reduce 的运行时。
//
// GX 里写 xs.map(f)、xs.filter(f)、xs.reduce(init, f)（小写方法式），转译器
// 把它们首字母大写还原为 List 上的方法调用。回调是 Go 原生闭包，原样透传。
//
// 依赖 Go 1.27 的泛型方法：方法可声明自己的类型参数，因此 Map 可以换型
// （T → R）并保持方法链式，例如 nums.Map(strconv.Itoa).Filter(...)。
package list

// List 是一个带链式方法的切片类型。它与 []E 双向可直接赋值/传参，
// 因为 []E 是无名类型、List[E] 是命名类型且底层类型相同。
type List[E any] []E

// Of 把任意切片适配为 List，便于对普通 []T 使用链式方法。
func Of[E any](xs []E) List[E] { return List[E](xs) }

// Slice 把 List 转回普通切片。
func Slice[E any](l List[E]) []E { return []E(l) }

// Map 把每个元素交给 f 变换，结果元素类型可不同（E → R）。
func (l List[E]) Map[R any](f func(E) R) List[R] {
	out := make(List[R], 0, len(l))
	for _, x := range l {
		out = append(out, f(x))
	}
	return out
}

// Filter 保留 f 返回 true 的元素（同型）。
func (l List[E]) Filter(f func(E) bool) List[E] {
	out := make(List[E], 0, len(l))
	for _, x := range l {
		if f(x) {
			out = append(out, x)
		}
	}
	return out
}

// Reduce 用 f 从左到右折叠；累加器类型 R 可与元素类型 E 不同。
func (l List[E]) Reduce[R any](init R, f func(acc R, x E) R) R {
	acc := init
	for _, x := range l {
		acc = f(acc, x)
	}
	return acc
}

// Each 对每个元素调用 f（无返回）。
func (l List[E]) Each(f func(E)) {
	for _, x := range l {
		f(x)
	}
}

// Len 返回元素个数。
func (l List[E]) Len() int { return len(l) }
