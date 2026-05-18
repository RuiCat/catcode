# core/buffer — 零拷贝缓冲区

## 包概述

`core/buffer` 包实现了零拷贝连续字节缓冲区，核心设计借鉴 catai 项目。通过 `[]*[]byte` 指针切片引用预编码的字节片段，利用闭包实现动态 `io.Reader`，彻底避免字符串拼接和内存复制。该包提供线程安全的快照式读取（`Get`）和可随机读取的合并式 Reader（`GetReader`），适用于需要高效组装多个不连续字节片段的场景（如流式 JSON 响应拼接）。

---

## 类型

### `Buffer`

零拷贝连续缓冲区，底层类型为 `[]*[]byte`。每个元素是指向某个字节切片的指针，支持通过指针直接引用外部预编码数据，实现零拷贝拼接。

```go
type Buffer []*[]byte
```

**设计要点：**
- `Buffer` 本身是一个切片，长度动态增长，但内容通过指针引用独立的内存块。
- 读取时通过 `Get()` 返回的快照 Reader 遍历所有指针指向的切片，拼接输出连续字节流。
- 由于直接引用指针，添加片段时无需复制数据；但需注意外部传入的字节切片在读取期间不应被修改。
- `Buffer` 的零值不可用，必须通过 `New()` 创建。

---

## 构造函数

### `New`

```go
func New() Buffer
```

创建并返回一个新的空 `Buffer` 实例。

**返回值：**

| 返回值 | 类型 | 描述 |
|------|------|------|
| `Buffer` | `Buffer` | 初始化为空的缓冲区实例 |

**功能描述：**
调用 `make(Buffer, 0)` 生成一个长度为 0 的 Buffer，后续可通过 `AddBytes`、`AddString`、`AddPtr` 添加片段。

**注意事项：**
- 返回的 Buffer 可以直接调用所有方法（包括值接收者方法如 `Len`、`ReadJSON`、`GetReader`）。

---

## 方法

### `AddBytes`

```go
func (b *Buffer) AddBytes(data []byte)
```

将字节切片的内容复制后添加到缓冲区（非零拷贝，安全复制）。

**参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `data` | `[]byte` | 要添加到缓冲区的字节数据 |

**功能描述：**
1. 分配新的 `[]byte` 指针。
2. 将 `data` 的内容复制到新分配的切片中（内部 `copy`）。
3. 将新指针追加到 `Buffer` 切片末尾。

**注意事项：**
- 与 `AddPtr` 不同，此方法会分配新内存并复制数据，因此调用方传入的 `data` 在添加后可立即修改或释放。
- 若追求极致零拷贝性能，请使用 `AddPtr` 代替。

---

### `AddString`

```go
func (b *Buffer) AddString(s string)
```

将字符串转换为字节切片后添加到缓冲区。

**参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `s` | `string` | 要添加到缓冲区的字符串 |

**功能描述：**
1. 分配新的 `[]byte` 指针。
2. 通过 `[]byte(s)` 将字符串转换为字节切片（Go 中此转换会分配新内存并复制字符串内容）。
3. 将新指针追加到 `Buffer` 切片末尾。

**注意事项：**
- 字符串到字节切片的转换会触发内存分配和数据复制。
- 适用于需要向缓冲区追加字符串字面量的场景。

---

### `AddPtr`

```go
func (b *Buffer) AddPtr(ptr *[]byte)
```

直接添加外部字节切片指针到缓冲区（最灵活，零拷贝）。

**参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `ptr` | `*[]byte` | 指向外部字节切片的指针 |

**功能描述：**
将 `ptr` 直接追加到 `Buffer` 的内部切片中，不分配新内存，不复制数据。`Buffer` 仅持有对原始切片的指针引用。

**注意事项：**
- 这是三种添加方法中最灵活的：调用方完全控制 `ptr` 指向的内存生命周期。
- 调用方必须保证 `ptr` 指向的切片内容在 `Buffer.Get()` 或 `Buffer.GetReader()` 完成读取之前不被修改或释放。
- 常见用法：将预编码的 JSON 片段指针传入，由 Buffer 负责拼接为完整流。

---

### `ReadJSON`

```go
func (b Buffer) ReadJSON(i int, v any) error
```

将数据序列化为 JSON 后更新缓冲区第 `i` 个位置的内容（原地序列化）。

**参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `i` | `int` | 要更新的片段索引（0-based） |
| `v` | `any` | 要序列化为 JSON 的任意数据 |

**返回值：**

| 返回值 | 类型 | 描述 |
|------|------|------|
| `error` | error | 序列化失败时返回错误；`i` 越界时返回 nil（静默忽略） |

**功能描述：**
1. 检查 `i` 是否在 `Buffer` 的有效范围内，若越界则直接返回 nil。
2. 复用第 `i` 个片段指向的 `[]byte` 底层数组（通过 `bytes.NewBuffer((*b[i])[:0])`），避免额外内存分配。
3. 调用 `json.NewEncoder(buf).Encode(v)` 将 `v` 序列化为 JSON 写入该 buffer。
4. 将 `*b[i]` 更新为序列化后的字节切片。

**注意事项：**
- 该方法使用值接收者，但通过指针修改切片内容（`*b[i] = buf.Bytes()`），因此仍能修改底层数据。
- 原地序列化复用了原有切片的底层数组，性能优于新建切片。
- 若 `i` 越界，方法静默返回 nil（不报错也不 panic）。
- 适用于先添加占位片段，后续再填充实际内容的模式。

---

### `Len`

```go
func (b Buffer) Len() int
```

返回缓冲区中的片段数量。

**返回值：**

