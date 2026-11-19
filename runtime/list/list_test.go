package list

import (
	"reflect"
	"strconv"
	"testing"
)

func TestMapTypeChange(t *testing.T) {
	nums := List[int]{1, 2, 3}
	got := nums.Map(strconv.Itoa)
	if !reflect.DeepEqual([]string(got), []string{"1", "2", "3"}) {
		t.Fatalf("Map(strconv.Itoa) = %v", []string(got))
	}
}

func TestFilter(t *testing.T) {
	nums := List[int]{1, 2, 3, 4, 5, 6}
	got := nums.Filter(func(x int) bool { return x%2 == 0 })
	if !reflect.DeepEqual([]int(got), []int{2, 4, 6}) {
		t.Fatalf("Filter(even) = %v", []int(got))
	}
}

func TestReduceTypeChange(t *testing.T) {
	nums := List[int]{1, 2, 3}
	// 累加器类型 R=string，与元素类型 E=int 不同（泛型方法换型）
	got := nums.Reduce("", func(acc string, x int) string {
		return acc + strconv.Itoa(x)
	})
	if got != "123" {
		t.Errorf("Reduce(string) = %q, want 123", got)
	}
}

func TestReduceSameType(t *testing.T) {
	nums := List[int]{1, 2, 3, 4}
	got := nums.Reduce(0, func(acc, x int) int { return acc + x })
	if got != 10 {
		t.Errorf("Reduce(sum) = %d, want 10", got)
	}
}

func TestChainMapFilter(t *testing.T) {
	// 换型链式：int → string → filter
	nums := List[int]{1, 2, 3, 4}
	got := nums.Map(strconv.Itoa).Filter(func(s string) bool { return s != "2" })
	if !reflect.DeepEqual([]string(got), []string{"1", "3", "4"}) {
		t.Fatalf("Map.Filter = %v", []string(got))
	}
}

func TestChainFilterReduce(t *testing.T) {
	nums := List[int]{1, 2, 3, 4, 5, 6}
	got := nums.Filter(func(x int) bool { return x%2 == 0 }).Reduce(0, func(a, b int) int { return a + b })
	if got != 12 {
		t.Errorf("Filter.Reduce = %d, want 12", got)
	}
}

func TestEmptyList(t *testing.T) {
	var empty List[int]
	if empty.Len() != 0 {
		t.Errorf("empty.Len() = %d", empty.Len())
	}
	if got := empty.Reduce(99, func(a, b int) int { return a + b }); got != 99 {
		t.Errorf("empty.Reduce = %d, want init 99", got)
	}
	if got := empty.Map(strconv.Itoa); len(got) != 0 {
		t.Errorf("empty.Map = %v", got)
	}
	called := false
	empty.Each(func(int) { called = true })
	if called {
		t.Error("empty.Each 不应调用回调")
	}
}

func TestOfAndSlice(t *testing.T) {
	raw := []int{1, 2, 3}
	l := Of(raw)
	if l.Len() != 3 {
		t.Fatalf("Of.Len() = %d", l.Len())
	}
	// List 与 []T 底层相同，Of/Slice 是零拷贝适配
	back := Slice(l)
	if !reflect.DeepEqual(back, raw) {
		t.Fatalf("Slice(Of(raw)) = %v", back)
	}
	// 修改通过底层数组可见（验证零拷贝语义）
	back[0] = 100
	if raw[0] != 100 {
		t.Error("Of/Slice 应为零拷贝适配")
	}
}

func TestEach(t *testing.T) {
	nums := List[int]{1, 2, 3}
	sum := 0
	nums.Each(func(x int) { sum += x })
	if sum != 6 {
		t.Errorf("Each sum = %d, want 6", sum)
	}
}
