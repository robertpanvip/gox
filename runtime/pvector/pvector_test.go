package pvector

import (
	"math/rand"
	"reflect"
	"testing"
)

func TestEmptyAndZeroValue(t *testing.T) {
	var v PVector[int]
	if v.Len() != 0 {
		t.Fatalf("零值 Len=%d", v.Len())
	}
	if got := v.Values(); len(got) != 0 {
		t.Fatalf("零值 Values=%v", got)
	}
	_ = v.Append(1) // 不应 panic
}

func TestAppendSequential(t *testing.T) {
	v := PVector[int]{}
	for i := 0; i < 5000; i++ {
		v = v.Append(i)
		if v.Len() != i+1 {
			t.Fatalf("Len after append %d = %d", i, v.Len())
		}
	}
	got := v.Values()
	want := make([]int, 5000)
	for i := range want {
		want[i] = i
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("顺序 append 5000 元素不一致，首差异: got[0]=%d got[1024]=%d got[4999]=%d",
			got[0], got[1024], got[4999])
	}
}

// 覆盖 tail 满、推入 trie、跨 shift 提升（1024/1025）等所有边界。
func TestBoundarySizes(t *testing.T) {
	for _, n := range []int{1, 31, 32, 33, 63, 64, 65, 1023, 1024, 1025, 1088, 32768, 32769} {
		v := PVector[int]{}
		for i := 0; i < n; i++ {
			v = v.Append(i)
		}
		got := v.Values()
		if len(got) != n {
			t.Fatalf("n=%d: len=%d", n, len(got))
		}
		for i := 0; i < n; i++ {
			if got[i] != i {
				t.Fatalf("n=%d: Values[%d]=%d", n, i, got[i])
			}
			if v.Get(i) != i {
				t.Fatalf("n=%d: Get(%d)=%d", n, i, v.Get(i))
			}
		}
	}
}

// 每次 Append 后，此前所有版本都必须保持原值（持久化不可变）。
func TestImmutabilityAcrossAppends(t *testing.T) {
	const n = 200
	versions := make([]PVector[int], n)
	v := PVector[int]{}
	for i := 0; i < n; i++ {
		versions[i] = v
		v = v.Append(i)
	}
	for i, snap := range versions {
		if snap.Len() != i {
			t.Fatalf("版本 %d 长度被改动: %d", i, snap.Len())
		}
		for j := 0; j < i; j++ {
			if snap.Get(j) != j {
				t.Fatalf("版本 %d 的元素 %d 被改动: %d", i, j, snap.Get(j))
			}
		}
	}
}

func TestSetImmutability(t *testing.T) {
	v := PVector[int]{}
	for i := 0; i < 300; i++ {
		v = v.Append(i)
	}
	old := v
	v2 := v.Set(5, 555).Set(100, 1000).Set(299, 2999)
	if v.Get(5) != 5 || v.Get(100) != 100 || v.Get(299) != 299 {
		t.Fatal("Set 修改了原向量")
	}
	if v2.Get(5) != 555 || v2.Get(100) != 1000 || v2.Get(299) != 2999 {
		t.Fatalf("Set 结果错误: %d %d %d", v2.Get(5), v2.Get(100), v2.Get(299))
	}
	if !reflect.DeepEqual(old.Values(), v.Values()) {
		t.Fatal("Set 后旧版本 Values 变化")
	}
}

// 随机 Append + Set 差分对照 []int，每步全量比对。
func TestRandomAgainstSliceModel(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	var model []int
	v := PVector[int]{}
	for step := 0; step < 4000; step++ {
		if len(model) == 0 || rng.Intn(100) < 65 {
			x := rng.Intn(1 << 20)
			v = v.Append(x)
			model = append(model, x)
		} else {
			i := rng.Intn(len(model))
			x := rng.Intn(1 << 20)
			v = v.Set(i, x)
			model[i] = x
		}
		if v.Len() != len(model) {
			t.Fatalf("step %d: Len=%d want=%d", step, v.Len(), len(model))
		}
		if !reflect.DeepEqual(v.Values(), model) {
			t.Fatalf("step %d: Values 与模型不一致", step)
		}
		// 抽样 Get
		for k := 0; k < 5 && len(model) > 0; k++ {
			i := rng.Intn(len(model))
			if v.Get(i) != model[i] {
				t.Fatalf("step %d: Get(%d)=%d want=%d", step, i, v.Get(i), model[i])
			}
		}
	}
}

func TestOf(t *testing.T) {
	in := []string{"a", "b", "c", "d"}
	v := Of(in)
	if !reflect.DeepEqual(v.Values(), in) {
		t.Fatalf("Of: %v", v.Values())
	}
	in[0] = "changed"
	if v.Get(0) != "a" {
		t.Fatal("Of 应与输入切片解耦")
	}
}

func TestPanicOutOfRange(t *testing.T) {
	v := Of([]int{1, 2, 3})
	for _, f := range []func(){
		func() { v.Get(-1) },
		func() { v.Get(3) },
		func() { v.Set(-1, 0) },
		func() { v.Set(3, 0) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("越界应 panic")
				}
			}()
			f()
		}()
	}
}