| 返回值 | 类型 | 描述 |
|------|------|------|
| `int` | `int` | 片段个数（非字节总数） |

**功能描述：**
直接返回 `len(b)`，即 `Buffer` 底层切片中 `*[]byte` 指针的个数。

**注意事项：**
- 返回的是片段数量，而非所有片段的累计字节长度。若需总字节数，可自行遍历计算或使用 `GetReader()`（内部已合并所有片段）。

---

### `Reset`

```go
func (b *Buffer) Reset()
```

清空缓冲区但保留底层数组。

**功能描述：**
通过 `*b = (*b)[:0]` 将切片长度置零，但保留底层数组的内存。后续再次添加片段时，若容量足够则可复用已有内存，避免重新分配。

**注意事项：**
- 被清空的元素（指指针本身，非指针指向的数据）如果无其他引用，会由 GC 回收。
- 指针指向的数据（`*[]byte`）如果无其他引用，同样由 GC 回收。
- 该方法不保证将缓冲区容量也清空，保留的底层数组可能占用内存。若需彻底释放，可重新赋值 `*b = New()`。

---

### `Get`

```go
func (b *Buffer) Get() io.Reader
```

返回一个 `io.Reader`，可连续读取所有缓冲区内容。返回的 Reader 反映了调用 `Get` 时刻的缓冲区快照——后续对 `Buffer` 的修改不会影响该 Reader。

**返回值：**

| 返回值 | 类型 | 描述 |
|------|------|------|
| `io.Reader` | 接口 | 一个 io.Reader，可连续读取缓冲区所有片段拼接后的字节流 |

**功能描述：**
1. 捕获调用时刻的缓冲区状态快照（`buf := *b`）。
2. 返回一个 `bufferCall` 闭包（实现了 `io.Reader` 接口）。
3. 闭包内部维护一个状态机（`i` 和 `y` 变量），跟踪当前读取的片段索引和片段内偏移。
4. 每次 `Read(p)` 调用时，循环从当前片段复制数据到 `p`，直到 `p` 填满或所有片段读取完毕。
5. 所有片段读取完毕后，返回 `(0, io.EOF)`。

**读取行为细节：**
- 若 `len(p) == 0`，立即返回 `(0, io.EOF)`（符合 `io.Reader` 规范）。
- 片段之间无缝拼接：当前片段读取完毕后自动切换到下一个片段。
- 若所有片段已在之前调用中读取完毕，后续调用持续返回 `(0, io.EOF)`。

**注意事项：**
- 返回的 Reader **不是并发安全的**：`i` 和 `y` 变量在闭包内共享，多个 goroutine 同时调用 `Read` 会产生数据竞争。应在单个 goroutine 中使用。
- 每次调用 `Get` 都创建一个新的独立 Reader，不同 Reader 之间互不影响。
- Reader 持有 Buffer 快照而非实时引用，添加新片段后需重新调用 `Get` 才能包含新数据。
- 该 Reader 仅实现 `io.Reader` 接口，不支持 `Seek`、`ReadAt` 等随机读取操作。

---

### `GetReader`

```go
func (b *Buffer) GetReader() io.Reader
```

返回一个并发安全的 `io.ReadSeeker`（内部实现），通过预先合并所有片段为单个连续字节切片来支持随机读取和并发读取。

**返回值：**

| 返回值 | 类型 | 描述 |
|------|------|------|
| `io.Reader` | 接口 | 一个 io.Reader（实际类型为 `*bytes.Reader`），支持 `Read`、`Seek`、`ReadAt` 等操作 |

**功能描述：**
1. 遍历所有片段指针，计算总字节数 `totalLen`。
2. 分配一个长度为 `totalLen` 的新字节切片 `merged`。
3. 依次将所有片段内容拷贝到 `merged` 中。
4. 使用 `bytes.NewReader(merged)` 返回一个支持随机读取的 Reader。

**注意事项：**
- 会分配新内存并复制所有数据，**失去零拷贝优势**。适用于需要多次读取或并发读取的场景。
- 返回的 Reader 实际类型为 `*bytes.Reader`，实现了 `io.Reader`、`io.ReaderAt`、`io.Seeker`、`io.ByteReader` 等多个接口，可通过类型断言获取完整能力。
- 每次调用 `GetReader` 都会重新合并并分配内存。若频繁调用，可考虑缓存结果。
- 返回的 Reader 是独立的（持有合并后的数据副本），后续对 `Buffer` 的修改不会影响已返回的 Reader。
- 与 `Get()` 不同，返回的 Reader 是并发安全的（`*bytes.Reader` 的 `Read` 方法内部无共享可变状态）。

---

## 使用模式

### 基本用法：组装并发送流式响应

```go
import "core/buffer"

buf := buffer.New()
buf.AddString(`{"data":`)
buf.AddBytes(preEncodedJSON)
buf.AddString(`}`)

reader := buf.Get()
io.Copy(w, reader)  // 输出: {"data":<preEncodedJSON>}
```

### 占位符 + 后填充模式

```go
buf := buffer.New()
buf.AddString("placeholder")       // 先占位
buf.ReadJSON(0, someStruct)        // 后续序列化真实数据覆盖占位符
buf.AddString("_suffix")

reader := buf.Get()
io.Copy(w, reader)
```

### 需要多次读取时用 GetReader

```go
buf := buffer.New()
buf.AddString("hello, world")

// 可多次从头读取
r := buf.GetReader()
data1, _ := io.ReadAll(r)
r.(io.Seeker).Seek(0, io.SeekStart)
data2, _ := io.ReadAll(r)
// data1 和 data2 相同
```
