package gox

// Signal 响应式信号（基础类型）
type Signal[T any] struct {
	value T
}

// New 创建新的 Signal
func New[T any](initialValue T) *Signal[T] {
	return &Signal[T]{
		value: initialValue,
	}
}

// Get 获取信号值
func (s *Signal[T]) Get() T {
	return s.value
}

// Set 设置信号值
func (s *Signal[T]) Set(newValue T) {
	s.value = newValue
}

// Int 整数信号
type Int = Signal[int]

// String 字符串信号
type String = Signal[string]

// Bool 布尔信号
type Bool = Signal[bool]

// Float 浮点数信号
type Float = Signal[float64]
