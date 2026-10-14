// 有问题欢迎联系 hot_kun@hotmail.com
// Package csvx 收拢导入导出要处理的几件标准库不管的事。
//
// 三件事都属于「不处理就出事故」：
//
//	BOM      没有它，Excel 双击打开 UTF-8 的 CSV 会把中文显示成乱码
//	公式注入  以 = + - @ 开头的单元格会被 Excel/WPS 当公式执行，
//	         而导出内容里有用户名、User-Agent 这类外部可控的字符串
//	行数上限  每一行都要过一次库或一次 bcrypt，1C1G 的机器经不起一个百万行的文件
package csvx

import (
	"encoding/csv"
	"errors"
	"io"
	"strings"
)

// BOM 是 UTF-8 字节顺序标记。Excel 靠它判断编码，缺了中文就是乱码。
const BOM = "\ufeff"

var (
	// ErrTooManyRows 表示数据行数超过调用方给的上限。
	ErrTooManyRows = errors.New("csv: too many rows")
	// ErrEmpty 表示文件里连表头都没有。
	ErrEmpty = errors.New("csv: empty file")
)

// Writer 写 CSV：自动补 BOM、自动中和公式注入。
type Writer struct {
	dst io.Writer // 非 nil 表示 BOM 还没写
	w   *csv.Writer
}

func NewWriter(dst io.Writer) *Writer {
	return &Writer{dst: dst, w: csv.NewWriter(dst)}
}

func (w *Writer) Write(rec []string) error {
	// BOM 必须在第一行**之前**。csv.Writer 自带缓冲、要到 Flush 才落盘，
	// 所以这里直接写底层 writer 是安全的：顺序由调用先后决定。
	if w.dst != nil {
		if _, err := io.WriteString(w.dst, BOM); err != nil {
			return err
		}
		w.dst = nil
	}

	cells := make([]string, len(rec))
	for i, cell := range rec {
		cells[i] = Escape(cell)
	}
	return w.w.Write(cells)
}

func (w *Writer) Flush() { w.w.Flush() }

// Err 返回最后一次写入的错误，含 Flush 时暴露出来的网络错误。
func (w *Writer) Err() error { return w.w.Error() }

// Escape 中和公式注入。
//
// Excel、WPS、Sheets 会把 = + - @ 开头的单元格当公式执行，`=HYPERLINK(...)`
// 之类足以在打开文件的人机器上发起请求。导出的单元格里有用户名、User-Agent、
// 请求路径这些外部可控的字符串，所以每一格都要过一遍。
//
// 处理方式取最小干预：前面加一个单引号——Excel 视其为「这是文本」的转义符，
// 不显示出来，其他编辑器只是多看到一个引号，不破坏内容。
func Escape(cell string) string {
	if dangerous(cell) {
		return "'" + cell
	}
	return cell
}

// Unescape 是 Escape 的逆运算，导入时逐格调用。
//
// 没有它，「导出 → 在 Excel 里改 → 导回」会把值永久改掉：手机号
// +8613800000000 导出成 '+8613800000000，导回时那个引号就留在库里了。
//
// 只还原 Escape 会写出来的形状，其余原样返回（值本身以引号开头、引号后面
// 又不是公式前缀时——比如昵称写 `'abc`——引号是数据，不能被吃掉）。
func Unescape(cell string) string {
	if len(cell) > 1 && cell[0] == '\'' && dangerous(cell[1:]) {
		return cell[1:]
	}
	return cell
}

// dangerous 判断一个值会不会被表格软件当成公式。
//
// 跳过开头的单引号再判：`'+86` 是「带 `+` 前缀的值」被转义后的样子，
// 它自己也危险（剥掉引号就是公式），所以得再转义一次。
// 否则 Unescape 分不清 `'+86` 里那个引号是转义还是数据，回程就不保真了。
func dangerous(cell string) bool {
	i := 0
	for i < len(cell) && cell[i] == '\'' {
		i++
	}
	if i >= len(cell) {
		return false
	}
	switch cell[i] {
	case '=', '+', '-', '@', '\t', '\r':
		return true
	}
	return false
}

// Read 解析一份 CSV，返回表头与数据行。
//
// 表头里的 BOM 会被去掉：Excel 保存的文件必然带它，不去掉第一列的列名
// 就永远匹配不上。
//
// maxRows 只限制数据行，超限返回 ErrTooManyRows 而**不是**默默截断——
// 截断会让调用方以为「文件里就这些内容」，然后按不完整的清单去建数据。
func Read(r io.Reader, maxRows int) (header []string, rows [][]string, err error) {
	cr := csv.NewReader(r)
	// 默认 FieldsPerRecord = 0，即「每行的列数必须与表头一致」。
	// 保持默认：模板列是固定的，某一行多一个逗号多半是有人在字段里写了
	// 没转义的逗号，继续解析只会把数据错位写进库。

	first, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, nil, ErrEmpty
	}
	if err != nil {
		return nil, nil, err
	}
	first[0] = strings.TrimPrefix(first[0], BOM)

	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return first, rows, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if len(rows) >= maxRows {
			return nil, nil, ErrTooManyRows
		}
		rows = append(rows, rec)
	}
}
