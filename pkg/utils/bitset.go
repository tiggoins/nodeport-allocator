package utils

import (
    "encoding/json"
    "fmt"
)

// BitSet 位图结构，用于高效的端口分配
type BitSet struct {
    bits   []uint64
    size   int
    offset int32 // 位图起始偏移量
}

const bitsPerWord = 64

// NewBitSet 创建新的位图
func NewBitSet(start, end int32) *BitSet {
    size := int(end - start + 1)
    wordsNeeded := (size + bitsPerWord - 1) / bitsPerWord
    return &BitSet{
        bits:   make([]uint64, wordsNeeded),
        size:   size,
        offset: start,
    }
}

// Set 设置指定位置为1
// 数据结构层安全检查：防止越界访问导致内存损坏
func (bs *BitSet) Set(port int32) error {
    if port < bs.offset || port >= bs.offset+int32(bs.size) {
        return fmt.Errorf("端口 %d 超出允许的范围 [%d, %d]", port, bs.offset, bs.offset+int32(bs.size)-1)
    }
    
    pos := int(port - bs.offset)
    wordIndex := pos / bitsPerWord
    bitIndex := pos % bitsPerWord
    bs.bits[wordIndex] |= 1 << bitIndex
    return nil
}

// Clear 清除指定位置（设置为0）
// 数据结构层安全检查：防止越界访问导致内存损坏
func (bs *BitSet) Clear(port int32) error {
    if port < bs.offset || port >= bs.offset+int32(bs.size) {
        return fmt.Errorf("端口 %d 超出允许的范围 [%d, %d]", port, bs.offset, bs.offset+int32(bs.size)-1)
    }
    
    pos := int(port - bs.offset)
    wordIndex := pos / bitsPerWord
    bitIndex := pos % bitsPerWord
    bs.bits[wordIndex] &^= 1 << bitIndex
    return nil
}

// Test 测试指定位置是否为1
func (bs *BitSet) Test(port int32) bool {
    if port < bs.offset || port >= bs.offset+int32(bs.size) {
        return false
    }
    
    pos := int(port - bs.offset)
    wordIndex := pos / bitsPerWord
    bitIndex := pos % bitsPerWord
    return (bs.bits[wordIndex] & (1 << bitIndex)) != 0
}

// FindFirstClear 找到第一个未设置的位
func (bs *BitSet) FindFirstClear() (int32, bool) {
    for wordIndex, word := range bs.bits {
        if word != ^uint64(0) { // 如果这个word不是全1
            for bitIndex := 0; bitIndex < bitsPerWord; bitIndex++ {
                if (word & (1 << bitIndex)) == 0 {
                    port := bs.offset + int32(wordIndex*bitsPerWord+bitIndex)
                    if port < bs.offset+int32(bs.size) {
                        return port, true
                    }
                }
            }
        }
    }
    return 0, false
}

// Count 计算已设置的位数
func (bs *BitSet) Count() int {
    count := 0
    for _, word := range bs.bits {
        count += popCount(word)
    }
    return count
}

// popCount 计算uint64中1的个数
func popCount(x uint64) int {
    count := 0
    for x != 0 {
        count++
        x &= x - 1 // 清除最低位的1
    }
    return count
}

// ToJSON 序列化为JSON
func (bs *BitSet) ToJSON() ([]byte, error) {
    data := map[string]interface{}{
        "bits":   bs.bits,
        "size":   bs.size,
        "offset": bs.offset,
    }
    return json.Marshal(data)
}

// FromJSON 从JSON反序列化
func (bs *BitSet) FromJSON(data []byte) error {
    var temp map[string]interface{}
    if err := json.Unmarshal(data, &temp); err != nil {
        return err
    }
    
    // 转换bits
    bitsInterface, ok := temp["bits"].([]interface{})
    if !ok {
        return fmt.Errorf("invalid bits format")
    }
    
    bits := make([]uint64, len(bitsInterface))
    for i, v := range bitsInterface {
        if f, ok := v.(float64); ok {
            bits[i] = uint64(f)
        } else {
            return fmt.Errorf("invalid bit value at index %d", i)
        }
    }
    
    size, ok := temp["size"].(float64)
    if !ok {
        return fmt.Errorf("invalid size format")
    }
    
    offset, ok := temp["offset"].(float64)
    if !ok {
        return fmt.Errorf("invalid offset format")
    }
    
    bs.bits = bits
    bs.size = int(size)
    bs.offset = int32(offset)
    
    return nil
}

